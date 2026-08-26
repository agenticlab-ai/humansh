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

type cursorEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Cursor's API-key and endpoint settings can redirect usage away from the
// provider configuration selected by the Cursor distribution. They are not
// inherited implicitly by the isolated Humansh child process.
var cursorOverrideEnvKeys = []string{
	"CURSOR_API_KEY", "CURSOR_AUTH_TOKEN", "CURSOR_API_ENDPOINT", "CURSOR_API_BASE_URL",
	"CURSOR_ENABLE_AUTHLESS", "CURSOR_AGENT_CLI_AUTHLESS_MODE", "CURSOR_AGENT_CLI_LOCAL_MODE",
	"CURSOR_ENABLE_BEDROCK", "CURSOR_ENABLE_LOCAL_BEDROCK", "CURSOR_BEDROCK_BASE_URL",
	"CURSOR_LOCAL_AGENT_API_KEY", "CURSOR_LOCAL_AGENT_API_KEY_HELPER", "CURSOR_LOCAL_AGENT_BASE_URL",
}

var cursorUserIdentityEnvKeys = []string{"USER", "LOGNAME"}

var cursorCredentialLocationEnvKeys = []string{"CURSOR_CONFIG_DIR", "CURSOR_DATA_DIR", "XDG_CONFIG_HOME"}

func (Adapter) ID() llm.ProviderID { return llm.Cursor }

func (a Adapter) Diagnose(context.Context) llm.Diagnostic {
	executable, found := a.discoverBinary()
	if !found {
		diagnostic := llm.Diagnostic{Configured: a.Config.Binary != "", AuthMode: "provider_managed", Message: "Cursor CLI is not installed"}
		if a.Config.Binary != "" {
			diagnostic.Executable = a.Config.Binary
			diagnostic.Message = fmt.Sprintf("selected Cursor CLI executable %q was not found", a.Config.Binary)
			diagnostic.NextSteps = []llm.DiagnosticAction{
				{Description: "Restore automatic Cursor executable selection", Command: "humansh config set providers.cursor.binary auto"},
				{Description: "Choose and recheck Cursor CLI", Command: "humansh setup"},
			}
		} else {
			diagnostic.NextSteps = []llm.DiagnosticAction{{Description: "Install Cursor CLI", Command: "curl https://cursor.com/install -fsS | bash"}}
		}
		return diagnostic
	}
	return llm.Diagnostic{
		Installed: true, Configured: true, AuthMode: "provider_managed", Executable: executable,
		Message:   "Cursor CLI is installed; live inference has not been checked",
		NextSteps: []llm.DiagnosticAction{{Description: "Run a live provider check", Command: "humansh provider test cursor"}},
	}
}

func (a Adapter) Probe(ctx context.Context) llm.Diagnostic {
	base := a.Diagnose(ctx)
	if !base.Installed && a.Runner == nil {
		return base
	}
	tempDir, err := os.MkdirTemp("", "humansh-cursor-probe-*")
	if err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Cursor CLI", err))
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Cursor CLI", err))
	}
	probeCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	result, runErr := a.runner().Run(probeCtx, processrunner.Spec{
		Path: a.binary(), Args: []string{"-p", providerutil.ProbePrompt}, Dir: tempDir, Env: cursorRuntimeEnv(tempDir),
		MaxStdout: 64 << 10, MaxStderr: 64 << 10,
	})
	return providerutil.ProbeDiagnostic(base, llm.Cursor, a.timeout(), result, runErr)
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
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
			return llm.TranslationResponse{}, providerutil.Missing("Cursor CLI", "curl https://cursor.com/install -fsS | bash", "")
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
