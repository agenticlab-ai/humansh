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
	"github.com/agenticlab-ai/humansh/internal/processrunner"
	"github.com/agenticlab-ai/humansh/internal/prompt"
)

type fakeRunner struct {
	calls      []processrunner.Spec
	deadlines  []bool
	deadlineAt []time.Time
	auth       string
	version    string
	help       string
	output     string
	probeErr   error
	modelErr   error
}

func clearClaudeEnvironment(t *testing.T) {
	t.Helper()
	for _, keys := range [][]string{claudeOverrideEnvKeys, claudeSubscriptionEnvKeys, claudeCredentialLocationEnvKeys, claudeUserIdentityEnvKeys} {
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
	if len(s.Args) == 2 && s.Args[0] == "-p" && s.Args[1] == "--version" {
		version := f.version
		if version == "" {
			version = "2.1.238 (Claude Code)\n"
		}
		return processrunner.Result{Stdout: []byte(version)}, nil
	}
	if len(s.Args) == 4 && s.Args[0] == "-p" && s.Args[1] == "--max-turns" && s.Args[2] == claudeMaxTurns && s.Args[3] == "--help" {
		help := f.help
		if help == "" {
			help = strings.Join(requiredHelpOptions, " ")
		}
		return processrunner.Result{Stdout: []byte(help)}, f.probeErr
	}
	if len(s.Args) >= 2 && s.Args[0] == "auth" {
		auth := f.auth
		if auth == "" {
			auth = `{"loggedIn":true,"authMethod":"oauth"}`
		}
		return processrunner.Result{Stdout: []byte(auth)}, nil
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
		t.Fatalf("diagnostic/model isolation directories are wrong: %q %q %q %q", runner.calls[0].Dir, runner.calls[1].Dir, runner.calls[2].Dir, runner.calls[3].Dir)
	}
	if !runner.deadlineAt[3].After(runner.deadlineAt[2]) {
		t.Fatalf("model deadline %v did not receive a fresh budget after diagnostics ending at %v", runner.deadlineAt[3], runner.deadlineAt[2])
	}
}

func TestContradictoryAPIAuthIsRejected(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{auth: `{"loggedIn":true,"authMethod":"oauth","billingMode":"api"}`}
	adapter := Adapter{Runner: runner}
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderAuth || typed.Code != "claude_metered_auth" {
		t.Fatalf("error=%#v", err)
	}
}

// TestSafeModeAndNewerClaudeVersionsStayAvailable guards the feature floor
// against regressing to the later interface-review baseline. Claude Code
// auto-updates, so pinning a single reviewed release would reject compatible
// versions both immediately before and after it.
func TestSafeModeAndNewerClaudeVersionsStayAvailable(t *testing.T) {
	clearClaudeEnvironment(t)
	for _, reported := range []string{"2.1.169 (Claude Code)", "2.1.235 (Claude Code)", "2.1.236 (Claude Code)", "2.1.238 (Claude Code)", "2.2.0 (Claude Code)", "3.0.0 (Claude Code)"} {
		adapter := Adapter{Runner: &fakeRunner{version: reported + "\n"}}
		diagnostic := adapter.Diagnose(context.Background())
		if !diagnostic.Available || len(diagnostic.Capabilities) == 0 {
			t.Fatalf("version %q should remain available: %+v", reported, diagnostic)
		}
	}
}

// TestUnreadableClaudeVersionFallsBackToProbes keeps an unparseable version
// string from disabling a CLI whose capabilities all probe correctly.
func TestUnreadableClaudeVersionFallsBackToProbes(t *testing.T) {
	clearClaudeEnvironment(t)
	adapter := Adapter{Runner: &fakeRunner{version: "claude (development build)\n"}}
	diagnostic := adapter.Diagnose(context.Background())
	if !diagnostic.Available || !strings.Contains(diagnostic.Message, "could not be read") {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
}

func TestUnqualifiedClaudeVersionFailsClosedBeforeModelCall(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{version: "2.1.168 (Claude Code)\n"}
	adapter := Adapter{Runner: runner}
	diagnostic := adapter.Diagnose(context.Background())
	if !diagnostic.Authenticated || diagnostic.Available || len(diagnostic.Capabilities) != 0 || !strings.Contains(diagnostic.Message, "predates Claude Code 2.1.169") {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	if len(diagnostic.NextSteps) != 2 || diagnostic.NextSteps[0].Command != "claude update" || diagnostic.NextSteps[1].Command != "humansh setup" {
		t.Fatalf("next steps=%+v", diagnostic.NextSteps)
	}
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	typed, ok := usererr.As(err)
	if !ok || typed.ExitCode != exitcode.ProviderUnavailable || typed.Code != "claude_capabilities" {
		t.Fatalf("error=%#v", err)
	}
	for _, call := range runner.calls {
		diagnosticCall := len(call.Args) == 2 && call.Args[0] == "-p" && call.Args[1] == "--version" ||
			len(call.Args) == 4 && call.Args[0] == "-p" && call.Args[1] == "--max-turns" && call.Args[2] == claudeMaxTurns && call.Args[3] == "--help" ||
			len(call.Args) > 0 && call.Args[0] == "auth"
		if !diagnosticCall {
			t.Fatalf("model call made with unqualified capabilities: %v", call.Args)
		}
	}
}

func TestClaudeDiagnosticReportsVersionAndLoginFailuresTogether(t *testing.T) {
	clearClaudeEnvironment(t)
	runner := &fakeRunner{version: "2.1.168 (Claude Code)\n", auth: `{"loggedIn":false,"authMethod":"none"}`}
	diagnostic := (Adapter{Runner: runner}).Diagnose(context.Background())
	if diagnostic.Authenticated || diagnostic.Available || len(diagnostic.Capabilities) != 0 {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	for _, want := range []string{"version 2.1.168", "fresh Claude Code processes report logged out"} {
		if !strings.Contains(diagnostic.Message, want) {
			t.Errorf("diagnostic message missing %q: %q", want, diagnostic.Message)
		}
	}
	commands := make([]string, 0, len(diagnostic.NextSteps))
	for _, action := range diagnostic.NextSteps {
		commands = append(commands, action.Command)
	}
	if !reflect.DeepEqual(commands, []string{"claude update", "claude auth login --claudeai", "humansh setup"}) {
		t.Fatalf("next-step commands=%v", commands)
	}
}

func TestClaudeDiagnosticCommandSafelyNamesExactExecutable(t *testing.T) {
	tests := map[string]string{
		"":                          "claude",
		"/Users/test/.local/claude": "/Users/test/.local/claude",
		"/tmp/Claude Code/claude":   `'/tmp/Claude Code/claude'`,
		"/tmp/it's/claude":          `'/tmp/it'"'"'s/claude'`,
	}
	for executable, want := range tests {
		if got := claudeDiagnosticCommand(executable); got != want {
			t.Errorf("claudeDiagnosticCommand(%q)=%q want %q", executable, got, want)
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

func TestClaudeSubscriptionOAuthEnvironmentIsForwardedWithoutDisclosure(t *testing.T) {
	clearClaudeEnvironment(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-oauth-access-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "test-oauth-refresh-secret")
	t.Setenv("CLAUDE_CODE_OAUTH_SCOPES", "user:profile user:inference")
	t.Setenv("GITHUB_TOKEN", "unrelated-secret")
	runner := &fakeRunner{auth: `{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}`}
	adapter := Adapter{Runner: runner}
	diagnostic := adapter.Diagnose(context.Background())
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
	diagnostic := (Adapter{Runner: runner}).Diagnose(context.Background())
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
	(Adapter{Runner: runner}).Diagnose(context.Background())
	for index, call := range runner.calls {
		env := strings.Join(call.Env, "\n")
		for _, key := range claudeCredentialLocationEnvKeys {
			if strings.Contains(env, key+"=") {
				t.Errorf("call %d forwarded relative %s: %v", index, key, call.Env)
			}
		}
	}
}

func TestParseAuthFixtures(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"oauth", `{"loggedIn":true,"authMethod":"oauth"}`, "claude.ai"},
		{"oauth-token", `{"loggedIn":true,"authMethod":"oauth_token","apiProvider":"firstParty"}`, "claude.ai"},
		{"claude-ai", `{"authenticated":true,"provider":"claude.ai subscription"}`, "claude.ai"},
		{"api-key-none-is-not-api", `{"loggedIn":true,"authMethod":"oauth","apiKeySource":"none"}`, "claude.ai"},
		{"contradictory-api-wins", `{"loggedIn":true,"authMethod":"oauth","billingMode":"api"}`, "api"},
		{"bedrock", `{"loggedIn":true,"provider":"bedrock"}`, "api"},
		{"logged-out", `{"loggedIn":false}`, "logged_out"},
		{"malformed", `{`, "unknown"},
		{"unrecognized", `{"loggedIn":true,"authMethod":"future-mode"}`, "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseAuth([]byte(test.json)); got != test.want {
				t.Fatalf("parseAuth(%s)=%q want %q", test.json, got, test.want)
			}
		})
	}
}

func TestMaxTurnsCapabilityUsesParserProbeInsteadOfHelpText(t *testing.T) {
	clearClaudeEnvironment(t)
	helpWithoutMaxTurns := strings.Join(requiredHelpOptions, " ")
	accepted := Adapter{Runner: &fakeRunner{help: helpWithoutMaxTurns}}
	if diagnostic := accepted.Diagnose(context.Background()); !diagnostic.Available {
		t.Fatalf("parser-accepted --max-turns was rejected because help omitted it: %+v", diagnostic)
	}
	rejected := Adapter{Runner: &fakeRunner{help: helpWithoutMaxTurns, probeErr: fmt.Errorf("unknown option --max-turns")}}
	if diagnostic := rejected.Diagnose(context.Background()); diagnostic.Available || len(diagnostic.Capabilities) != 0 {
		t.Fatalf("parser-rejected --max-turns was accepted: %+v", diagnostic)
	}
}

func TestSafeSubscriptionInvocation(t *testing.T) {
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
	call := runner.calls[3]
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

func TestParentAPIOverrideRejectedBeforeModelCall(t *testing.T) {
	clearClaudeEnvironment(t)
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	runner := &fakeRunner{}
	adapter := Adapter{Runner: runner}
	_, err := adapter.Translate(context.Background(), llm.TranslationRequest{})
	if err == nil {
		t.Fatal("override accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("model/auth subprocess called %d times", len(runner.calls))
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
	for _, want := range []string{"Not logged in", "did not accept the login", "claude auth login --claudeai", "humansh provider test claude"} {
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

func (r *staticClaudeRunner) Run(_ context.Context, spec processrunner.Spec) (processrunner.Result, error) {
	if len(spec.Args) == 2 && spec.Args[0] == "-p" && spec.Args[1] == "--version" {
		return processrunner.Result{Stdout: []byte("2.1.238 (Claude Code)")}, nil
	}
	if len(spec.Args) == 4 && spec.Args[0] == "-p" && spec.Args[1] == "--max-turns" && spec.Args[2] == claudeMaxTurns && spec.Args[3] == "--help" {
		return processrunner.Result{Stdout: []byte(strings.Join(requiredHelpOptions, " "))}, nil
	}
	if len(spec.Args) > 0 && spec.Args[0] == "auth" {
		return processrunner.Result{Stdout: []byte(`{"loggedIn":true,"authMethod":"oauth"}`)}, nil
	}
	return processrunner.Result{Stdout: []byte(r.output)}, nil
}

type stderrClaudeRunner struct {
	stderr string
	err    error
}

func (r *stderrClaudeRunner) Run(_ context.Context, spec processrunner.Spec) (processrunner.Result, error) {
	if len(spec.Args) == 2 && spec.Args[0] == "-p" && spec.Args[1] == "--version" {
		return processrunner.Result{Stdout: []byte("2.1.238 (Claude Code)")}, nil
	}
	if len(spec.Args) == 4 && spec.Args[0] == "-p" && spec.Args[1] == "--max-turns" && spec.Args[2] == claudeMaxTurns && spec.Args[3] == "--help" {
		return processrunner.Result{Stdout: []byte(strings.Join(requiredHelpOptions, " "))}, nil
	}
	if len(spec.Args) > 0 && spec.Args[0] == "auth" {
		return processrunner.Result{Stdout: []byte(`{"loggedIn":true,"authMethod":"oauth"}`)}, nil
	}
	return processrunner.Result{Stderr: []byte(r.stderr)}, r.err
}
