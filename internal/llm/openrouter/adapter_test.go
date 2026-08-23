package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	usererr "github.com/humansh/humansh/internal/errors"
	"github.com/humansh/humansh/internal/llm"
	"github.com/humansh/humansh/internal/llm/contracttest"
	"github.com/humansh/humansh/internal/shell/protocol"
)

func TestRequestShapeAndWireSchema(t *testing.T) {
	t.Parallel()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-OpenRouter-Metadata") != "enabled" {
			t.Errorf("router metadata=%q", r.Header.Get("X-OpenRouter-Metadata"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"status\":\"ok\",\"command\":\"ls\",\"explanation\":\"Lists files.\",\"clarification\":\"\",\"assumptions\":[]}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", StructuredOutputProven: true}, Client: server.Client()}
	response, err := adapter.Translate(context.Background(), llm.TranslationRequest{Input: "list files", Shell: "zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Command != "ls" {
		t.Fatalf("response=%+v", response)
	}
	if _, ok := body["tools"]; ok {
		t.Fatal("tools field present")
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatal("tool_choice field present")
	}
	if body["max_tokens"] != float64(translationMaxTokens) {
		t.Fatalf("max_tokens=%v", body["max_tokens"])
	}
	encoded, _ := json.Marshal(body["response_format"])
	if containsJSONKey(encoded, "maxLength") || containsJSONKey(encoded, "$schema") {
		t.Fatalf("unsupported schema keyword in %s", encoded)
	}
}

func TestValidateStructuredOutputModelUsesFreeCatalogMetadata(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/model/test/model" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request=%s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":{"id":"test/model","supported_parameters":["response_format","structured_outputs"]}}`))
	}))
	defer server.Close()
	adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", AllowUnprovenSchemaProbe: true}, Client: server.Client()}
	if err := adapter.ValidateStructuredOutputModel(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateStructuredOutputModelRejectsUnsupportedMissingAndMalformedModels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, code string
		status           int
		exit             int
	}{
		{name: "unsupported", status: http.StatusOK, body: `{"data":{"id":"stealth/ox-alpha","supported_parameters":["response_format"]}}`, code: "openrouter_structured_output_unsupported", exit: protocol.ExitProviderUnavailable},
		{name: "missing", status: http.StatusNotFound, body: `{"error":{"message":"Not found"}}`, code: "openrouter_model_not_found", exit: protocol.ExitProviderUnavailable},
		{name: "malformed", status: http.StatusOK, body: `{"data":null}`, code: "provider_malformed", exit: protocol.ExitProviderMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			adapter := Adapter{Config: Config{Model: "stealth/ox-alpha", BaseURL: server.URL, APIKey: "test-key", AllowUnprovenSchemaProbe: true}, Client: server.Client()}
			err := adapter.ValidateStructuredOutputModel(context.Background())
			typed, ok := usererr.As(err)
			if !ok || typed.Code != test.code || typed.ExitCode != test.exit {
				t.Fatalf("error=%#v want code=%s exit=%d", err, test.code, test.exit)
			}
			if test.name == "unsupported" {
				rendered := usererr.Render(typed, false)
				for _, want := range []string{"stealth/ox-alpha does not support strict structured output", "No compatibility request was sent", "supported_parameters=structured_outputs"} {
					if !strings.Contains(rendered, want) {
						t.Errorf("unsupported-model error omitted %q: %s", want, rendered)
					}
				}
			}
		})
	}
}

func TestStructuredOutputProbeUsesExactSchemaWithSmallBudget(t *testing.T) {
	t.Parallel()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"status\":\"ok\",\"command\":\"pwd\",\"explanation\":\"Prints the current directory.\",\"clarification\":\"\",\"assumptions\":[]}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	adapter := Adapter{Config: Config{
		Model: "test/model", BaseURL: server.URL, APIKey: "test-key", AllowUnprovenSchemaProbe: true,
	}, Client: server.Client()}
	response, err := adapter.ProbeStructuredOutput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if response.Command != "pwd" {
		t.Fatalf("response=%+v", response)
	}
	if body["max_tokens"] != float64(probeMaxTokens) || probeMaxTokens >= translationMaxTokens {
		t.Fatalf("probe max_tokens=%v production=%d", body["max_tokens"], translationMaxTokens)
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("probe messages=%#v", body["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "user" || len(message["content"].(string)) > 160 {
		t.Fatalf("probe message=%#v", messages[0])
	}
	if _, ok := body["tools"]; ok {
		t.Fatal("tools field present")
	}
	wireSchema, err := WireSchema()
	if err != nil {
		t.Fatal(err)
	}
	wantFormat, _ := json.Marshal(strictResponseFormat(wireSchema))
	gotFormat, _ := json.Marshal(body["response_format"])
	if string(gotFormat) != string(wantFormat) {
		t.Fatalf("probe schema differs from production schema:\ngot  %s\nwant %s", gotFormat, wantFormat)
	}
	provider, ok := body["provider"].(map[string]any)
	if !ok || provider["require_parameters"] != true {
		t.Fatalf("provider routing=%#v", body["provider"])
	}
}

func TestHTTP402MapsQuota(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "credits", 402) }))
	defer server.Close()
	adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", StructuredOutputProven: true}, Client: server.Client()}
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != protocol.ExitProviderQuota {
		t.Fatalf("error=%#v", err)
	}
}

func TestHTTPStatusMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status int
		exit   int
		code   string
	}{
		{400, protocol.ExitProviderUnavailable, "openrouter_invalid_request"},
		{401, protocol.ExitProviderAuth, "openrouter_auth"},
		{402, protocol.ExitProviderQuota, "openrouter_credits"},
		{403, protocol.ExitProviderUnavailable, "openrouter_policy"},
		{404, protocol.ExitProviderUnavailable, "openrouter_route_not_found"},
		{408, protocol.ExitProviderTemporary, "provider_timeout"},
		{429, protocol.ExitProviderQuota, "provider_quota"},
		{500, protocol.ExitProviderTemporary, "provider_temporary"},
		{503, protocol.ExitProviderTemporary, "provider_temporary"},
	}
	for _, test := range tests {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("server body containing test-key must stay redacted"))
			}))
			defer server.Close()
			adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", StructuredOutputProven: true}, Client: server.Client()}
			_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
			typed, ok := usererr.As(err)
			if !ok || typed.ExitCode != test.exit || typed.Code != test.code {
				t.Fatalf("error=%#v want exit=%d code=%s", err, test.exit, test.code)
			}
			if strings.Contains(typed.Error(), "test-key") || typed.DebugCause != nil && strings.Contains(typed.DebugCause.Error(), "test-key") {
				t.Fatal("credential leaked through error")
			}
		})
	}
}

func TestHTTP404PreservesSanitizedOpenRouterRoutingError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-OpenRouter-Metadata") != "enabled" {
			t.Errorf("router metadata=%q", r.Header.Get("X-OpenRouter-Metadata"))
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"No allowed providers support structured outputs. Bearer sk-or-v1-supersecret\n"},"openrouter_metadata":{"summary":"available=1, selected=none"}}`))
	}))
	defer server.Close()
	adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", StructuredOutputProven: true}, Client: server.Client()}
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.Code != "openrouter_route_not_found" {
		t.Fatalf("error=%#v", err)
	}
	for _, want := range []string{"No allowed providers support structured outputs", "available=1, selected=none"} {
		if !strings.Contains(typed.Title, want) {
			t.Errorf("title omitted %q: %s", want, typed.Title)
		}
	}
	if strings.Contains(typed.Title, "supersecret") || strings.ContainsAny(typed.Title, "\r\n") {
		t.Fatalf("unsafe provider detail in title: %q", typed.Title)
	}
}

func TestReadOnlyKeyValidationAndProvenModelGate(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/key" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request=%s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":{"limit_remaining":10}}`))
	}))
	defer server.Close()

	unproven := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key"}, Client: server.Client()}
	if diagnostic := unproven.Diagnose(context.Background()); diagnostic.Available || diagnostic.Authenticated || requests.Load() != 0 {
		t.Fatalf("unproven diagnostic=%+v requests=%d", diagnostic, requests.Load())
	}
	if _, err := unproven.Translate(context.Background(), llm.TranslationRequest{}); err == nil {
		t.Fatal("unproven model was allowed to make a translation request")
	}

	proven := unproven
	proven.Config.StructuredOutputProven = true
	if diagnostic := proven.Diagnose(context.Background()); !diagnostic.Available || !diagnostic.Authenticated || requests.Load() != 1 {
		t.Fatalf("proven diagnostic=%+v requests=%d", diagnostic, requests.Load())
	}
}

func TestHTTP402IncludesCreditAndSubscriptionFixes(t *testing.T) {
	t.Parallel()
	err := mapHTTP(http.StatusPaymentRequired, nil, 20*time.Second)
	typed, ok := usererr.As(err)
	if !ok || len(typed.Fixes) != 3 {
		t.Fatalf("error=%#v", err)
	}
	rendered := usererr.Render(typed, false)
	for _, text := range []string{"Add credits", "humansh provider use codex", "humansh provider use claude"} {
		if !strings.Contains(rendered, text) {
			t.Errorf("rendered error omitted %q: %s", text, rendered)
		}
	}
}

func TestProviderContract(t *testing.T) {
	t.Parallel()
	newServer := func(chatBody string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/key":
				_, _ = w.Write([]byte(`{"data":{"limit_remaining":10}}`))
			case "/chat/completions":
				_, _ = w.Write([]byte(chatBody))
			default:
				http.NotFound(w, r)
			}
		}))
	}
	success := newServer(`{"choices":[{"message":{"content":"{\"status\":\"ok\",\"command\":\"ls\",\"explanation\":\"Lists files.\",\"clarification\":\"\",\"assumptions\":[]}"},"finish_reason":"stop"}]}`)
	defer success.Close()
	malformed := newServer(`{"choices":[{"message":{"content":"{"},"finish_reason":"stop"}]}`)
	defer malformed.Close()
	oversized := newServer(strings.Repeat("x", (1<<20)+1))
	defer oversized.Close()
	adapterFor := func(server *httptest.Server) Adapter {
		return Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", StructuredOutputProven: true}, Client: server.Client()}
	}
	contracttest.Run(t, contracttest.Cases{
		Provider: adapterFor(success),
		ID:       llm.OpenRouter,
		Malformed: func(ctx context.Context) error {
			_, err := adapterFor(malformed).Translate(ctx, llm.TranslationRequest{})
			return err
		},
		Oversized: func(ctx context.Context) error {
			_, err := adapterFor(oversized).Translate(ctx, llm.TranslationRequest{})
			return err
		},
	})
}

func TestRejectsToolCallsTruncationAndOversizedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"tool-call", `{"choices":[{"message":{"content":"{}","tool_calls":[{}]},"finish_reason":"stop"}]}`},
		{"refusal", `{"choices":[{"message":{"content":"{}","refusal":"denied"},"finish_reason":"stop"}]}`},
		{"truncated", `{"choices":[{"message":{"content":"{}"},"finish_reason":"length"}]}`},
		{"missing-choice", `{"choices":[]}`},
		{"multiple-choices", `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"},{"message":{"content":"{}"},"finish_reason":"stop"}]}`},
		{"oversized", strings.Repeat("x", (1<<20)+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.body)) }))
			defer server.Close()
			adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, APIKey: "test-key", StructuredOutputProven: true}, Client: server.Client()}
			_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
			typed, ok := usererr.As(err)
			if !ok || typed.ExitCode != protocol.ExitProviderMalformed {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}

func TestTimeoutCancellationAndLazyKeyLoading(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	loads := 0
	adapter := Adapter{Config: Config{Model: "test/model", BaseURL: server.URL, Timeout: 50 * time.Millisecond, StructuredOutputProven: true}, Client: server.Client(), KeyLoader: func() (string, error) {
		loads++
		return "lazy-key", nil
	}}
	started := time.Now()
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	close(release)
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != protocol.ExitProviderTemporary || time.Since(started) > time.Second {
		t.Fatalf("error=%#v elapsed=%s", err, time.Since(started))
	}
	if loads != 1 {
		t.Fatalf("key loads=%d", loads)
	}
}

func TestCredentialControlsAreRejectedBeforeHTTP(t *testing.T) {
	for _, key := range []string{"test\nkey", " leading-space", "trailing-space ", "test\tkey"} {
		called := false
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, fmt.Errorf("unexpected HTTP call")
		})}
		adapter := Adapter{Config: Config{Model: "test/model", APIKey: key, StructuredOutputProven: true}, Client: client}
		_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
		typed, ok := usererr.As(err)
		if !ok || typed.ExitCode != protocol.ExitProviderAuth || called {
			t.Fatalf("key=%q error=%#v called=%t", key, err, called)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestDiagnosticDoesNotExposeCredentialLoaderErrors(t *testing.T) {
	diagnostic := (Adapter{KeyLoader: func() (string, error) {
		return "", fmt.Errorf("failed while handling sk-or-v1-supersecret")
	}}).Diagnose(context.Background())
	if strings.Contains(diagnostic.Message, "supersecret") || !strings.Contains(diagnostic.Message, "could not be loaded securely") {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
}

func TestDefaultClientDoesNotFollowCredentialedRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	adapter := Adapter{Config: Config{Model: "test/model", BaseURL: source.URL, APIKey: "test-key", StructuredOutputProven: true}}
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	if err == nil || redirected {
		t.Fatalf("error=%#v redirected=%t", err, redirected)
	}
}

func containsJSONKey(data []byte, key string) bool {
	var value any
	_ = json.Unmarshal(data, &value)
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if k == key || walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}
