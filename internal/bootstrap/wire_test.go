package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agenticlab-ai/humansh/internal/config"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/claude"
	"github.com/agenticlab-ai/humansh/internal/llm/cursor"
	"github.com/agenticlab-ai/humansh/internal/shell"
)

func TestCompositionRootResolvesAllConfiguredAdaptersWithoutLoadingSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("OPENROUTER_API_KEY", "")
	credentials := filepath.Join(home, "config", "humansh", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(credentials), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte(`{"openrouter_api_key":"secret"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := Build()
	if err != nil {
		t.Fatalf("Build eagerly loaded unsafe credentials: %v", err)
	}
	for _, id := range []llm.ProviderID{llm.Codex, llm.Claude, llm.Cursor, llm.OpenRouter} {
		if _, ok := runtime.Engine.Providers.Get(id); !ok {
			t.Errorf("provider %q not registered", id)
		}
	}
	for _, id := range []shell.ID{shell.Zsh, shell.Bash} {
		if _, ok := runtime.Engine.Shells.Get(id); !ok {
			t.Fatalf("%s adapter not registered", id)
		}
	}
}

func TestReconfigureProvidersAppliesUnpersistedCursorExecutable(t *testing.T) {
	runtime := Runtime{Paths: config.Paths{}}
	cfg := config.Default()
	cfg.Cursor.Binary = "/opt/homebrew/bin/cursor-agent"
	reconfigured, err := ReconfigureProviders(runtime, cfg)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := reconfigured.Engine.Providers.Get(llm.Cursor)
	if !ok {
		t.Fatal("Cursor provider missing")
	}
	adapter, ok := provider.(cursor.Adapter)
	if !ok || adapter.Config.Binary != cfg.Cursor.Binary || reconfigured.Config.Cursor.Binary != cfg.Cursor.Binary {
		t.Fatalf("provider=%T config=%+v runtime=%+v", provider, adapter.Config, reconfigured.Config.Cursor)
	}
}

func TestRegisteringCursorDoesNotReplaceClaude(t *testing.T) {
	providers, err := configuredProviders(config.Default(), config.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	claudeProvider, claudeOK := providers.Get(llm.Claude)
	cursorProvider, cursorOK := providers.Get(llm.Cursor)
	if !claudeOK || !cursorOK || claudeProvider.ID() != llm.Claude || cursorProvider.ID() != llm.Cursor {
		t.Fatalf("Claude/Cursor registration collision: claude=(%T,%t) cursor=(%T,%t)", claudeProvider, claudeOK, cursorProvider, cursorOK)
	}
}

func TestReconfigureProvidersAppliesUnpersistedClaudeExecutable(t *testing.T) {
	runtime := Runtime{Paths: config.Paths{}}
	cfg := config.Default()
	cfg.Claude.Binary = "/opt/homebrew/bin/claude"
	reconfigured, err := ReconfigureProviders(runtime, cfg)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := reconfigured.Engine.Providers.Get(llm.Claude)
	if !ok {
		t.Fatal("Claude provider missing")
	}
	adapter, ok := provider.(claude.Adapter)
	if !ok || adapter.Config.Binary != cfg.Claude.Binary || reconfigured.Config.Claude.Binary != cfg.Claude.Binary {
		t.Fatalf("provider=%T config=%+v runtime=%+v", provider, adapter.Config, reconfigured.Config.Claude)
	}
}

func TestDiagnosticBuildPreservesValidConfigWhileReportingUnsafeModes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Shell.ForceTranslateBinding = "^X^T"
	store := config.FileStore{Paths: paths}
	if err := store.SaveAtomic(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.ConfigFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runtime, err := BuildDiagnostic()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Config.Shell.ForceTranslateBinding != "^X^T" || !strings.Contains(strings.Join(runtime.LoadIssues, " "), "permissions are unsafe") {
		t.Fatalf("config=%+v issues=%v", runtime.Config, runtime.LoadIssues)
	}
	if err := config.RepairPermissions(paths); err != nil {
		t.Fatal(err)
	}
	runtime, err = BuildDiagnostic()
	if err != nil || len(runtime.LoadIssues) != 0 || runtime.Config.Shell.ForceTranslateBinding != "^X^T" {
		t.Fatalf("repaired config=%+v issues=%v err=%v", runtime.Config, runtime.LoadIssues, err)
	}
}

func TestProviderSetupOwnsConcreteOpenRouterProbe(t *testing.T) {
	var keyChecks, modelChecks, probes int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/key":
			keyChecks++
			_, _ = writer.Write([]byte(`{"data":{"limit_remaining":10}}`))
		case "/model/test/model":
			modelChecks++
			_, _ = writer.Write([]byte(`{"data":{"id":"test/model","supported_parameters":["structured_outputs"]}}`))
		case "/chat/completions":
			probes++
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"{\"status\":\"ok\",\"command\":\"pwd\",\"explanation\":\"Prints the current directory.\",\"clarification\":\"\",\"assumptions\":[]}"},"finish_reason":"stop"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.OpenRouter.BaseURL = server.URL
	service := providerSetup{}
	if err := service.ValidateOpenRouterKey(context.Background(), cfg, "test/model", "test-key"); err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateOpenRouterModel(context.Background(), cfg, "test/model", "test-key"); err != nil {
		t.Fatal(err)
	}
	response, err := service.ProbeOpenRouter(context.Background(), cfg, "test/model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if keyChecks != 1 || modelChecks != 1 || probes != 1 || response.Command != "pwd" {
		t.Fatalf("key checks=%d model checks=%d probes=%d response=%+v", keyChecks, modelChecks, probes, response)
	}
}
