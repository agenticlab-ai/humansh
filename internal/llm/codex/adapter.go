package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agenticlab-ai/humansh/assets"
	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/exitcode"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/providerutil"
	"github.com/agenticlab-ai/humansh/internal/processrunner"
	"github.com/agenticlab-ai/humansh/internal/prompt"
)

type Config struct {
	Binary                    string
	Model                     string
	Timeout                   time.Duration
	SubscriptionAuthConfirmed bool
	AuthRecordPath            string
}

type Adapter struct {
	Config           Config
	Runner           processrunner.Runner
	prepareIsolation func(string) error
	verifyIsolation  func(string) error
}

// minimumVersion is the oldest `codex exec` release whose isolation config keys
// humansh has verified. It is a floor, not a pin: Codex ships frequently (0.148
// to 0.149 inside a single day), so pinning exact releases would disable the
// provider within days and tell the user to update a CLI that is already newer.
// Capability probing remains the real gate.
var minimumVersion = [3]int{0, 148, 0}

var requiredHelpOptions = []string{
	"--ephemeral", "--skip-git-repo-check", "--sandbox", "--ignore-user-config", "--ignore-rules",
	"--strict-config", "--color", "--output-schema", "--output-last-message", "--cd",
}

var observedCapabilities = []string{
	"qualified-isolation-config", "strict-structured-output", "final-message-file", "read-only-sandbox", "tools-disabled",
}

func (Adapter) ID() llm.ProviderID { return llm.Codex }

func (a Adapter) Diagnose(ctx context.Context) llm.Diagnostic {
	diagnosticCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	tempDir, err := os.MkdirTemp("", "humansh-codex-diagnose-*")
	if err != nil {
		return llm.Diagnostic{Message: "Codex diagnostic isolation could not be created"}
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return llm.Diagnostic{Message: "Codex diagnostic isolation could not be secured"}
	}
	binary := a.binary()
	runner := a.runner()
	env := processrunner.MinimalEnv(tempDir, nil)
	version, versionErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"exec", "--version"}, Dir: tempDir, Env: env, MaxStdout: 4096, MaxStderr: 4096})
	if processrunner.IsNotFound(versionErr) {
		return llm.Diagnostic{Message: "Codex CLI not installed"}
	}
	reportedVersion := strings.TrimSpace(string(version.Stdout))
	help, helpErr := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"exec", "--help"}, Dir: tempDir, Env: env, MaxStdout: 64 << 10, MaxStderr: 64 << 10})
	probesOK := versionErr == nil && helpErr == nil && containsAll(string(help.Stdout)+"\n"+string(help.Stderr), requiredHelpOptions)
	meetsFloor, versionParsed := providerutil.VersionFloor(reportedVersion, minimumVersion)
	// An unreadable version string must not disable a CLI whose capabilities all
	// probe correctly; doctor reports the unknown version instead.
	capabilitiesOK := probesOK && (meetsFloor || !versionParsed)
	status, err := runner.Run(diagnosticCtx, processrunner.Spec{Path: binary, Args: []string{"login", "status"}, Dir: tempDir, Env: env, MaxStdout: 64 << 10, MaxStderr: 64 << 10})
	auth := parseStatus(string(status.Stdout) + "\n" + string(status.Stderr))
	record := a.inspectAuthRecord()
	authenticated := err == nil && auth == "chatgpt" && record == "chatgpt"
	effectiveAuth := auth
	if auth == "api_key" || record == "api_key" {
		effectiveAuth = "api_key"
	}
	message := ""
	switch {
	case !capabilitiesOK && probesOK && versionParsed && !meetsFloor:
		message = "Codex is older than the minimum verified release 0.148.0; update Codex"
	case !capabilitiesOK:
		message = "required non-interactive isolation capabilities are missing from this Codex build; update Codex"
	case authenticated && !versionParsed:
		message = "ChatGPT subscription authentication confirmed; capabilities verified by probe because the reported version could not be read"
	case authenticated:
		message = "ChatGPT subscription authentication confirmed"
	case effectiveAuth == "api_key":
		message = "usage-based API-key authentication is active; ChatGPT subscription authentication is required"
	case auth == "logged_out":
		message = "login required; choose Sign in with ChatGPT"
	case err != nil:
		message = "Codex login status check failed"
	case auth == "unknown" && record == "chatgpt":
		message = "status wording unrecognized; local auth record corroborates ChatGPT, but explicit subscription confirmation is required"
	case auth == "chatgpt" && record != "chatgpt":
		message = "status reports ChatGPT, but the local auth record could not corroborate it"
	default:
		message = "subscription authentication could not be confirmed"
	}
	if !authenticated && auth == "unknown" && record == "chatgpt" && a.Config.SubscriptionAuthConfirmed {
		authenticated = true
		if capabilitiesOK {
			message = "status wording unrecognized; using explicit subscription confirmation"
		}
	}
	capabilities := []string(nil)
	if capabilitiesOK {
		capabilities = append(capabilities, observedCapabilities...)
	}
	return llm.Diagnostic{Installed: true, Configured: true, Authenticated: authenticated, Available: authenticated && capabilitiesOK, AuthMode: effectiveAuth, Version: reportedVersion, Capabilities: capabilities, Message: message}
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
	diagnostic := a.Diagnose(ctx)
	if !diagnostic.Available {
		switch {
		case !diagnostic.Installed:
			return llm.TranslationResponse{}, providerutil.Missing("Codex CLI", "curl -fsSL https://chatgpt.com/codex/install.sh | sh", "codex login")
		case diagnostic.Authenticated && len(diagnostic.Capabilities) == 0:
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderUnavailable, "codex_capabilities", "Codex is not qualified for safe structured translation.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Update Codex, then check", Command: "humansh doctor"})
		case diagnostic.AuthMode == "api_key":
			return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderAuth, "codex_metered_auth", "Codex is signed in with usage-based API-key authentication, not a ChatGPT subscription.", "Nothing was changed or executed.", false, nil,
				usererr.Fix{Description: "Sign out, then choose Sign in with ChatGPT", Command: "codex logout && codex login"},
				usererr.Fix{Description: "Or explicitly use metered OpenRouter", Command: "humansh provider use openrouter"})
		default:
			return llm.TranslationResponse{}, providerutil.Auth("Codex", "codex login", "codex login status", nil)
		}
	}
	tempDir, err := os.MkdirTemp("", "humansh-codex-*")
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Codex", err)
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Codex", err)
	}
	schemaPath, outputPath := filepath.Join(tempDir, "schema.json"), filepath.Join(tempDir, "last-message.json")
	if err := os.WriteFile(schemaPath, assets.TranslationSchema, 0o600); err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Codex", err)
	}
	if err := os.WriteFile(outputPath, nil, 0o600); err != nil {
		return llm.TranslationResponse{}, providerutil.Temporary("Codex", err)
	}
	if a.prepareIsolation != nil {
		if err := a.prepareIsolation(tempDir); err != nil {
			return llm.TranslationResponse{}, providerutil.Temporary("Codex", err)
		}
	}
	input, err := prompt.Build(request)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("encode request", err)
	}
	args := []string{"exec", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "--ignore-user-config", "--ignore-rules", "--strict-config", "--color", "never",
		"-c", `approval_policy="never"`, "-c", `forced_login_method="chatgpt"`, "-c", `web_search="disabled"`, "-c", "project_doc_max_bytes=0",
		"-c", "agents.enabled=false", "-c", "features.multi_agent=false", "-c", "features.apps=false", "-c", "features.shell_tool=false", "-c", "features.unified_exec=false",
		"-c", "features.shell_snapshot=false", "-c", "features.hooks=false", "-c", "features.skill_mcp_dependency_install=false", "-c", "features.goals=false",
		"-c", "features.memories=false", "-c", `history.persistence="none"`, "-c", "analytics.enabled=false", "-c", "allow_login_shell=false",
		"--output-schema", schemaPath, "--output-last-message", outputPath, "--cd", tempDir}
	if a.Config.Model != "" {
		args = append(args, "--model", a.Config.Model)
	}
	args = append(args, "-")
	callCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	result, runErr := a.runner().Run(callCtx, processrunner.Spec{Path: a.binary(), Args: args, Stdin: input, Dir: tempDir, Env: processrunner.MinimalEnv(tempDir, nil), MaxStdout: 1 << 20, MaxStderr: 1 << 20})
	if a.verifyIsolation != nil {
		if err := a.verifyIsolation(tempDir); err != nil {
			return llm.TranslationResponse{}, providerutil.Temporary("Codex", err)
		}
	}
	if runErr != nil {
		if processrunner.IsNotFound(runErr) {
			return llm.TranslationResponse{}, providerutil.Missing("Codex CLI", "", "codex login")
		}
		if processrunner.IsOutputLimit(runErr) {
			return llm.TranslationResponse{}, providerutil.Malformed("Codex output exceeded the capture limit", runErr)
		}
		return llm.TranslationResponse{}, providerutil.MapCLIError(llm.Codex, a.timeout(), result.Stderr, runErr)
	}
	outputFile, err := os.Open(outputPath)
	if err != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("Codex final-message file could not be opened", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(outputFile, (1<<20)+1))
	closeErr := outputFile.Close()
	if readErr != nil || closeErr != nil {
		return llm.TranslationResponse{}, providerutil.Malformed("Codex final-message file could not be read", errors.Join(readErr, closeErr))
	}
	if len(data) > 1<<20 {
		return llm.TranslationResponse{}, providerutil.Malformed("Codex final-message file exceeded 1 MiB", nil)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return llm.TranslationResponse{}, providerutil.Malformed("Codex final-message file is empty", err)
	}
	response, err := providerutil.DecodeResponse(data)
	if err != nil {
		return llm.TranslationResponse{}, err
	}
	if response.Status == "ok" && strings.TrimSpace(response.Command) == "" {
		return llm.TranslationResponse{}, usererr.WithExit(exitcode.ProviderMalformed, "codex_incomplete", "Codex ended before producing a command.", "Nothing was changed or executed.", true, nil,
			usererr.Fix{Description: "Retry, or diagnose with", Command: "humansh provider test codex"})
	}
	return response, nil
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
	return "codex"
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

func parseStatus(output string) string {
	chatGPT, apiKey, loggedOut := false, false, false
	for _, line := range strings.Split(output, "\n") {
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "logged in using chatgpt", "signed in with chatgpt":
			chatGPT = true
		case "logged in using api key", "signed in with api key":
			apiKey = true
		case "not logged in", "logged out":
			loggedOut = true
		}
	}
	switch {
	case apiKey:
		return "api_key"
	case loggedOut:
		return "logged_out"
	case chatGPT:
		return "chatgpt"
	default:
		return "unknown"
	}
}

func (a Adapter) inspectAuthRecord() string {
	path := a.Config.AuthRecordPath
	if path == "" {
		return "unknown"
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 {
		return "unknown"
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return "unknown"
	}
	var chatGPT, apiKey bool
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
		case string:
			text := strings.ToLower(strings.TrimSpace(typed))
			if text == "" || text == "none" || text == "null" {
				return
			}
			switch key {
			case "api_key", "apikey", "openai_api_key", "codex_api_key":
				apiKey = true
			}
			if text == "api_key" || text == "apikey" {
				apiKey = true
			}
			if text == "chatgpt" || key == "access_token" || key == "id_token" || key == "refresh_token" {
				chatGPT = true
			}
		}
	}
	walk("", value)
	if apiKey {
		return "api_key"
	}
	if chatGPT {
		return "chatgpt"
	}
	return "unknown"
}
