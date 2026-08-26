package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agenticlab-ai/humansh/assets"
	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/providerutil"
	"github.com/agenticlab-ai/humansh/internal/processrunner"
	"github.com/agenticlab-ai/humansh/internal/prompt"
)

type Config struct {
	Binary, Model string
	Timeout       time.Duration
}
type Adapter struct {
	Config Config
	Runner processrunner.Runner
}

type claudeEnvelope struct {
	StructuredOutput json.RawMessage `json:"structured_output"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	TerminalReason   string          `json:"terminal_reason"`
}

// Claude's structured-output implementation uses an internal StructuredOutput
// tool round trip and may need a validation retry. One turn terminates before
// the validated result envelope is emitted; three remains tightly bounded
// while allowing the documented structured-output workflow to complete.
const claudeMaxTurns = "3"

var claudeOverrideEnvKeys = []string{
	"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
}

// These are Claude provider credentials documented for SDK and automated
// environments. They are forwarded only to Claude Code, never added to the
// generic minimal environment shared by other providers.
var claudeCredentialEnvKeys = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES",
}

// Claude Code resolves provider-managed credentials through configurable
// storage roots as well as HOME. The isolated humansh subprocess must receive
// the same roots or it can incorrectly use a different credential store. Only
// absolute paths are forwarded so isolation cannot be redirected relative to
// the request working directory.
var claudeCredentialLocationEnvKeys = []string{
	"ANTHROPIC_CONFIG_DIR", "CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR", "XDG_CONFIG_HOME",
}

var claudeUserIdentityEnvKeys = []string{"USER", "LOGNAME"}

func (Adapter) ID() llm.ProviderID { return llm.Claude }

func (a Adapter) Diagnose(context.Context) llm.Diagnostic {
	executable, found := a.discoverBinary()
	if !found {
		diagnostic := llm.Diagnostic{Configured: a.Config.Binary != "", AuthMode: "provider_managed", Message: "Claude Code is not installed"}
		if a.Config.Binary != "" {
			diagnostic.Executable = a.Config.Binary
			diagnostic.Message = fmt.Sprintf("selected Claude Code executable %q was not found", a.Config.Binary)
			diagnostic.NextSteps = []llm.DiagnosticAction{
				{Description: "Restore automatic Claude executable selection", Command: "humansh config set providers.claude.binary auto"},
				{Description: "Choose and recheck Claude Code", Command: "humansh setup"},
			}
		} else {
			diagnostic.NextSteps = []llm.DiagnosticAction{{Description: "Install Claude Code", Command: "curl -fsSL https://claude.ai/install.sh | bash"}}
		}
		return diagnostic
	}
	return llm.Diagnostic{
		Installed: true, Configured: true, AuthMode: "provider_managed", Executable: executable,
		Message:   "Claude Code is installed; live inference has not been checked",
		NextSteps: []llm.DiagnosticAction{{Description: "Run a live provider check", Command: "humansh provider test claude"}},
	}
}

func (a Adapter) Probe(ctx context.Context) llm.Diagnostic {
	base := a.Diagnose(ctx)
	if !base.Installed && a.Runner == nil {
		return base
	}
	tempDir, err := os.MkdirTemp("", "humansh-claude-probe-*")
	if err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Claude Code", err))
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Claude Code", err))
	}
	probeCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	result, runErr := a.runner().Run(probeCtx, processrunner.Spec{
		Path: a.binary(), Args: []string{"-p", providerutil.ProbePrompt}, Dir: tempDir, Env: claudeRuntimeEnv(tempDir),
		MaxStdout: 64 << 10, MaxStderr: 64 << 10,
	})
	return providerutil.ProbeDiagnostic(base, llm.Claude, a.timeout(), result, runErr)
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
	tempDir, err := os.MkdirTemp("", "humansh-claude-*")
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Claude Code", err)
	}
	defer os.RemoveAll(tempDir)
	_ = os.Chmod(tempDir, 0o700)
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("encode request", err)
	}
	wireSchema, err := claudeWireSchema()
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("build Claude wire schema", err)
	}
	args := []string{"--safe-mode", "-p", prompt.Instruction + "\nRead the request object from stdin.", "--output-format", "json", "--json-schema", wireSchema, "--tools", "", "--disallowedTools", "mcp__*", "--permission-mode", "dontAsk", "--disable-slash-commands", "--no-chrome", "--no-session-persistence", "--max-turns", claudeMaxTurns}
	if a.Config.Model != "" {
		args = append(args, "--model", a.Config.Model)
	}
	callCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	result, runErr := a.runner().Run(callCtx, processrunner.Spec{Path: a.binary(), Args: args, Stdin: requestJSON, Dir: tempDir, Env: claudeRuntimeEnv(tempDir), MaxStdout: 1 << 20, MaxStderr: 1 << 20})
	if runErr != nil {
		if processrunner.IsNotFound(runErr) {
			return llm.TranslationResponse{}, providerutil.Missing("Claude Code", "curl -fsSL https://claude.ai/install.sh | bash", "")
		}
		if processrunner.IsOutputLimit(runErr) {
			return llm.TranslationResponse{}, providerutil.Malformed("Claude Code output exceeded the capture limit", runErr)
		}
		return llm.TranslationResponse{}, a.mapCLIError(result, runErr)
	}
	var envelope claudeEnvelope
	decoder := json.NewDecoder(bytes.NewReader(result.Stdout))
	if err := decoder.Decode(&envelope); err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("Claude output envelope", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return llm.TranslationResponse{}, providerutil.Malformed("Claude output envelope contained trailing data", err)
	}
	if envelope.IsError {
		return llm.TranslationResponse{}, a.mapCLIError(result, fmt.Errorf("Claude Code returned an error result"))
	}
	if len(envelope.StructuredOutput) == 0 {
		return llm.TranslationResponse{}, providerutil.Malformed("Claude structured_output is missing", nil)
	}
	return providerutil.DecodeResponse(envelope.StructuredOutput)
}

// claudeWireSchema derives Claude's inline schema from the canonical embedded
// contract. Claude Code's --json-schema validator does not load the canonical
// Draft 2020-12 meta-schema URI, but every validation keyword humansh uses is
// supported without that dialect declaration. Keep all actual constraints and
// omit only the root metadata field at the CLI boundary.
func claudeWireSchema() (string, error) {
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(assets.TranslationSchema, &schema); err != nil {
		return "", err
	}
	delete(schema, "$schema")
	encoded, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// mapCLIError keeps Claude's structured error result when the CLI exits
// nonzero. Claude emits useful failures (including login failures) as a JSON
// envelope on stdout, so reporting only exec.ExitError loses the actual cause.
func (a Adapter) mapCLIError(result processrunner.Result, runErr error) error {
	detail := claudeFailureDetail(result)
	cause := runErr
	if detail != "" {
		cause = fmt.Errorf("%w: Claude Code reported: %s", runErr, detail)
	}
	mapped := providerutil.MapCLIError(llm.Claude, a.timeout(), []byte(detail), cause)
	typed, ok := usererr.As(mapped)
	if !ok || detail == "" {
		return mapped
	}
	typed.Summary = "Claude Code reported: " + detail + "\nNothing was changed or executed."
	if typed.Code == "provider_auth" && isClaudeLoginFailure(detail) {
		typed.Code = "claude_translation_auth"
		typed.Title = "Claude Code's translation process could not use its provider-managed authentication."
		typed.Fixes = []usererr.Fix{
			{Description: "Check the exact provider error, then retry with", Command: "humansh provider test claude"},
		}
	}
	return mapped
}

func claudeFailureDetail(result processrunner.Result) string {
	var envelope claudeEnvelope
	if json.Unmarshal(bytes.TrimSpace(result.Stdout), &envelope) == nil {
		if detail := cleanClaudeFailureDetail(envelope.Result); detail != "" {
			return detail
		}
		if detail := cleanClaudeFailureDetail(envelope.TerminalReason); detail != "" {
			return "terminal reason: " + detail
		}
	}
	// Claude normally writes its structured failure to stdout. Stderr is a
	// bounded fallback for startup/parser/network errors that happen before the
	// JSON envelope is created. Never echo an unparsed stdout payload because it
	// may contain a copy of the translation request.
	return cleanClaudeFailureDetail(string(result.Stderr))
}

func cleanClaudeFailureDetail(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = strings.Join(strings.Fields(usererr.RedactDebug(value)), " ")
	const maxRunes = 400
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes]) + "…"
	}
	return value
}

func isClaudeLoginFailure(detail string) bool {
	detail = strings.ToLower(detail)
	return strings.Contains(detail, "not logged in") || strings.Contains(detail, "please run /login") || strings.Contains(detail, "sign in")
}

func (a Adapter) runner() processrunner.Runner {
	if a.Runner != nil {
		return a.Runner
	}
	return processrunner.ExecRunner{}
}
func (a Adapter) binary() string {
	if a.Config.Binary != "" {
		return a.Config.Binary
	}
	if a.Runner != nil {
		return "claude"
	}
	if resolved, err := exec.LookPath("claude"); err == nil {
		if filepath.IsAbs(resolved) {
			return resolved
		}
		if absolute, absoluteErr := filepath.Abs(resolved); absoluteErr == nil {
			return absolute
		}
	}
	// Claude's native installer places the launcher here. Setup may be running
	// from a shell whose PATH has not yet picked up ~/.local/bin, especially
	// immediately after a CLI self-update moved an older npm/Homebrew install.
	// Use only this fixed per-user location; aliases and arbitrary shell startup
	// evaluation remain intentionally out of scope.
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", "claude")
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	return "claude"
}

func (a Adapter) discoverBinary() (string, bool) {
	if a.Runner != nil {
		return "", true
	}
	command := a.binary()
	if filepath.IsAbs(command) {
		info, err := os.Stat(command)
		return command, err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", false
	}
	if filepath.IsAbs(resolved) {
		return resolved, true
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return resolved, true
	}
	return absolute, true
}

func claudeRuntimeEnv(tempDir string) []string {
	extra := make(map[string]string, len(claudeCredentialEnvKeys)+len(claudeCredentialLocationEnvKeys)+len(claudeUserIdentityEnvKeys))
	for _, key := range claudeCredentialEnvKeys {
		if value := os.Getenv(key); value != "" {
			extra[key] = value
		}
	}
	for _, key := range claudeCredentialLocationEnvKeys {
		if value := os.Getenv(key); filepath.IsAbs(value) {
			extra[key] = value
		}
	}
	for _, key := range claudeUserIdentityEnvKeys {
		if value := os.Getenv(key); value != "" && !strings.ContainsAny(value, "\x00\r\n=") {
			extra[key] = value
		}
	}
	return processrunner.MinimalEnv(tempDir, extra)
}
func (a Adapter) timeout() time.Duration {
	if a.Config.Timeout > 0 {
		return a.Config.Timeout
	}
	return 20 * time.Second
}
