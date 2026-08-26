package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agenticlab-ai/humansh/assets"
	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/exitcode"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/contracttest"
	"github.com/agenticlab-ai/humansh/internal/llm/providerutil"
	"github.com/agenticlab-ai/humansh/internal/processrunner"
	"github.com/agenticlab-ai/humansh/internal/prompt"
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

func clearClaudeEnvironment(t *testing.T) {
	t.Helper()
	for _, keys := range [][]string{claudeOverrideEnvKeys, claudeCredentialEnvKeys, claudeCredentialLocationEnvKeys, claudeUserIdentityEnvKeys} {
		for _, key := range keys {
			t.Setenv(key, "")
		}
	}
}

func (f *fakeRunner) Run(ctx context.Context, s processrunner.Spec) (processrunner.Result, error) {
	f.calls = append(f.calls, s)
	deadlineAt, hasDeadline := ctx.Deadline()
	f.deadlines = append(f.deadlines, hasDeadline)
	f.deadlineAt = append(f.deadlineAt, deadlineAt)
	if err := ctx.Err(); err != nil {
		return processrunner.Result{}, err
	}
	if reflect.DeepEqual(s.Args, []string{"-p", providerutil.ProbePrompt}) {
		output := f.probeOutput
		if output == "" {
			output = providerutil.ProbeMarker
		}
		return processrunner.Result{Stdout: []byte(output), Stderr: []byte(f.probeStderr)}, f.probeErr
	}
	output := f.output
	if output == "" {
		output = `{"structured_output":{"status":"ok","command":"ls","explanation":"Lists files.","clarification":"","assumptions":[]}}`
	}
	return processrunner.Result{Stdout: []byte(output)}, f.modelErr
}

func TestEveryProviderSubprocessIsTimedAndIsolated(t *testing.T) {
	clearClaudeEnvironment(t)
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

func TestTranslateDoesNotDependOnAuthStatusSubcommands(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{}
	adapter := Adapter{Runner: runner}
	if _, err := adapter.Translate(context.Background(), llm.TranslationRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || len(runner.calls[0].Args) == 0 || runner.calls[0].Args[0] == "auth" {
		t.Fatalf("translation unexpectedly depended on an auth subcommand: %+v", runner.calls)
	}
}

func TestMinimalProbeUsesOnlyPrintAndSurfacesManagedDistributionErrors(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{}
	diagnostic := (Adapter{Runner: runner}).Probe(context.Background())
	if !diagnostic.LiveCheck || !diagnostic.Available || !diagnostic.Authenticated || diagnostic.AuthMode != "provider_managed" {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0].Args, []string{"-p", providerutil.ProbePrompt}) {
		t.Fatalf("probe argv=%+v", runner.calls)
	}

	runner = &fakeRunner{
		probeStderr: "claude: error: Login disabled by ASBX toolbox distribution. No login required.",
		probeErr:    fmt.Errorf("exit status 1"),
	}
	diagnostic = (Adapter{Runner: runner}).Probe(context.Background())
	if diagnostic.Available || !diagnostic.LiveCheck || !strings.Contains(diagnostic.Message, "Login disabled by ASBX toolbox distribution") {
		t.Fatalf("failed diagnostic=%+v", diagnostic)
	}
	for _, action := range diagnostic.NextSteps {
		if strings.Contains(action.Command, "auth") || strings.Contains(action.Command, "login") {
			t.Fatalf("probe failure prescribed unsupported auth command: %+v", diagnostic.NextSteps)
		}
	}
}

func TestMissingConfiguredClaudeExecutableCanBeResetToPathSelection(t *testing.T) {
	clearClaudeEnvironment(t)
	missing := filepath.Join(t.TempDir(), "claude")
	diagnostic := (Adapter{Config: Config{Binary: missing}}).Diagnose(context.Background())
	if diagnostic.Installed || diagnostic.Executable != missing || !strings.Contains(diagnostic.Message, "was not found") {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	if len(diagnostic.NextSteps) != 2 || diagnostic.NextSteps[0].Command != "humansh config set providers.claude.binary auto" || diagnostic.NextSteps[1].Command != "humansh setup" {
		t.Fatalf("next steps=%+v", diagnostic.NextSteps)
	}
}

func TestAutomaticClaudeSelectionFindsNativeInstallBeforeShellPathRefresh(t *testing.T) {
	clearClaudeEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	launcher := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("native Claude launcher fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := (Adapter{}).binary(); got != launcher {
		t.Fatalf("automatic Claude binary=%q want native install %q", got, launcher)
	}
}

func TestClaudeProviderOAuthEnvironmentIsForwardedWithoutDisclosure(t *testing.T) {
	clearClaudeEnvironment(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-oauth-access-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "test-oauth-refresh-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_SCOPES", "user:profile user:inference")
	t.Setenv("GITHUB_TOKEN", "unrelated-secret")
	runner := &fakeRunner{}
	adapter := Adapter{Runner: runner}
	diagnostic := adapter.Probe(context.Background())
	if !diagnostic.Available {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	for index, call := range runner.calls {
		env := strings.Join(call.Env, "\n")
		for _, want := range []string{
			"CLAUDE_CODE_OAUTH_TOKEN=test-oauth-access-secret",
			"CLAUDE_CODE_OAUTH_REFRESH_TOKEN=test-oauth-refresh-secret",
			"CLAUDE_CODE_OAUTH_SCOPES=user:profile user:inference",
		} {
			if !strings.Contains(env, want) {
				t.Errorf("call %d environment missing %q", index, want)
			}
		}
		if strings.Contains(env, "GITHUB_TOKEN") {
			t.Errorf("call %d received unrelated secret", index)
		}
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"test-oauth-access-secret", "test-oauth-refresh-secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("diagnostic disclosed OAuth credential: %s", encoded)
		}
	}
}

func TestClaudeCredentialLocationsAndKeychainIdentityAreForwarded(t *testing.T) {
	clearClaudeEnvironment(t)
	locations := map[string]string{
		"ANTHROPIC_CONFIG_DIR":            filepath.Join(t.TempDir(), "anthropic"),
		"CLAUDE_CONFIG_DIR":               filepath.Join(t.TempDir(), "claude"),
		"CLAUDE_SECURESTORAGE_CONFIG_DIR": filepath.Join(t.TempDir(), "secure-storage"),
		"XDG_CONFIG_HOME":                 filepath.Join(t.TempDir(), "xdg"),
	}
	for key, value := range locations {
		t.Setenv(key, value)
	}
	t.Setenv("USER", "test-user")
	t.Setenv("LOGNAME", "test-login")
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "must-not-leak"))
	runner := &fakeRunner{}
	diagnostic := (Adapter{Runner: runner}).Probe(context.Background())
	if !diagnostic.Available {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	for index, call := range runner.calls {
		env := strings.Join(call.Env, "\n")
		for key, value := range locations {
			if !strings.Contains(env, key+"="+value) {
				t.Errorf("call %d environment missing %s", index, key)
			}
		}
		for _, want := range []string{"USER=test-user", "LOGNAME=test-login"} {
			if !strings.Contains(env, want) {
				t.Errorf("call %d environment missing %q", index, want)
			}
		}
		if strings.Contains(env, "XDG_DATA_HOME") || strings.Contains(env, "must-not-leak") {
			t.Errorf("call %d received an unrelated XDG location: %v", index, call.Env)
		}
	}
}

func TestClaudeRelativeCredentialLocationsAreNotForwarded(t *testing.T) {
	clearClaudeEnvironment(t)
	for _, key := range claudeCredentialLocationEnvKeys {
		t.Setenv(key, "project-controlled-relative-path")
	}
	runner := &fakeRunner{}
	(Adapter{Runner: runner}).Probe(context.Background())
	for index, call := range runner.calls {
		env := strings.Join(call.Env, "\n")
		for _, key := range claudeCredentialLocationEnvKeys {
			if strings.Contains(env, key+"=") {
				t.Errorf("call %d forwarded relative %s: %v", index, key, call.Env)
			}
		}
	}
}

func TestSafeStructuredInvocation(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{}
	adapter := Adapter{Runner: runner}
	response, err := adapter.Translate(context.Background(), llm.TranslationRequest{Input: "MARKER_SECRET", Shell: "zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Command != "ls" {
		t.Fatalf("response=%+v", response)
	}
	call := runner.calls[0]
	wireSchema, err := claudeWireSchema()
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"--safe-mode", "-p", prompt.Instruction + "\nRead the request object from stdin.", "--output-format", "json", "--json-schema", wireSchema, "--tools", "", "--disallowedTools", "mcp__*", "--permission-mode", "dontAsk", "--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--max-turns", claudeMaxTurns}
	if !reflect.DeepEqual(call.Args, wantArgs) {
		t.Fatalf("Claude argv drifted:\n got: %#v\nwant: %#v", call.Args, wantArgs)
	}
	joined := strings.Join(call.Args, " ")
	for _, required := range []string{"--safe-mode", "--tools ", "--disallowedTools mcp__*", "--permission-mode dontAsk", "--no-chrome", "--no-session-persistence", "--max-turns " + claudeMaxTurns} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing %q in %s", required, joined)
		}
	}
	if slices.Contains(call.Args, "*") {
		t.Fatalf("blanket tool denial also removes Claude's required StructuredOutput mechanism: %v", call.Args)
	}
	if strings.Contains(joined, "--bare") || strings.Contains(joined, "MARKER_SECRET") {
		t.Fatalf("unsafe argv: %s", joined)
	}
	if !strings.Contains(string(call.Stdin), "MARKER_SECRET") {
		t.Fatal("dynamic request not on stdin")
	}
	for _, env := range call.Env {
		if strings.HasPrefix(env, "ANTHROPIC_") || strings.HasPrefix(env, "CLAUDE_CODE_USE_") {
			t.Fatalf("override leaked: %s", env)
		}
	}
}

func TestClaudeWireSchemaOmitsOnlyUnsupportedDialectMetadata(t *testing.T) {
	wireJSON, err := claudeWireSchema()
	if err != nil {
		t.Fatal(err)
	}
	var canonical, wire map[string]any
	if err := json.Unmarshal(assets.TranslationSchema, &canonical); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wireJSON), &wire); err != nil {
		t.Fatal(err)
	}
	if _, exists := wire["$schema"]; exists {
		t.Fatalf("Claude wire schema still declares an unsupported dialect: %s", wireJSON)
	}
	delete(canonical, "$schema")
	if !reflect.DeepEqual(wire, canonical) {
		t.Fatalf("Claude wire schema changed response constraints:\n got: %#v\nwant: %#v", wire, canonical)
	}
}

func TestParentAPIOverrideIsNotForwarded(t *testing.T) {
	clearClaudeEnvironment(t)
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	runner := &fakeRunner{}
	adapter := Adapter{Runner: runner}
	if _, err := adapter.Translate(context.Background(), llm.TranslationRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("model subprocess calls=%d", len(runner.calls))
	}
	if env := strings.Join(runner.calls[0].Env, "\n"); strings.Contains(env, "ANTHROPIC_API_KEY") || strings.Contains(env, "secret") {
		t.Fatalf("parent API override leaked to Claude: %v", runner.calls[0].Env)
	}
}

func TestProviderContract(t *testing.T) {
	clearClaudeEnvironment(t)
	contracttest.Run(t, contracttest.Cases{
		Provider: Adapter{Runner: &fakeRunner{}},
		ID:       llm.Claude,
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

func TestOutputLimitIsMalformedProviderResponse(t *testing.T) {
	clearClaudeEnvironment(t)
	_, err := (Adapter{Runner: &fakeRunner{modelErr: processrunner.ErrOutputLimit}}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderMalformed {
		t.Fatalf("error=%#v", err)
	}
}

func TestNonzeroClaudeJSONFailureIsShownAndClassified(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{
		output:   `{"is_error":true,"terminal_reason":"api_error","result":"Not logged in · Please run /login"}`,
		modelErr: fmt.Errorf("exit status 1"),
	}
	_, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderAuth || typed.Code != "claude_translation_auth" {
		t.Fatalf("error=%#v", err)
	}
	for _, want := range []string{"Not logged in", "provider-managed authentication", "humansh provider test claude"} {
		if !strings.Contains(usererr.Render(typed, false), want) {
			t.Errorf("normal error missing %q:\n%s", want, usererr.Render(typed, false))
		}
	}
	debug := usererr.Render(typed, true)
	for _, want := range []string{"exit status 1", "Not logged in"} {
		if !strings.Contains(debug, want) {
			t.Errorf("debug error missing %q:\n%s", want, debug)
		}
	}
}

func TestNonzeroClaudeFailurePreservesSafeStderrDetail(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &stderrClaudeRunner{stderr: "service handshake failed\nplease retry", err: fmt.Errorf("exit status 1")}
	_, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderTemporary {
		t.Fatalf("error=%#v", err)
	}
	if rendered := usererr.Render(typed, false); !strings.Contains(rendered, "service handshake failed please retry") {
		t.Fatalf("provider detail was discarded:\n%s", rendered)
	}
}

func TestTrailingClaudeEnvelopeDataIsRejected(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &staticClaudeRunner{output: `{"structured_output":{"status":"ok","command":"ls","explanation":"Lists files.","clarification":"","assumptions":[]}} {}`}
	_, err := (Adapter{Runner: runner}).Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderMalformed {
		t.Fatalf("trailing output accepted: %#v", err)
	}
}

type staticClaudeRunner struct{ output string }

func (r *staticClaudeRunner) Run(_ context.Context, _ processrunner.Spec) (processrunner.Result, error) {
	return processrunner.Result{Stdout: []byte(r.output)}, nil
}

type stderrClaudeRunner struct {
	stderr string
	err    error
}

func (r *stderrClaudeRunner) Run(_ context.Context, _ processrunner.Spec) (processrunner.Result, error) {
	return processrunner.Result{Stderr: []byte(r.stderr)}, r.err
}
