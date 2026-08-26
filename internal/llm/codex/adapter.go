package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	Binary  string
	Model   string
	Timeout time.Duration
}

type Adapter struct {
	Config           Config
	Runner           processrunner.Runner
	prepareIsolation func(string) error
	verifyIsolation  func(string) error
}

func (Adapter) ID() llm.ProviderID { return llm.Codex }

func (a Adapter) Diagnose(context.Context) llm.Diagnostic {
	executable, found := a.discoverBinary()
	if !found {
		return llm.Diagnostic{
			Configured: a.Config.Binary != "", AuthMode: "provider_managed", Message: "Codex CLI is not installed",
			NextSteps: []llm.DiagnosticAction{{Description: "Install Codex", Command: "curl -fsSL https://chatgpt.com/codex/install.sh | sh"}},
		}
	}
	return llm.Diagnostic{
		Installed: true, Configured: true, AuthMode: "provider_managed", Executable: executable,
		Message:   "Codex CLI is installed; live inference has not been checked",
		NextSteps: []llm.DiagnosticAction{{Description: "Run a live provider check", Command: "humansh provider test codex"}},
	}
}

func (a Adapter) Probe(ctx context.Context) llm.Diagnostic {
	base := a.Diagnose(ctx)
	if !base.Installed && a.Runner == nil {
		return base
	}
	tempDir, err := os.MkdirTemp("", "humansh-codex-probe-*")
	if err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Codex", err))
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Codex", err))
	}
	if err := prepareProbeRepository(tempDir); err != nil {
		return providerutil.DiagnosticFromError(base, providerutil.Temporary("Codex", err))
	}
	probeCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	// Codex normally requires a Git worktree. Supplying an empty, private
	// worktree lets the live check exercise the bare `codex exec <prompt>`
	// surface without depending on an optional repository-check flag.
	result, runErr := a.runner().Run(probeCtx, processrunner.Spec{
		Path: a.binary(), Args: []string{"exec", providerutil.ProbePrompt}, Dir: tempDir,
		Env: processrunner.MinimalEnv(tempDir, nil), MaxStdout: 64 << 10, MaxStderr: 64 << 10,
	})
	return providerutil.ProbeDiagnostic(base, llm.Codex, a.timeout(), result, runErr)
}

func prepareProbeRepository(dir string) error {
	gitDir := filepath.Join(dir, ".git")
	for _, relative := range []string{"objects", filepath.Join("refs", "heads"), filepath.Join("refs", "tags")} {
		if err := os.MkdirAll(filepath.Join(gitDir, relative), 0o700); err != nil {
			return fmt.Errorf("prepare private probe worktree: %w", err)
		}
	}
	files := map[string]string{
		"HEAD":   "ref: refs/heads/main\n",
		"config": "[core]\n\trepositoryformatversion = 0\n\tbare = false\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(gitDir, name), []byte(contents), 0o600); err != nil {
			return fmt.Errorf("prepare private probe worktree: %w", err)
		}
	}
	return nil
}

func (a Adapter) Translate(ctx context.Context, request llm.TranslationRequest) (llm.TranslationResponse, error) {
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
		"-c", `approval_policy="never"`, "-c", `web_search="disabled"`, "-c", "project_doc_max_bytes=0",
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
			return llm.TranslationResponse{}, providerutil.Missing("Codex CLI", "curl -fsSL https://chatgpt.com/codex/install.sh | sh", "")
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

func (a Adapter) discoverBinary() (string, bool) {
	if a.Runner != nil {
		return "", true
	}
	resolved, err := exec.LookPath(a.binary())
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
func (a Adapter) timeout() time.Duration {
	if a.Config.Timeout > 0 {
		return a.Config.Timeout
	}
	return 20 * time.Second
}
