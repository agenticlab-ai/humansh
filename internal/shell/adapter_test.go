package shell

import (
	"context"
	"strings"
	"testing"
)

type registryShell struct{ id ID }

func (s registryShell) ID() ID                                        { return s.id }
func (registryShell) Diagnose(context.Context) Diagnostic             { return Diagnostic{Available: true} }
func (registryShell) Capabilities() Capabilities                      { return Capabilities{} }
func (s registryShell) PromptProfile() PromptProfile                  { return PromptProfile{Shell: string(s.id)} }
func (registryShell) ValidateGenerated(context.Context, string) error { return nil }
func (registryShell) NormalizeGenerated(value string) (string, error) { return value, nil }
func (registryShell) IntegrationAsset() ([]byte, bool)                { return nil, false }
func (registryShell) SupportedProtocols() []string                    { return []string{"test-v1"} }

func TestNewRegistryRejectsDuplicateShellIDs(t *testing.T) {
	_, err := NewRegistry(registryShell{id: Zsh}, registryShell{id: Zsh})
	if err == nil || !strings.Contains(err.Error(), "duplicate adapter ID") {
		t.Fatalf("error=%v", err)
	}
}

func TestNewRegistryAcceptsAdditionalShell(t *testing.T) {
	const second ID = "second"
	registry, err := NewRegistry(registryShell{id: Zsh}, registryShell{id: second})
	if err != nil {
		t.Fatal(err)
	}
	if adapter, ok := registry.Get(second); !ok || adapter.ID() != second {
		t.Fatalf("second shell missing: %v %v", adapter, ok)
	}
}
