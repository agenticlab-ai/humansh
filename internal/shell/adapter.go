package shell

import (
	"context"
	"fmt"
)

type ID string

const (
	Zsh  ID = "zsh"
	Bash ID = "bash"
)

type FirstTokenKind string

const (
	TokenAlias      FirstTokenKind = "alias"
	TokenFunction   FirstTokenKind = "function"
	TokenBuiltin    FirstTokenKind = "builtin"
	TokenReserved   FirstTokenKind = "reserved"
	TokenCommand    FirstTokenKind = "command"
	TokenUnresolved FirstTokenKind = "unresolved"
	TokenEmpty      FirstTokenKind = "empty"
	TokenUnknown    FirstTokenKind = "unknown"
)

func (k FirstTokenKind) Valid() bool {
	switch k {
	case TokenAlias, TokenFunction, TokenBuiltin, TokenReserved, TokenCommand, TokenUnresolved, TokenEmpty, TokenUnknown:
		return true
	default:
		return false
	}
}

type Capabilities struct {
	InspectEditableBuffer bool
	ReplaceEditableBuffer bool
	ConditionalAccept     bool
	ResolveAliases        bool
	ResolveFunctions      bool
	ExplicitPrefixMode    bool
}

type Diagnostic struct {
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Message   string `json:"message,omitempty"`
}

type PromptProfile struct {
	Shell string `json:"shell"`
}

type Adapter interface {
	ID() ID
	Diagnose(ctx context.Context) Diagnostic
	Capabilities() Capabilities
	PromptProfile() PromptProfile
	ValidateGenerated(ctx context.Context, command string) error
	NormalizeGenerated(command string) (string, error)
	IntegrationAsset() ([]byte, bool)
	SupportedProtocols() []string
}

type Registry interface {
	Get(ID) (Adapter, bool)
	List() []Adapter
}

type MapRegistry map[ID]Adapter

func NewRegistry(adapters ...Adapter) (MapRegistry, error) {
	registry := make(MapRegistry, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, fmt.Errorf("register shell: nil adapter")
		}
		id := adapter.ID()
		if id == "" {
			return nil, fmt.Errorf("register shell: empty adapter ID")
		}
		if _, exists := registry[id]; exists {
			return nil, fmt.Errorf("register shell %q: duplicate adapter ID", id)
		}
		registry[id] = adapter
	}
	return registry, nil
}

func (r MapRegistry) Get(id ID) (Adapter, bool) { a, ok := r[id]; return a, ok }
func (r MapRegistry) List() []Adapter {
	out := make([]Adapter, 0, len(r))
	for _, adapter := range r {
		out = append(out, adapter)
	}
	return out
}
