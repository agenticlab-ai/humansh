package config

import (
	"strings"
	"testing"

	"github.com/agenticlab-ai/humansh/internal/shell"
	"github.com/agenticlab-ai/humansh/internal/shell/protocol"
)

func TestBindingLabel(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"^G":     "Ctrl-G",
		"^X^M":   "Ctrl-X then Enter",
		"^X^T":   "Ctrl-X then Ctrl-T",
		"^[a":    "Esc then a",
		"^I":     "Tab",
		"abc":    "a then b then c",
		"caret^": "c then a then r then e then t then ^",
	}
	for binding, want := range tests {
		binding, want := binding, want
		t.Run(binding, func(t *testing.T) {
			t.Parallel()
			if got := BindingLabel(binding); got != want {
				t.Fatalf("BindingLabel(%q) = %q, want %q", binding, got, want)
			}
		})
	}
}

func TestParseBindingAcceptsFriendlySetupInput(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Ctrl-G":                  "^G",
		"Ctrl-X Ctrl-T":           "^X^T",
		"Ctrl-X, then Enter":      "^X^M",
		"Control-X Return":        "^X^M",
		"Esc t":                   "^[t",
		"^X^M":                    "^X^M",
		"Ctrl-X → Ctrl-T":         "^X^T",
		"Escape then Control-Tab": "",
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseBinding(input)
			if want == "" {
				if err == nil {
					t.Fatalf("ParseBinding(%q) = %q, want an error", input, got)
				}
				return
			}
			if err != nil || got != want {
				t.Fatalf("ParseBinding(%q) = %q, %v; want %q", input, got, err, want)
			}
		})
	}
}

func TestConfigRejectsShortcutPrefixConflicts(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Shell.ForceTranslateBinding = "^X"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "prefixes") {
		t.Fatalf("prefix conflict accepted: %v", err)
	}
}

func TestDefaultClearLineBindingIsEscape(t *testing.T) {
	t.Parallel()
	cfg := Default()
	if cfg.Shell.ClearLineBinding != "^[" || BindingLabel(cfg.Shell.ClearLineBinding) != "Esc" {
		t.Fatalf("default clear binding = %q (%s), want Escape", cfg.Shell.ClearLineBinding, BindingLabel(cfg.Shell.ClearLineBinding))
	}
}

func TestConfigRejectsClearLineShortcutConflicts(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Shell.ForceTranslateBinding = "^[t"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "clear line and force translate") {
		t.Fatalf("clear-line prefix conflict accepted: %v", err)
	}
}

func TestBashConfigRequiresExplicitReadlineMode(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Shell.Name = shell.Bash
	cfg.Shell.Protocol = protocol.ReadlineVersion
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "smart_enter=false") {
		t.Fatalf("Bash Smart Enter accepted: %v", err)
	}
	cfg.Shell.SmartEnter = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid Bash config rejected: %v", err)
	}
	cfg.Shell.ClearLineBinding = "^M"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "cannot replace ordinary Enter") {
		t.Fatalf("Bash Enter replacement accepted: %v", err)
	}
}
