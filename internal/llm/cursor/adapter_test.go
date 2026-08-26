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
	"github.com/agenticlab-ai/humansh/internal/llm/providerutil"
	"github.com/agenticlab-ai/humansh/internal/processrunner"
)

type fakeRunner struct {
	calls       []processrunner.Spec
	deadlines   []bool
	deadlineAt  []time.Time
	probeOutput string
	probeStderr string
	probeErr    error
	output      string
	modelErr    error
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
	if reflect.DeepEqual(spec.Args, []string{"-p", providerutil.ProbePrompt}) {
		value := f.probeOutput
		if value == "" {
			value = providerutil.ProbeMarker
		}
		return processrunner.Result{Stdout: []byte(value), Stderr: []byte(f.probeStderr)}, f.probeErr
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
	if len(runner.calls) != 1 || len(runner.deadlines) != 1 {
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
	call := runner.calls[0]
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
	if got := runner.calls[0].Args; !reflect.DeepEqual(got[len(got)-2:], []string{"--model", "cursor-model"}) {
		t.Fatalf("model args=%v", got)
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
	diagnostic := (Adapter{Runner: runner}).Probe(context.Background())
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

func TestParentCursorOverridesAreNotForwarded(t *testing.T) {
	for _, key := range cursorOverrideEnvKeys {
		t.Run(key, func(t *testing.T) {
			clearCursorEnvironment(t)
			t.Setenv(key, "secret-or-endpoint")
			runner := &fakeRunner{}
			if _, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{}); err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("model subprocess calls=%d", len(runner.calls))
			}
			if env := strings.Join(runner.calls[0].Env, "\n"); strings.Contains(env, key+"=") || strings.Contains(env, "secret-or-endpoint") {
				t.Fatalf("override reached subprocess: %v", runner.calls[0].Env)
			}
		})
	}
}

func TestMinimalProbeUsesOnlyPrintAndSurfacesProviderErrors(t *testing.T) {
	clearCursorEnvironment(t)
	runner := &fakeRunner{}
	diagnostic := (Adapter{Runner: runner}).Probe(context.Background())
	if !diagnostic.LiveCheck || !diagnostic.Available || !diagnostic.Authenticated || diagnostic.AuthMode != "provider_managed" {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0].Args, []string{"-p", providerutil.ProbePrompt}) {
		t.Fatalf("probe argv=%+v", runner.calls)
	}

	runner = &fakeRunner{probeStderr: "Authentication is managed by your organization", probeErr: fmt.Errorf("exit status 1")}
	diagnostic = (Adapter{Runner: runner}).Probe(context.Background())
	if diagnostic.Available || !diagnostic.LiveCheck || !strings.Contains(diagnostic.Message, "Authentication is managed by your organization") {
		t.Fatalf("failed diagnostic=%+v", diagnostic)
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
