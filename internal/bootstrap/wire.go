package bootstrap

import (
	"context"
	"os"

	"github.com/agenticlab-ai/humansh/internal/app"
	"github.com/agenticlab-ai/humansh/internal/classifier"
	"github.com/agenticlab-ai/humansh/internal/config"
	"github.com/agenticlab-ai/humansh/internal/contextinfo"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/llm/claude"
	"github.com/agenticlab-ai/humansh/internal/llm/codex"
	"github.com/agenticlab-ai/humansh/internal/llm/cursor"
	"github.com/agenticlab-ai/humansh/internal/llm/openrouter"
	"github.com/agenticlab-ai/humansh/internal/risk"
	"github.com/agenticlab-ai/humansh/internal/shell"
	"github.com/agenticlab-ai/humansh/internal/shell/bash"
	"github.com/agenticlab-ai/humansh/internal/shell/zsh"
	"github.com/agenticlab-ai/humansh/internal/validate"
)

type Runtime struct {
	Engine        app.Engine
	ProviderSetup ProviderSetup
	Config        config.RuntimeConfig
	Overrides     config.ClassifierOverrides
	Store         config.FileStore
	Paths         config.Paths
	LoadIssues    []string
}

// ProviderSetup owns provider-specific configuration probes at the composition
// root so CLI handlers never construct concrete adapters.
type ProviderSetup interface {
	ValidateOpenRouterKey(context.Context, config.RuntimeConfig, string, string) error
	ValidateOpenRouterModel(context.Context, config.RuntimeConfig, string, string) error
	ProbeOpenRouter(context.Context, config.RuntimeConfig, string, string) (llm.TranslationResponse, error)
}

type providerSetup struct{}

func (providerSetup) openRouter(cfg config.RuntimeConfig, model, key string) openrouter.Adapter {
	return openrouter.Adapter{Config: openrouter.Config{
		Model: model, BaseURL: cfg.OpenRouter.BaseURL, APIKey: key, Timeout: cfg.Timeout,
		AllowUnprovenSchemaProbe: true,
	}}
}

func (s providerSetup) ValidateOpenRouterKey(ctx context.Context, cfg config.RuntimeConfig, model, key string) error {
	return s.openRouter(cfg, model, key).ValidateKey(ctx)
}

func (s providerSetup) ValidateOpenRouterModel(ctx context.Context, cfg config.RuntimeConfig, model, key string) error {
	return s.openRouter(cfg, model, key).ValidateStructuredOutputModel(ctx)
}

func (s providerSetup) ProbeOpenRouter(ctx context.Context, cfg config.RuntimeConfig, model, key string) (llm.TranslationResponse, error) {
	response, err := s.openRouter(cfg, model, key).ProbeStructuredOutput(ctx)
	if err != nil {
		return llm.TranslationResponse{}, err
	}
	if err := validate.Response(response); err != nil {
		return llm.TranslationResponse{}, err
	}
	return response, nil
}

func Build() (Runtime, error) {
	return build(false)
}

func BuildDiagnostic() (Runtime, error) {
	return build(true)
}

// ReconfigureProviders rebuilds provider adapters from an in-progress setup
// configuration without persisting it. This lets setup verify an executable
// choice before the final apply confirmation.
func ReconfigureProviders(runtime Runtime, cfg config.RuntimeConfig) (Runtime, error) {
	providers, err := configuredProviders(cfg, runtime.Paths)
	if err != nil {
		return Runtime{}, err
	}
	runtime.Config = cfg
	runtime.Engine.Providers = providers
	return runtime, nil
}

func build(diagnostic bool) (Runtime, error) {
	paths, err := config.ResolvePaths()
	if err != nil {
		return Runtime{}, err
	}
	store := config.FileStore{Paths: paths}
	var issues []string
	cfg, err := store.Load()
	if err != nil {
		if !diagnostic {
			if os.IsNotExist(err) {
				cfg = config.Default()
			} else {
				return Runtime{}, err
			}
		} else {
			diagnosticConfig, parseErr := store.LoadDiagnostic()
			switch {
			case parseErr == nil:
				cfg = diagnosticConfig
				issues = append(issues, "configuration file permissions are unsafe: "+err.Error())
			case os.IsNotExist(parseErr):
				cfg = config.Default()
				issues = append(issues, "configuration file missing: run `humansh setup`")
			default:
				cfg = config.Default()
				issues = append(issues, "configuration file is malformed: "+parseErr.Error())
			}
		}
	}
	overrides, err := store.LoadOverrides()
	if err != nil {
		if !diagnostic {
			return Runtime{}, err
		}
		diagnosticOverrides, parseErr := store.LoadOverridesDiagnostic()
		if parseErr == nil {
			overrides = diagnosticOverrides
			issues = append(issues, "classifier override file permissions are unsafe: "+err.Error())
		} else {
			issues = append(issues, "classifier override file is malformed: "+parseErr.Error())
			overrides = config.DefaultOverrides()
		}
	}
	providers, err := configuredProviders(cfg, paths)
	if err != nil {
		return Runtime{}, err
	}
	shells, err := shell.NewRegistry(zsh.Adapter{}, bash.Adapter{})
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{Engine: app.Engine{Classifier: classifier.Classifier{}, Providers: providers, Shells: shells, Validator: validate.Validator{}, Risk: risk.Analyzer{}, Context: contextinfo.Local{}}, ProviderSetup: providerSetup{}, Config: cfg, Overrides: overrides, Store: store, Paths: paths, LoadIssues: issues}, nil
}

func configuredProviders(cfg config.RuntimeConfig, paths config.Paths) (llm.MapRegistry, error) {
	return llm.NewRegistry(
		codex.Adapter{Config: codex.Config{Model: cfg.Codex.Model, Timeout: cfg.Timeout}},
		claude.Adapter{Config: claude.Config{Binary: cfg.Claude.Binary, Model: cfg.Claude.Model, Timeout: cfg.Timeout}},
		cursor.Adapter{Config: cursor.Config{Binary: cfg.Cursor.Binary, Model: cfg.Cursor.Model, Timeout: cfg.Timeout}},
		openrouter.Adapter{Config: openrouter.Config{Model: cfg.OpenRouter.Model, BaseURL: cfg.OpenRouter.BaseURL, Timeout: cfg.Timeout, StructuredOutputProven: cfg.OpenRouter.StructuredOutputProven}, KeyLoader: func() (string, error) {
			return config.LoadOpenRouterKey(paths)
		}},
	)
}
