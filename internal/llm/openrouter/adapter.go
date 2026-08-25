package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/agenticlab-ai/humansh/assets"
	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/exitcode"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/providerutil"
	"github.com/agenticlab-ai/humansh/internal/prompt"
)

type Config struct {
	Model, BaseURL, APIKey   string
	Timeout                  time.Duration
	StructuredOutputProven   bool
	AllowUnprovenSchemaProbe bool
}
type Adapter struct {
	Config    Config
	Client    *http.Client
	KeyLoader func() (string, error)
}

const (
	translationMaxTokens = 800
	probeMaxTokens       = 128
	compatibleModelsURL  = "https://openrouter.ai/models?order=newest&supported_parameters=structured_outputs"
)

func (Adapter) ID() llm.ProviderID { return llm.OpenRouter }

func (a Adapter) Diagnose(ctx context.Context) llm.Diagnostic {
	key, err := a.loadKey()
	if err != nil {
		return llm.Diagnostic{Installed: true, AuthMode: "unknown", Message: "OpenRouter credential could not be loaded securely; reconfigure it"}
	}
	if key == "" {
		return llm.Diagnostic{Installed: true, AuthMode: "missing", Message: "OpenRouter API key is not configured"}
	}
	if a.Config.Model == "" || !a.Config.StructuredOutputProven {
		return llm.Diagnostic{Installed: true, Configured: true, Authenticated: false, AuthMode: "api_key_detected_unverified", Message: "An API key is configured but has not been validated; no schema-proven OpenRouter model is configured"}
	}
	if err := a.validateKey(ctx, key); err != nil {
		return llm.Diagnostic{Installed: true, Configured: true, Authenticated: false, AuthMode: "api_key_invalid", Version: a.Config.Model, Message: "OpenRouter API key validation failed: " + diagnosticMessage(err)}
	}
	return llm.Diagnostic{Installed: true, Configured: true, Authenticated: true, Available: true, AuthMode: "api_key", Version: a.Config.Model, Capabilities: []string{"strict-structured-output", "tools-disabled"}}
}

func (a Adapter) ValidateKey(ctx context.Context) error {
	key, err := a.loadKey()
	if err != nil {
		return usererr.WithExit(exitcode.ProviderAuth, "openrouter_key_load", "OpenRouter credential could not be loaded securely.", "Nothing was changed or executed.", false, err, usererr.Fix{Description: "Repair it with", Command: "humansh provider configure openrouter"})
	}
	if key == "" {
		return usererr.WithExit(exitcode.ProviderAuth, "openrouter_key_missing", "OpenRouter API key is not configured.", "Nothing was changed or executed.", false, nil, usererr.Fix{Description: "Configure it with", Command: "humansh provider configure openrouter"})
	}
	return a.validateKey(ctx, key)
}

// ValidateStructuredOutputModel performs a read-only catalog lookup before
// setup spends model credits on the exact-schema probe. OpenRouter distinguishes
// basic response_format support from strict structured_outputs support.
func (a Adapter) ValidateStructuredOutputModel(ctx context.Context) error {
	key, err := a.requestKey()
	if err != nil {
		return err
	}
	author, slug, ok := strings.Cut(a.Config.Model, "/")
	if !ok || author == "" || slug == "" {
		return usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_model_id_invalid", "OpenRouter requires a concrete provider/model ID.", "Nothing was changed or executed; no model credits were used.", false, nil,
			usererr.Fix{Description: "Choose a compatible model at " + compatibleModelsURL})
	}

	callCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	modelURL := strings.TrimRight(a.baseURL(), "/") + "/model/" + url.PathEscape(author) + "/" + url.PathEscape(slug)
	request, err := http.NewRequestWithContext(callCtx, http.MethodGet, modelURL, nil)
	if err != nil {
		return providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("X-OpenRouter-Title", "humansh")
	response, err := a.client().Do(request)
	if err != nil {
		return providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (256<<10)+1))
	if err != nil {
		return providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	if len(data) > 256<<10 {
		return providerutil.Malformed("OpenRouter model response exceeded 256 KiB", nil)
	}
	if response.StatusCode == http.StatusNotFound {
		return usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_model_not_found", "OpenRouter does not recognize model "+safeExternalText(a.Config.Model, 256)+".", "Nothing was changed or executed; no model credits were used.", false, fmt.Errorf("OpenRouter model lookup HTTP 404"),
			usererr.Fix{Description: "Choose a compatible model at " + compatibleModelsURL})
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return mapHTTP(response.StatusCode, data, a.timeout())
	}
	var envelope struct {
		Data struct {
			ID                  string   `json:"id"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Data.ID == "" {
		return providerutil.Malformed("OpenRouter model response", err)
	}
	for _, parameter := range envelope.Data.SupportedParameters {
		if parameter == "structured_outputs" {
			return nil
		}
	}
	model := safeExternalText(envelope.Data.ID, 256)
	return usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_structured_output_unsupported", "OpenRouter model "+model+" does not support strict structured output.", "The free catalog check found the model, but its current endpoints do not advertise the structured_outputs capability humansh requires. No compatibility request was sent and no model credits were used.", false, nil,
		usererr.Fix{Description: "Choose a compatible model at " + compatibleModelsURL})
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
	key, err := a.requestKey()
	if err != nil {
		return llm.TranslationResponse{}, err
	}
	wireSchema, err := WireSchema()
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("build OpenRouter wire schema", err)
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("encode request", err)
	}
	body := map[string]any{
		"model": a.Config.Model, "stream": false, "max_tokens": translationMaxTokens,
		"messages":        []map[string]string{{"role": "system", "content": prompt.Instruction}, {"role": "user", "content": string(requestJSON)}},
		"response_format": strictResponseFormat(wireSchema),
		"provider":        map[string]any{"require_parameters": true},
	}
	return a.complete(ctx, key, body)
}

// ProbeStructuredOutput tests the exact production response schema with a
// deliberately small prompt and output budget. Setup uses it only after the
// user has explicitly selected OpenRouter and supplied a concrete model.
func (a Adapter) ProbeStructuredOutput(ctx context.Context) (llm.TranslationResponse, error) {
	key, err := a.requestKey()
	if err != nil {
		return llm.TranslationResponse{}, err
	}
	wireSchema, err := WireSchema()
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("build OpenRouter wire schema", err)
	}
	body := map[string]any{
		"model": a.Config.Model, "stream": false, "max_tokens": probeMaxTokens,
		"messages": []map[string]string{{
			"role":    "user",
			"content": `Return status "ok", command "pwd", a short explanation, an empty clarification, and no assumptions.`,
		}},
		"response_format": strictResponseFormat(wireSchema),
		"provider":        map[string]any{"require_parameters": true},
	}
	return a.complete(ctx, key, body)
}

func (a Adapter) requestKey() (string, error) {
	key, err := a.loadKey()
	if err != nil {
		return "", usererr.WithExit(exitcode.ProviderAuth, "openrouter_key_load", "OpenRouter credential could not be loaded securely.", "Nothing was changed or executed.", false, err, usererr.Fix{Description: "Repair it with", Command: "humansh provider configure openrouter"})
	}
	if key == "" {
		return "", usererr.WithExit(exitcode.ProviderAuth, "openrouter_key_missing", "OpenRouter API key is not configured.", "Nothing was changed or executed.", false, nil, usererr.Fix{Description: "Configure it with", Command: "humansh provider configure openrouter"})
	}
	if a.Config.Model == "" || a.Config.Model == "openrouter/auto" || (!a.Config.StructuredOutputProven && !a.Config.AllowUnprovenSchemaProbe) {
		return "", usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_model_missing", "OpenRouter has no proven structured-output model configured.", "Nothing was changed or executed.", false, nil, usererr.Fix{Description: "Configure and probe a concrete model with", Command: "humansh provider configure openrouter"})
	}
	return key, nil
}

func strictResponseFormat(wireSchema map[string]any) map[string]any {
	return map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "humansh_translation", "strict": true, "schema": wireSchema}}
}

func (a Adapter) complete(ctx context.Context, key string, body map[string]any) (llm.TranslationResponse, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("encode OpenRouter request", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	url := strings.TrimRight(a.baseURL(), "/") + "/chat/completions"
	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return llm.TranslationResponse{}, providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+key)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-OpenRouter-Title", "humansh")
	httpRequest.Header.Set("X-OpenRouter-Metadata", "enabled")
	response, err := a.client().Do(httpRequest)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, (1<<20)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	if len(data) > 1<<20 {
		return llm.TranslationResponse{}, providerutil.Malformed("OpenRouter response exceeded 1 MiB", nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return llm.TranslationResponse{}, mapHTTP(response.StatusCode, data, a.timeout())
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []any  `json:"tool_calls"`
				Refusal   any    `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("OpenRouter response envelope", err)
	}
	if len(envelope.Choices) != 1 || len(envelope.Choices[0].Message.ToolCalls) != 0 || envelope.Choices[0].Message.Refusal != nil || envelope.Choices[0].Message.Content == "" || envelope.Choices[0].FinishReason == "length" {
		return llm.TranslationResponse{}, providerutil.Malformed("OpenRouter returned missing, tool, refusal, or truncated content", nil)
	}
	return providerutil.DecodeResponse([]byte(envelope.Choices[0].Message.Content))
}

func (a Adapter) validateKey(ctx context.Context, key string) error {
	callCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodGet, strings.TrimRight(a.baseURL(), "/")+"/key", nil)
	if err != nil {
		return providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := a.client().Do(request)
	if err != nil {
		return providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil {
		return providerutil.TemporaryOrTimeout(llm.OpenRouter, a.timeout(), err)
	}
	if len(data) > 64<<10 {
		return providerutil.Malformed("OpenRouter key-status response exceeded 64 KiB", nil)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return mapHTTP(response.StatusCode, data, a.timeout())
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		return providerutil.Malformed("OpenRouter key-status response", nil)
	}
	return nil
}

func diagnosticMessage(err error) string {
	if typed, ok := usererr.As(err); ok {
		return typed.Title
	}
	return "read-only key check did not complete"
}

func WireSchema() (map[string]any, error) {
	var schema map[string]any
	if err := json.Unmarshal(assets.TranslationSchema, &schema); err != nil {
		return nil, err
	}
	scrub(schema)
	return schema, nil
}
func scrub(value any) {
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, "$schema")
		delete(typed, "maxLength")
		for _, child := range typed {
			scrub(child)
		}
	case []any:
		for _, child := range typed {
			scrub(child)
		}
	}
}
func (a Adapter) loadKey() (string, error) {
	var key string
	var err error
	if a.Config.APIKey != "" {
		key = a.Config.APIKey
	} else if a.KeyLoader != nil {
		key, err = a.KeyLoader()
	}
	if err != nil {
		return "", err
	}
	if len(key) > 16<<10 {
		return "", fmt.Errorf("OpenRouter credential exceeds 16 KiB")
	}
	for _, r := range key {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", fmt.Errorf("OpenRouter credential contains whitespace or control characters")
		}
	}
	return key, nil
}
func (a Adapter) baseURL() string {
	if a.Config.BaseURL != "" {
		return a.Config.BaseURL
	}
	return "https://openrouter.ai/api/v1"
}
func (a Adapter) timeout() time.Duration {
	if a.Config.Timeout > 0 {
		return a.Config.Timeout
	}
	return 20 * time.Second
}
func (a Adapter) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func mapHTTP(status int, data []byte, timeout time.Duration) error {
	cause := fmt.Errorf("OpenRouter HTTP %d", status)
	switch status {
	case 401:
		return usererr.WithExit(exitcode.ProviderAuth, "openrouter_auth", "OpenRouter API key is invalid, disabled, or expired.", "Nothing was changed or executed.", false, cause, usererr.Fix{Description: "Configure a valid key with", Command: "humansh provider configure openrouter"})
	case 402:
		return usererr.WithExit(exitcode.ProviderQuota, "openrouter_credits", "OpenRouter credits or the API key spending limit are exhausted.", "Nothing was changed or executed; no paid fallback was attempted.", true, cause,
			usererr.Fix{Description: "Add credits or raise this key's limit in OpenRouter"},
			usererr.Fix{Description: "Or explicitly switch subscription provider with", Command: "humansh provider use codex"},
			usererr.Fix{Description: "Or", Command: "humansh provider use claude"})
	case 400:
		return usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_invalid_request", withOpenRouterDetail("OpenRouter rejected the configured model or structured-output schema.", data), "Nothing was changed or executed.", false, cause, usererr.Fix{Description: "Check the model and schema with", Command: "humansh provider test openrouter"})
	case 403:
		return usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_policy", withOpenRouterDetail("OpenRouter denied this key, model, or provider policy.", data), "Nothing was changed or executed.", false, cause, usererr.Fix{Description: "Configure a permitted model/key with", Command: "humansh provider configure openrouter"})
	case 404:
		return usererr.WithExit(exitcode.ProviderUnavailable, "openrouter_route_not_found", withOpenRouterDetail("OpenRouter found no eligible endpoint for the selected model.", data), "Nothing was changed or executed.", false, cause,
			usererr.Fix{Description: "Choose a compatible model at " + compatibleModelsURL},
			usererr.Fix{Description: "Then configure it with", Command: "humansh provider configure openrouter"})
	case 408:
		return providerutil.Timeout(llm.OpenRouter, timeout, cause)
	case 429:
		return providerutil.Quota("OpenRouter", cause)
	default:
		if status >= 500 {
			return providerutil.Temporary("OpenRouter", cause)
		}
		return providerutil.Malformed("unexpected OpenRouter status", cause)
	}
}

func withOpenRouterDetail(title string, data []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Metadata struct {
			Summary string `json:"summary"`
		} `json:"openrouter_metadata"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return title
	}
	detail := safeExternalText(envelope.Error.Message, 400)
	summary := safeExternalText(envelope.Metadata.Summary, 200)
	if summary != "" && !strings.Contains(strings.ToLower(detail), strings.ToLower(summary)) {
		if detail != "" {
			detail += "; routing: " + summary
		} else {
			detail = "Routing: " + summary
		}
	}
	if detail == "" {
		return title
	}
	return strings.TrimSuffix(title, ".") + ". OpenRouter reported: " + detail
}

func safeExternalText(value string, maxBytes int) string {
	value = strings.Join(strings.Fields(usererr.RedactDebug(value)), " ")
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value) + "…"
}
