package cursor

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

type cursorEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

var requiredHelpOptions = []string{"--print", "--output-format", "--mode", "--sandbox", "--trust"}

var observedCapabilities = []string{
	"ask-mode-read-only", "json-envelope", "sandbox-enabled", "empty-workspace", "local-schema-validation",
}

// Cursor's API-key and endpoint settings can redirect usage away from the
// signed-in Cursor account. Humansh supports only the official browser login
// so selecting Cursor never silently changes billing or destination.
var cursorOverrideEnvKeys = []string{
	"CURSOR_API_KEY", "CURSOR_AUTH_TOKEN", "CURSOR_API_ENDPOINT", "CURSOR_API_BASE_URL",
	"CURSOR_ENABLE_AUTHLESS", "CURSOR_AGENT_CLI_AUTHLESS_MODE", "CURSOR_AGENT_CLI_LOCAL_MODE",
	"CURSOR_ENABLE_BEDROCK", "CURSOR_ENABLE_LOCAL_BEDROCK", "CURSOR_BEDROCK_BASE_URL",
	"CURSOR_LOCAL_AGENT_API_KEY", "CURSOR_LOCAL_AGENT_API_KEY_HELPER", "CURSOR_LOCAL_AGENT_BASE_URL",
}

var cursorUserIdentityEnvKeys = []string{"USER", "LOGNAME"}

var cursorCredentialLocationEnvKeys = []string{"CURSOR_CONFIG_DIR", "CURSOR_DATA_DIR", "XDG_CONFIG_HOME"}

func (Adapter) ID() llm.ProviderID { return llm.Cursor }

func (a Adapter) Diagnose(ctx context.Context) llm.Diagnostic {
	for _, key := range cursorOverrideEnvKeys {
		if os.Getenv(key) != "" {
			return llm.Diagnostic{Installed: true, Configured: true, AuthMode: "override", Message: key + " overrides Cursor browser-login authentication"}
		}
	}

	diagnosticCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	tempDir, err := os.MkdirTemp("", "humansh-cursor-diagnose-*")
	if err != nil {
		return llm.Diagnostic{Message: "Cursor CLI diagnostic isolation could not be created"}
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return llm.Diagnostic{Message: "Cursor CLI diagnostic isolation could not be secured"}
	}

	binary, executable := a.diagnosticBinary()
	command := cursorDiagnosticCommand(executable)
	env := cursorRuntimeEnv(tempDir)
	runner := a.runner()
	version, versionErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"--version"}, Dir: tempDir, Env: env, MaxStdout: 4096, MaxStderr: 4096})
	if processrunner.IsNotFound(versionErr) {
		if a.Config.Binary != "" {
			return llm.Diagnostic{
				Configured: true, Executable: a.Config.Binary,
				Message: fmt.Sprintf("selected Cursor CLI executable %q was not found", a.Config.Binary),
				NextSteps: []llm.DiagnosticAction{
					{Description: "Restore automatic Cursor executable selection", Command: "humansh config set providers.cursor.binary auto"},
					{Description: "Choose and recheck Cursor CLI", Command: "humansh setup"},
				},
			}
		}
		return llm.Diagnostic{Message: "Cursor CLI is not installed", NextSteps: []llm.DiagnosticAction{{Description: "Install Cursor CLI", Command: "curl https://cursor.com/install -fsS | bash"}}}
	}
	reportedVersion := strings.TrimSpace(string(version.Stdout))
	if reportedVersion == "" {
		reportedVersion = strings.TrimSpace(string(version.Stderr))
	}
	help, helpErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"--help"}, Dir: tempDir, Env: env, MaxStdout: 64 << 10, MaxStderr: 64 << 10})
	capabilitiesOK := versionErr == nil && helpErr == nil && containsAll(string(help.Stdout)+"\n"+string(help.Stderr), requiredHelpOptions)
	status, statusErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"status", "--format", "json"}, Dir: tempDir, Env: env, MaxStdout: 64 << 10, MaxStderr: 64 << 10})
	auth := parseAuth(status.Stdout)
	authenticated := statusErr == nil && auth == "cursor.com"

	var problems []string
	var nextSteps []llm.DiagnosticAction
	if !capabilitiesOK {
		problems = append(problems, "required read-only non-interactive capabilities are missing")
		nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Update this exact Cursor CLI executable", Command: command + " update"})
	}
	if !authenticated {
		switch auth {
		case "logged_out":
			problems = append(problems, "fresh Cursor CLI processes report logged out")
			nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Sign in this exact Cursor CLI executable in your browser", Command: command + " login"})
		case "api":
			problems = append(problems, "API-key authentication is active instead of a Cursor browser login")
			nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Remove the API-key authentication and sign in through Cursor", Command: command + " login"})
		default:
			if statusErr != nil {
				problems = append(problems, "Cursor authentication status check failed")
			} else {
				problems = append(problems, "Cursor browser-login authentication could not be confirmed")
			}
			nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Check this exact Cursor CLI executable's login", Command: command + " status"})
		}
	}
	message := strings.Join(problems, "; ")
	if authenticated && capabilitiesOK {
		message = "Cursor browser login and qualified read-only capabilities confirmed"
	}
	if len(nextSteps) > 0 {
		nextSteps = append(nextSteps, llm.DiagnosticAction{Description: "Recheck Cursor CLI in humansh", Command: "humansh setup"})
	}
	capabilities := []string(nil)
	if capabilitiesOK {
		capabilities = append(capabilities, observedCapabilities...)
	}
	return llm.Diagnostic{
		Installed: true, Configured: true, Authenticated: authenticated, Available: authenticated && capabilitiesOK,
		AuthMode: auth, Executable: executable, Version: reportedVersion, Capabilities: capabilities, Message: message, NextSteps: nextSteps,
	}
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
	// Readiness checks and the model request each receive a full timeout budget.
	// Sharing one deadline here made the three diagnostic subprocesses consume
	// part of the time configured for the actual translation.
	diagnostic := a.Diagnose(ctx)
	if !diagnostic.Available {
		switch {
		case !diagnostic.Installed:
			return llm.TranslationResponse{}, providerutil.Missing("Cursor CLI", "curl https://cursor.com/install -fsS | bash", "cursor-agent login")
		case diagnostic.AuthMode == "override":
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderAuth, "cursor_auth_override", diagnostic.Message+".", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Unset the reported Cursor API, endpoint, authless, local, or Bedrock override, then retry", Command: "humansh doctor --provider cursor"})
		case len(diagnostic.Capabilities) == 0:
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderUnavailable, "cursor_capabilities", "Cursor CLI is not qualified for read-only structured translation.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Update Cursor CLI, then check", Command: "cursor-agent update && humansh doctor --provider cursor"})
		case diagnostic.AuthMode == "api":
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderAuth, "cursor_metered_auth", "Cursor CLI is using API-key authentication instead of a Cursor browser login.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Sign in through Cursor", Command: "cursor-agent login"}, usererr.Fix{Description: "Check", Command: "cursor-agent status"})
		default:
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderAuth, "provider_auth", "Cursor CLI is not logged in through the required Cursor browser account.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Fix", Command: "cursor-agent login"}, usererr.Fix{Description: "Check", Command: "cursor-agent status"})
		}
	}

	tempDir, err := os.MkdirTemp("", "humansh-cursor-*")
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Cursor CLI", err)
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Cursor CLI", err)
	}
	wirePrompt, err := cursorPrompt(request)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("encode Cursor request", err)
	}
	args := []string{"--print", "--output-format", "json", "--mode", "ask", "--sandbox", "enabled", "--trust"}
	if a.Config.Model != "" {
		args = append(args, "--model", a.Config.Model)
	}
	callCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	result, runErr := a.runner().Run(callCtx, processrunner.Spec{Path: a.binary(), Args: args, Stdin: wirePrompt, Dir: tempDir, Env: cursorRuntimeEnv(tempDir), MaxStdout: 1 << 20, MaxStderr: 1 << 20})
	if runErr != nil {
		if processrunner.IsNotFound(runErr) {
			return llm.TranslationResponse{}, providerutil.Missing("Cursor CLI", "curl https://cursor.com/install -fsS | bash", "cursor-agent login")
		}
		if processrunner.IsOutputLimit(runErr) {
			return llm.TranslationResponse{}, providerutil.Malformed("Cursor CLI output exceeded the capture limit", runErr)
		}
		return llm.TranslationResponse{}, a.mapCLIError(result, runErr)
	}

	var envelope cursorEnvelope
	if err := providerutil.RejectDuplicateJSONKeys(result.Stdout); err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("Cursor output envelope contained duplicate JSON fields", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(result.Stdout))
	if err := decoder.Decode(&envelope); err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("Cursor output envelope", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return llm.TranslationResponse{}, providerutil.Malformed("Cursor output envelope contained trailing data", err)
	}
	if envelope.Type != "result" || envelope.Subtype != "success" || envelope.IsError {
		return llm.TranslationResponse{}, a.mapCLIError(result, fmt.Errorf("Cursor CLI returned a non-success result"))
	}
	if strings.TrimSpace(envelope.Result) == "" {
		return llm.TranslationResponse{}, providerutil.Malformed("Cursor result is empty", nil)
	}
	return providerutil.DecodeResponse([]byte(envelope.Result))
}

func cursorPrompt(request llm.TranslationRequest) ([]byte, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var value bytes.Buffer
	value.WriteString(prompt.Instruction)
	value.WriteString("\nThe exact response JSON Schema is between RESPONSE_SCHEMA_BEGIN and RESPONSE_SCHEMA_END. Return the response object only, without Markdown fences.\nRESPONSE_SCHEMA_BEGIN\n")
	value.Write(assets.TranslationSchema)
	value.WriteString("\nRESPONSE_SCHEMA_END\nREQUEST_JSON_BEGIN\n")
	value.Write(requestJSON)
	value.WriteString("\nREQUEST_JSON_END\n")
	return value.Bytes(), nil
}

func parseAuth(data []byte) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return "unknown"
	}
	var authenticated, loggedOut, api bool
	var walk func(string, any)
	walk = func(key string, item any) {
		switch typed := item.(type) {
		case map[string]any:
			for childKey, child := range typed {
				walk(strings.ToLower(strings.ReplaceAll(childKey, "_", "")), child)
			}
		case []any:
			for _, child := range typed {
				walk(key, child)
			}
		case bool:
			if key == "authenticated" || key == "loggedin" {
				if typed {
					authenticated = true
				} else {
					loggedOut = true
				}
			}
		case string:
			text := strings.ToLower(strings.TrimSpace(typed))
			if text == "api" || text == "api-key" || text == "api_key" || text == "apikey" || strings.Contains(text, "api key") {
				api = true
			}
			if (key == "status" || key == "authstatus") && (text == "authenticated" || text == "loggedin" || text == "logged-in" || text == "logged_in") {
				authenticated = true
			}
			if (key == "status" || key == "authstatus") && (text == "unauthenticated" || text == "loggedout" || text == "logged-out" || text == "logged_out") {
				loggedOut = true
			}
		}
	}
	walk("", value)
	if api {
		return "api"
	}
	if authenticated {
		return "cursor.com"
	}
	if loggedOut {
		return "logged_out"
	}
	return "unknown"
}

func (a Adapter) mapCLIError(result processrunner.Result, runErr error) error {
	detail := cursorFailureDetail(result)
	cause := runErr
	if detail != "" {
		cause = fmt.Errorf("%w: Cursor CLI reported: %s", runErr, detail)
	}
	mapped := providerutil.MapCLIError(llm.Cursor, a.timeout(), []byte(detail), cause)
	typed, ok := usererr.As(mapped)
	if ok && detail != "" {
		typed.Summary = "Cursor CLI reported: " + detail + "\nNothing was changed or executed."
	}
	return mapped
}

func cursorFailureDetail(result processrunner.Result) string {
	var envelope cursorEnvelope
	if json.Unmarshal(bytes.TrimSpace(result.Stdout), &envelope) == nil && envelope.IsError {
		if detail := cleanFailureDetail(envelope.Result); detail != "" {
			return detail
		}
	}
	return cleanFailureDetail(string(result.Stderr))
}

func cleanFailureDetail(value string) string {
	value = strings.Join(strings.Fields(usererr.RedactDebug(value)), " ")
	runes := []rune(value)
	if len(runes) > 400 {
		return string(runes[:400]) + "…"
	}
	return value
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
		return "cursor-agent"
	}
	if resolved, err := absoluteLookPath("cursor-agent"); err == nil {
		return resolved
	}
	if resolved, err := absoluteLookPath("agent"); err == nil {
		return resolved
	}
	return "cursor-agent"
}

func absoluteLookPath(name string) (string, error) {
	resolved, err := exec.LookPath(name)
	if err != nil || filepath.IsAbs(resolved) {
		return resolved, err
	}
	return filepath.Abs(resolved)
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

func cursorDiagnosticCommand(executable string) string {
	if executable == "" {
		return "cursor-agent"
	}
	if !strings.ContainsAny(executable, " \t\n'\"\\$`;&|<>()[]{}*?!#~") {
		return executable
	}
	return "'" + strings.ReplaceAll(executable, "'", `'"'"'`) + "'"
}

func cursorRuntimeEnv(tempDir string) []string {
	extra := make(map[string]string, len(cursorUserIdentityEnvKeys)+len(cursorCredentialLocationEnvKeys)+2)
	for _, key := range cursorUserIdentityEnvKeys {
		if value := os.Getenv(key); value != "" && !strings.ContainsAny(value, "\x00\r\n=") {
			extra[key] = value
		}
	}
	for _, key := range cursorCredentialLocationEnvKeys {
		if value := os.Getenv(key); filepath.IsAbs(value) {
			extra[key] = value
		}
	}
	if value := os.Getenv("AGENT_CLI_CREDENTIAL_STORE"); value == "file" {
		extra["AGENT_CLI_CREDENTIAL_STORE"] = value
	}
	if value := os.Getenv("OSTYPE"); value != "" && !strings.ContainsAny(value, "\x00\r\n=") {
		extra["OSTYPE"] = value
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
