package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/exitcode"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/contracttest"
	"github.com/agenticlab-ai/humansh/internal/processrunner"
)

type fakeRunner struct {
	calls      []processrunner.Spec
	deadlines  []bool
	deadlineAt []time.Time
	version    string
	help       string
	status     string
	output     string
	versionErr error
	helpErr    error
	statusErr  error
	modelErr   error
}

func clearCursorEnvironment(t *testing.T) {
	t.Helper()
	keys := append(append([]string{}, cursorOverrideEnvKeys...), cursorUserIdentityEnvKeys...)
	keys = append(keys, cursorCredentialLocationEnvKeys...)
	keys = append(keys, "AGENT_CLI_CREDENTIAL_STORE", "OSTYPE")
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func (f *fakeRunner) Run(ctx context.Context, spec processrunner.Spec) (processrunner.Result, error) {
	f.calls = append(f.calls, spec)
	deadlineAt, deadline := ctx.Deadline()
	f.deadlines = append(f.deadlines, deadline)
	f.deadlineAt = append(f.deadlineAt, deadlineAt)
	if err := ctx.Err(); err != nil {
		return processrunner.Result{}, err
	}
	if reflect.DeepEqual(spec.Args, []string{"--version"}) {
		value := f.version
		if value == "" {
			value = "2026.07.23-e383d2b\n"
		}
		return processrunner.Result{Stdout: []byte(value)}, f.versionErr
	}
	if reflect.DeepEqual(spec.Args, []string{"--help"}) {
		value := f.help
		if value == "" {
			value = strings.Join(requiredHelpOptions, " ")
		}
		return processrunner.Result{Stdout: []byte(value)}, f.helpErr
	}
	if reflect.DeepEqual(spec.Args, []string{"status", "--format", "json"}) {
		value := f.status
		if value == "" {
			value = `{"authenticated":true,"email":"person@example.com"}`
		}
		return processrunner.Result{Stdout: []byte(value)}, f.statusErr
	}
	value := f.output
	if value == "" {
		value = `{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"ok\",\"command\":\"ls\",\"explanation\":\"Lists files.\",\"clarification\":\"\",\"assumptions\":[]}"}`
	}
	return processrunner.Result{Stdout: []byte(value)}, f.modelErr
}

func TestEveryCursorSubprocessIsTimedAndIsolated(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{}
	adapter := Adapter{Config: Config{Timeout: 3 * time.Second}, Runner: runner}
	if _, err := adapter.Translate(context.Background(), llm.TranslationRequest{Input: "list files", Shell: "zsh"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 || len(runner.deadlines) != 4 {
		t.Fatalf("calls=%d deadlines=%d", len(runner.calls), len(runner.deadlines))
	}
	for index, call := range runner.calls {
		if !runner.deadlines[index] {
			t.Errorf("call %d has no deadline", index)
		}
		if call.Dir == "" || filepath.Clean(filepath.Dir(call.Dir)) != filepath.Clean(os.TempDir()) {
			t.Errorf("call %d directory is not isolated: %q", index, call.Dir)
		}
		if !slices.Contains(call.Env, "TMPDIR="+call.Dir) {
			t.Errorf("call %d TMPDIR does not match isolation directory: %v", index, call.Env)
		}
	}
	if runner.calls[0].Dir != runner.calls[1].Dir || runner.calls[0].Dir != runner.calls[2].Dir || runner.calls[3].Dir == runner.calls[0].Dir {
		t.Fatalf("diagnostic/model isolation directories are wrong")
	}
	if !runner.deadlineAt[3].After(runner.deadlineAt[2]) {
		t.Fatalf("model deadline %v did not receive a fresh budget after diagnostics ending at %v", runner.deadlineAt[3], runner.deadlineAt[2])
	}
}

func TestCursorTimeoutExplainsCauseAndHowToIncreaseLimit(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{modelErr: context.DeadlineExceeded}
	_, err := (Adapter{Config: Config{Timeout: 20 * time.Second}, Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderTemporary || typed.Code != "provider_timeout" {
		t.Fatalf("error=%#v", err)
	}
	rendered := usererr.Render(typed, false)
	for _, want := range []string{
		"Cursor CLI timed out before completing the translation.",
		"configured provider timeout is 20 seconds",
		"Nothing was changed or executed.",
		"humansh config set timeout_seconds 45",
		"humansh provider test cursor",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("timeout guidance omitted %q:\n%s", want, rendered)
		}
	}
}

func TestSafeReadOnlyInvocation(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{}
	response, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{Input: "MARKER_SECRET", Shell: "zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Command != "ls" {
		t.Fatalf("response=%+v", response)
	}
	call := runner.calls[3]
	want := []string{"--print", "--output-format", "json", "--mode", "ask", "--sandbox", "enabled", "--trust"}
	if !reflect.DeepEqual(call.Args, want) {
		t.Fatalf("Cursor argv drifted:\n got: %#v\nwant: %#v", call.Args, want)
	}
	if strings.Contains(strings.Join(call.Args, " "), "MARKER_SECRET") || !strings.Contains(string(call.Stdin), "MARKER_SECRET") {
		t.Fatalf("dynamic request was not confined to stdin: args=%v stdin=%q", call.Args, call.Stdin)
	}
	for _, required := range []string{"RESPONSE_SCHEMA_BEGIN", `"additionalProperties": false`, "REQUEST_JSON_BEGIN"} {
		if !strings.Contains(string(call.Stdin), required) {
			t.Errorf("stdin omitted %q", required)
		}
	}
}

func TestConfiguredModelIsPassedAsSeparateArgument(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{}
	_, err := (Adapter{Config: Config{Model: "cursor-model"}, Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := runner.calls[3].Args; !reflect.DeepEqual(got[len(got)-2:], []string{"--model", "cursor-model"}) {
		t.Fatalf("model args=%v", got)
	}
}

func TestParseAuthFixtures(t *testing.T) {
	tests := []struct{ name, value, want string }{
		{"authenticated-bool", `{"authenticated":true,"email":"person@example.com"}`, "cursor.com"},
		{"logged-in-bool", `{"loggedIn":true}`, "cursor.com"},
		{"status", `{"status":"logged_in"}`, "cursor.com"},
		{"installed-cli-status", `{"status":"authenticated","isAuthenticated":true,"hasAccessToken":true,"hasRefreshToken":true,"message":"Logged in"}`, "cursor.com"},
		{"installed-cli-logged-out", `{"status":"unauthenticated","isAuthenticated":false,"hasAccessToken":false,"hasRefreshToken":false,"message":"Not logged in"}`, "logged_out"},
		{"logged-out", `{"authenticated":false}`, "logged_out"},
		{"api-wins", `{"authenticated":true,"authMethod":"api_key"}`, "api"},
		{"malformed", `{`, "unknown"},
		{"unknown", `{"email":"person@example.com"}`, "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseAuth([]byte(test.value)); got != test.want {
				t.Fatalf("parseAuth(%s)=%q want %q", test.value, got, test.want)
			}
		})
	}
}

func TestCursorCredentialLocationsAndFileStoreAreNarrowlyForwarded(t *testing.T) {
	clearCursorEnvironment(t)
	for _, key := range cursorCredentialLocationEnvKeys {
		t.Setenv(key, filepath.Join(t.TempDir(), strings.ToLower(key)))
	}
	t.Setenv("AGENT_CLI_CREDENTIAL_STORE", "file")
	t.Setenv("OSTYPE", "darwin25.0")
	t.Setenv("GITHUB_TOKEN", "unrelated-secret")
	runner := &fakeRunner{}
	diagnostic := (Adapter{Runner: runner}).Diagnose(context.Background())
	if !diagnostic.Available {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	for index, call := range runner.calls {
		env := strings.Join(call.Env, "\n")
		for _, key := range cursorCredentialLocationEnvKeys {
			if !strings.Contains(env, key+"="+os.Getenv(key)) {
				t.Errorf("call %d omitted %s: %v", index, key, call.Env)
			}
		}
		for _, want := range []string{"AGENT_CLI_CREDENTIAL_STORE=file", "OSTYPE=darwin25.0"} {
			if !strings.Contains(env, want) {
				t.Errorf("call %d omitted %q: %v", index, want, call.Env)
			}
		}
		if strings.Contains(env, "GITHUB_TOKEN") || strings.Contains(env, "unrelated-secret") {
			t.Errorf("call %d received unrelated secret", index)
		}
	}
}

func TestParentCursorOverridesAreRejectedBeforeSubprocess(t *testing.T) {
	for _, key := range cursorOverrideEnvKeys {
		t.Run(key, func(t *testing.T) {
			clearCursorEnvironment(t)
			t.Setenv(key, "secret-or-endpoint")
			runner := &fakeRunner{}
			_, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
			typed, ok := usererr.As(err)
			if !ok || typed.ExitCode != exitcode.ProviderAuth || typed.Code != "cursor_auth_override" {
				t.Fatalf("error=%#v", err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("override reached subprocess: %d calls", len(runner.calls))
			}
		})
	}
}

func TestMissingCapabilitiesFailClosedBeforeModelCall(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{help: "--print --output-format"}
	diagnostic := (Adapter{Runner: runner}).Diagnose(context.Background())
	if diagnostic.Available || len(diagnostic.Capabilities) != 0 || !diagnostic.Authenticated {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	_, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.Code != "cursor_capabilities" {
		t.Fatalf("error=%#v", err)
	}
	for _, call := range runner.calls {
		if !reflect.DeepEqual(call.Args, []string{"--version"}) && !reflect.DeepEqual(call.Args, []string{"--help"}) && !reflect.DeepEqual(call.Args, []string{"status", "--format", "json"}) {
			t.Fatalf("model call made without safe capabilities: %v", call.Args)
		}
	}
}

func TestProviderContract(t *testing.T) {
	clearCursorEnvironment(t)
	contracttest.Run(t, contracttest.Cases{
		Provider: Adapter{Runner: &fakeRunner{}}, ID: llm.Cursor,
		Malformed: func(ctx context.Context) error {
			_, err := (Adapter{Runner: &fakeRunner{output: `{`}}).Translate(ctx, llm.TranslationRequest{})
			return err
		},
		Oversized: func(ctx context.Context) error {
			_, err := (Adapter{Runner: &fakeRunner{modelErr: processrunner.ErrOutputLimit}}).Translate(ctx, llm.TranslationRequest{})
			return err
		},
	})
}

func TestNonzeroCursorFailurePreservesBoundedSafeDetail(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{output: `{"type":"result","subtype":"error","is_error":true,"result":"Authentication required"}`, modelErr: fmt.Errorf("exit status 1")}
	_, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderAuth {
		t.Fatalf("error=%#v", err)
	}
	if rendered := usererr.Render(typed, false); !strings.Contains(rendered, "Authentication required") {
		t.Fatalf("failure detail missing:\n%s", rendered)
	}
}

func TestTrailingEnvelopeDataIsRejected(t *testing.T) {
	clearCursorEnvironment(t)
	valid := `{"type":"result","subtype":"success","is_error":false,"result":"{\"status\":\"ok\",\"command\":\"ls\",\"explanation\":\"Lists files.\",\"clarification\":\"\",\"assumptions\":[]}"}`
	_, err := (Adapter{Runner: &fakeRunner{output: valid + ` {}`}}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderMalformed {
		t.Fatalf("trailing output accepted: %#v", err)
	}
}

func TestDuplicateEnvelopeFieldsAreRejected(t *testing.T) {
	clearCursorEnvironment(t)
	output := `{"type":"result","subtype":"success","is_error":false,"result":"{}","result":"{\"status\":\"ok\",\"command\":\"ls\",\"explanation\":\"Lists files.\",\"clarification\":\"\",\"assumptions\":[]}"}`
	_, err := (Adapter{Runner: &fakeRunner{output: output}}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderMalformed {
		t.Fatalf("duplicate envelope field accepted: %#v", err)
	}
}
