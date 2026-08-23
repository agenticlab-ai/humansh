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

	"github.com/humansh/humansh/assets"
	usererr "github.com/humansh/humansh/internal/errors"
	"github.com/humansh/humansh/internal/exitcode"
	"github.com/humansh/humansh/internal/llm"
	"github.com/humansh/humansh/internal/llm/providerutil"
	"github.com/humansh/humansh/internal/processrunner"
	"github.com/humansh/humansh/internal/prompt"
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

// minimumVersion is the first official Claude Code release with --safe-mode.
// Every other isolation control is also probed directly, so the later release
// used for humansh's interface review is a test baseline rather than an
// artificial compatibility floor.
var minimumVersion = [3]int{2, 1, 169}

var requiredHelpOptions = []string{
	"--safe-mode", "--print", "--output-format", "--json-schema", "--tools", "--disallowedTools",
	"--permission-mode", "--disable-slash-commands", "--no-chrome", "--no-session-persistence",
}

var observedCapabilities = []string{
	"safe-mode", "strict-structured-output", "tools-disabled", "session-persistence-disabled", "bounded-turns",
}

var claudeOverrideEnvKeys = []string{
	"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
}

// These are Claude subscription credentials documented for SDK and automated
// environments. They are forwarded only to Claude Code, never added to the
// generic minimal environment shared by other providers.
var claudeSubscriptionEnvKeys = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_SCOPES",
}

// Claude Code resolves subscription credentials through configurable storage
// roots as well as HOME. A direct `claude auth status` sees these values from
// the login shell; the isolated humansh subprocess must receive the same roots
// or it can incorrectly inspect a different, logged-out credential store.
// Only absolute paths are forwarded so isolation cannot be redirected relative
// to the request working directory.
var claudeCredentialLocationEnvKeys = []string{
	"ANTHROPIC_CONFIG_DIR", "CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR", "XDG_CONFIG_HOME",
}

var claudeUserIdentityEnvKeys = []string{"USER", "LOGNAME"}

func (Adapter) ID() llm.ProviderID { return llm.Claude }

func (a Adapter) Diagnose(ctx context.Context) llm.Diagnostic {
	for _, key := range claudeOverrideEnvKeys {
		if os.Getenv(key) != "" {
			return llm.Diagnostic{Installed: true, Configured: true, AuthMode: "override", Message: key + " overrides claude.ai subscription authentication"}
		}
	}
	diagnosticCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	tempDir, err := os.MkdirTemp("", "humansh-claude-diagnose-*")
	if err != nil {
		return llm.Diagnostic{Message: "Claude Code diagnostic isolation could not be created"}
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return llm.Diagnostic{Message: "Claude Code diagnostic isolation could not be secured"}
	}
	runner := a.runner()
	env := claudeRuntimeEnv(tempDir)
	binary, executable := a.diagnosticBinary()
	diagnosticCommand := claudeDiagnosticCommand(executable)
	version, versionErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"-p", "--version"}, Dir: tempDir, Env: env, MaxStdout: 4096, MaxStderr: 4096})
	if processrunner.IsNotFound(versionErr) {
		if executable != "" {
			return llm.Diagnostic{
				Configured: true,
				Executable: executable,
				Message:    fmt.Sprintf("selected Claude Code executable %q was not found", executable),
				NextSteps: []llm.DiagnosticAction{
					{Description: "Restore automatic Claude executable selection", Command: "humansh config set providers.claude.binary auto"},
					{Description: "Choose and recheck Claude Code", Command: "humansh setup"},
				},
			}
		}
		return llm.Diagnostic{Message: "Claude Code is not installed", NextSteps: []llm.DiagnosticAction{{Description: "Install Claude Code", Command: "curl -fsSL https://claude.ai/install.sh | bash"}}}
	}
	reportedVersion := strings.TrimSpace(string(version.Stdout))
	help, helpErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"-p", "--max-turns", claudeMaxTurns, "--help"}, Dir: tempDir, Env: env, MaxStdout: 64 << 10, MaxStderr: 64 << 10})
	probesOK := versionErr == nil && helpErr == nil && containsAll(string(help.Stdout)+"\n"+string(help.Stderr), requiredHelpOptions)
	meetsFloor, versionParsed := providerutil.VersionFloor(reportedVersion, minimumVersion)
	// An unreadable version string must not disable a CLI whose capabilities all
	// probe correctly; doctor reports the unknown version instead.
	capabilitiesOK := probesOK && (meetsFloor || !versionParsed)
	status, err := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"auth", "status", "--json"}, Dir: tempDir, Env: env, MaxStdout: 64 << 10, MaxStderr: 64 << 10})
	auth := parseAuth(status.Stdout)
	authenticated := err == nil && auth == "claude.ai"
	var problems []string
	var nextSteps []llm.DiagnosticAction
	if !capabilitiesOK {
		if probesOK && versionParsed && !meetsFloor {
			problems = append(problems, fmt.Sprintf("version %s predates Claude Code 2.1.169, the first release with --safe-mode", claudeVersionLabel(reportedVersion)))
		} else {
			problems = append(problems, "required isolated structured-mode capabilities are missing")
		}
		nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Update this exact Claude Code executable, or put another qualified installation earlier in PATH", Command: diagnosticCommand + " update"})
	}
	if !authenticated {
		switch auth {
		case "logged_out":
			problems = append(problems, "fresh Claude Code processes report logged out")
			nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Sign in this exact Claude Code executable with your claude.ai subscription", Command: diagnosticCommand + " auth login --claudeai"})
		case "api":
			problems = append(problems, "API, Console, or cloud billing is active instead of a claude.ai subscription")
			nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Switch this exact Claude Code executable to your claude.ai subscription", Command: diagnosticCommand + " auth login --claudeai"})
		default:
			if err != nil {
				problems = append(problems, "Claude authentication status check failed")
			} else {
				problems = append(problems, "claude.ai subscription authentication could not be confirmed")
			}
			nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Check this exact Claude Code executable's subscription authentication", Command: diagnosticCommand + " auth status --text"})
		}
	}
	message := strings.Join(problems, "; ")
	if authenticated && capabilitiesOK {
		message = "claude.ai subscription authentication and qualified safe-mode capabilities confirmed"
		if !versionParsed {
			message = "claude.ai subscription authentication confirmed; capabilities verified by probe because the reported version could not be read"
		}
	}
	if len(nextSteps) > 0 {
		nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Recheck Claude Code in humansh", Command: "humansh setup"})
	}
	capabilities := []string(nil)
	if capabilitiesOK {
		capabilities = append(capabilities, observedCapabilities...)
	}
	return llm.Diagnostic{Installed: true, Configured: true, Authenticated: authenticated, Available: authenticated && capabilitiesOK, AuthMode: auth, Executable: executable, Version: reportedVersion, Capabilities: capabilities, Message: message, NextSteps: nextSteps}
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
	diagnostic := a.Diagnose(ctx)
	if !diagnostic.Available {
		switch {
		case !diagnostic.Installed:
			return llm.TranslationResponse{}, providerutil.Missing("Claude Code", "curl -fsSL https://claude.ai/install.sh | bash", "claude auth login --claudeai")
		case diagnostic.AuthMode == "override":
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderAuth, "claude_auth_override", diagnostic.Message+".", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Unset API/cloud overrides, then retry; for example", Command: "unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN ANTHROPIC_BASE_URL CLAUDE_CODE_USE_BEDROCK CLAUDE_CODE_USE_VERTEX CLAUDE_CODE_USE_FOUNDRY"})
		case len(diagnostic.Capabilities) == 0:
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderUnavailable, "claude_capabilities", "Claude Code is not qualified for safe structured translation.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Update the selected Claude Code installation, then check", Command: "claude update && humansh doctor --provider claude"})
		case diagnostic.AuthMode == "api":
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderAuth, "claude_metered_auth", "Claude Code is using API, Console, or cloud-provider billing instead of a claude.ai subscription.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Sign in to claude.ai", Command: "claude auth login --claudeai"},
				usererr.Fix{Description: "Check", Command: "claude auth status --text"})
		default:
			return llm.TranslationResponse{}, providerutil.Auth("Claude Code", "claude auth login --claudeai", "claude auth status --text", nil)
		}
	}
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
			return llm.TranslationResponse{}, providerutil.Missing("Claude Code", "", "claude auth login --claudeai")
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
		_, executable := a.diagnosticBinary()
		command := claudeDiagnosticCommand(executable)
		typed.Code = "claude_translation_auth"
		typed.Title = "Claude Code's translation process did not accept the login that its auth check reported."
		typed.Fixes = []usererr.Fix{
			{Description: "Refresh the login for this exact executable with", Command: command + " auth login --claudeai"},
			{Description: "Then retry with", Command: "humansh provider test claude"},
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

func parseAuth(data []byte) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return "unknown"
	}
	var subscription, metered, loggedOut bool
	var walk func(string, any)
	walk = func(key string, item any) {
		switch typed := item.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(strings.ToLower(childKey), child)
			}
		case []any:
			for _, child := range typed {
				walk(key, child)
			}
		case bool:
			if (key == "loggedin" || key == "authenticated") && !typed {
				loggedOut = true
			}
		case string:
			text := strings.ToLower(strings.TrimSpace(typed))
			if text == "" || text == "none" || text == "null" || text == "false" || text == "disabled" {
				return
			}
			if strings.Contains(text, "claude.ai") || text == "oauth" || text == "oauth_token" || strings.Contains(text, "subscription") {
				subscription = true
			}
			if strings.Contains(text, "bedrock") || strings.Contains(text, "vertex") || strings.Contains(text, "foundry") || strings.Contains(text, "console") || text == "api" || text == "api_key" || text == "apikey" {
				metered = true
			}
			if strings.Contains(key, "apikey") && text != "none" {
				metered = true
			}
		}
	}
	walk("", value)
	if metered {
		return "api"
	}
	if loggedOut {
		return "logged_out"
	}
	if subscription {
		return "claude.ai"
	}
	return "unknown"
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

func (a Adapter) diagnosticBinary() (command, executable string) {
	command = a.binary()
	if a.Runner != nil {
		return command, ""
	}
	if filepath.IsAbs(command) {
		return command, command
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		return command, ""
	}
	return resolved, resolved
}

func claudeVersionLabel(reported string) string {
	if fields := strings.Fields(reported); len(fields) > 0 {
		return fields[0]
	}
	return "unknown"
}

func claudeDiagnosticCommand(executable string) string {
	if executable == "" {
		return "claude"
	}
	if !strings.ContainsAny(executable, " \t\n'\"\\$`;&|<>()[]{}*?!#~") {
		return executable
	}
	return "'" + strings.ReplaceAll(executable, "'", `'"'"'`) + "'"
}

func claudeRuntimeEnv(tempDir string) []string {
	extra := make(map[string]string, len(claudeSubscriptionEnvKeys)+len(claudeCredentialLocationEnvKeys)+len(claudeUserIdentityEnvKeys))
	for _, key := range claudeSubscriptionEnvKeys {
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

func containsAll(value string, required []string) bool {
	for _, item := range required {
		if !strings.Contains(value, item) {
			return false
		}
	}
	return true
}
