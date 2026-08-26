package llm

import (
	"context"
	"strings"
	"testing"
)

type registryProvider struct{ id ProviderID }

func (p registryProvider) ID() ProviderID { return p.id }
func (registryProvider) Diagnose(context.Context) Diagnostic {
	return Diagnostic{Available: true}
}
func (registryProvider) Probe(context.Context) Diagnostic {
	return Diagnostic{Installed: true, Configured: true, Authenticated: true, Available: true, LiveCheck: true, AuthMode: "test"}
}
func (registryProvider) Translate(context.Context, TranslationRequest) (TranslationResponse, error) {
	return TranslationResponse{Status: "ok", Command: "true", Explanation: "Succeeds.", Assumptions: []string{}}, nil
}

func TestNewRegistryRejectsDuplicateProviderIDs(t *testing.T) {
	_, err := NewRegistry(registryProvider{id: Codex}, registryProvider{id: Codex})
	if err == nil || !strings.Contains(err.Error(), "duplicate adapter ID") {
		t.Fatalf("error=%v", err)
	}
}

func TestNewRegistryAcceptsAdditionalProvider(t *testing.T) {
	const fourth ProviderID = "fourth"
	registry, err := NewRegistry(registryProvider{id: Codex}, registryProvider{id: fourth})
	if err != nil {
		t.Fatal(err)
	}
	if provider, ok := registry.Get(fourth); !ok || provider.ID() != fourth {
		t.Fatalf("fourth provider missing: %v %v", provider, ok)
	}
}
