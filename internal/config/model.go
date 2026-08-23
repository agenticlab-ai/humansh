package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/humansh/humansh/internal/llm"
	"github.com/humansh/humansh/internal/shell"
	"github.com/humansh/humansh/internal/shell/protocol"
)

const CurrentVersion = 1

type ShellConfig struct {
	Name                  shell.ID
	Protocol              string
	SmartEnter            bool
	ClearLineBinding      string
	ForceTranslateBinding string
	ForceLiteralBinding   string
}

type CodexConfig struct {
	Model                     string
	AuthMode                  string
	SubscriptionAuthConfirmed bool
}

type ClaudeConfig struct {
	Binary   string
	Model    string
	AuthMode string
}

type CursorConfig struct {
	Binary   string
	Model    string
	AuthMode string
}

type OpenRouterConfig struct {
	Model                  string
	BaseURL                string
	CredentialRef          string
	StructuredOutputProven bool
	StructuredOutputModel  string
}

type FallbackConfig struct {
	Enabled                bool
	Order                  []llm.ProviderID
	AllowMeteredOpenRouter bool
}

type RuntimeConfig struct {
	Version         int
	Provider        llm.ProviderID
	Timeout         time.Duration
	AmbiguityPolicy string
	WorkingContext  string
	Shell           ShellConfig
	Codex           CodexConfig
	Claude          ClaudeConfig
	Cursor          CursorConfig
	OpenRouter      OpenRouterConfig
	Fallback        FallbackConfig
}

func Default() RuntimeConfig {
	return RuntimeConfig{
		Version:         CurrentVersion,
		Provider:        llm.Codex,
		Timeout:         20 * time.Second,
		AmbiguityPolicy: "ask",
		WorkingContext:  "basename",
		Shell: ShellConfig{
			Name:                  shell.Zsh,
			Protocol:              protocol.Version,
			SmartEnter:            true,
			ClearLineBinding:      "^[",
			ForceTranslateBinding: "^G",
			ForceLiteralBinding:   "^X^M",
		},
		Codex:      CodexConfig{AuthMode: "subscription"},
		Claude:     ClaudeConfig{AuthMode: "subscription"},
		Cursor:     CursorConfig{AuthMode: "account"},
		OpenRouter: OpenRouterConfig{BaseURL: "https://openrouter.ai/api/v1", CredentialRef: "openrouter-default"},
		Fallback:   FallbackConfig{Order: []llm.ProviderID{llm.Codex, llm.Claude, llm.Cursor, llm.OpenRouter}},
	}
}

func (c RuntimeConfig) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	switch c.Provider {
	case llm.Codex, llm.Claude, llm.Cursor, llm.OpenRouter:
	default:
		return fmt.Errorf("unsupported provider %q", c.Provider)
	}
	if c.Timeout < 3*time.Second || c.Timeout > 60*time.Second {
		return fmt.Errorf("timeout must be between 3 and 60 seconds")
	}
	if c.AmbiguityPolicy != "ask" {
		return fmt.Errorf("ambiguity_policy must be ask")
	}
	switch c.WorkingContext {
	case "none", "basename", "full":
	default:
		return fmt.Errorf("working_context must be none, basename, or full")
	}
	switch c.Shell.Name {
	case shell.Zsh:
		if c.Shell.Protocol != protocol.Version {
			return fmt.Errorf("zsh requires protocol %s", protocol.Version)
		}
	case shell.Bash:
		if c.Shell.Protocol != protocol.ReadlineVersion {
			return fmt.Errorf("bash requires protocol %s", protocol.ReadlineVersion)
		}
		if c.Shell.SmartEnter {
			return fmt.Errorf("bash uses safe explicit translation mode and requires shell.smart_enter=false")
		}
	default:
		return fmt.Errorf("unsupported shell %q", c.Shell.Name)
	}
	if err := validateBinding(c.Shell.ForceTranslateBinding); err != nil {
		return fmt.Errorf("force translate binding: %w", err)
	}
	if err := validateBinding(c.Shell.ForceLiteralBinding); err != nil {
		return fmt.Errorf("force literal binding: %w", err)
	}
	if err := validateBinding(c.Shell.ClearLineBinding); err != nil {
		return fmt.Errorf("clear line binding: %w", err)
	}
	if c.Shell.ForceTranslateBinding == c.Shell.ForceLiteralBinding {
		return fmt.Errorf("force translate and force literal bindings must differ")
	}
	if c.Shell.ClearLineBinding == c.Shell.ForceTranslateBinding {
		return fmt.Errorf("clear line and force translate bindings must differ")
	}
	if c.Shell.ClearLineBinding == c.Shell.ForceLiteralBinding {
		return fmt.Errorf("clear line and force literal bindings must differ")
	}
	translateKeys := bindingKeys(c.Shell.ForceTranslateBinding)
	literalKeys := bindingKeys(c.Shell.ForceLiteralBinding)
	if bindingPrefix(translateKeys, literalKeys) || bindingPrefix(literalKeys, translateKeys) {
		return fmt.Errorf("force translate and force literal bindings cannot be prefixes of each other")
	}
	clearKeys := bindingKeys(c.Shell.ClearLineBinding)
	if bindingPrefix(clearKeys, translateKeys) || bindingPrefix(translateKeys, clearKeys) {
		return fmt.Errorf("clear line and force translate bindings cannot be prefixes of each other")
	}
	if bindingPrefix(clearKeys, literalKeys) || bindingPrefix(literalKeys, clearKeys) {
		return fmt.Errorf("clear line and force literal bindings cannot be prefixes of each other")
	}
	if c.Shell.Name == shell.Bash {
		for name, binding := range map[string]string{"clear line": c.Shell.ClearLineBinding, "force translate": c.Shell.ForceTranslateBinding, "force literal": c.Shell.ForceLiteralBinding} {
			if binding == "^M" || binding == "^J" {
				return fmt.Errorf("bash %s binding cannot replace ordinary Enter", name)
			}
		}
	}
	if c.Fallback.Enabled || c.Fallback.AllowMeteredOpenRouter {
		return fmt.Errorf("automatic fallback is not supported in the MVP")
	}
	if c.Codex.AuthMode != "subscription" {
		return fmt.Errorf("providers.codex.auth_mode must be subscription")
	}
	if c.Claude.AuthMode != "subscription" {
		return fmt.Errorf("providers.claude.auth_mode must be subscription")
	}
	if c.Cursor.AuthMode != "account" {
		return fmt.Errorf("providers.cursor.auth_mode must be account")
	}
	if err := validateExecutablePath(c.Claude.Binary); err != nil {
		return fmt.Errorf("providers.claude.binary: %w", err)
	}
	if err := validateExecutablePath(c.Cursor.Binary); err != nil {
		return fmt.Errorf("providers.cursor.binary: %w", err)
	}
	for name, model := range map[string]string{"providers.codex.model": c.Codex.Model, "providers.claude.model": c.Claude.Model, "providers.cursor.model": c.Cursor.Model, "providers.openrouter.model": c.OpenRouter.Model} {
		if err := validateModel(model); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if c.OpenRouter.Model == "openrouter/auto" {
		return fmt.Errorf("OpenRouter requires a concrete model; openrouter/auto is not supported")
	}
	if c.OpenRouter.Model != "" && (!strings.Contains(c.OpenRouter.Model, "/") || strings.HasPrefix(c.OpenRouter.Model, "/") || strings.HasSuffix(c.OpenRouter.Model, "/")) {
		return fmt.Errorf("providers.openrouter.model must use the concrete provider/model ID shown by OpenRouter")
	}
	if c.OpenRouter.CredentialRef != "openrouter-default" {
		return fmt.Errorf("providers.openrouter.credential_ref must be openrouter-default")
	}
	if c.OpenRouter.StructuredOutputProven && c.OpenRouter.Model == "" {
		return fmt.Errorf("OpenRouter structured-output proof requires a configured model")
	}
	if c.OpenRouter.StructuredOutputProven && c.OpenRouter.StructuredOutputModel != c.OpenRouter.Model {
		return fmt.Errorf("OpenRouter structured-output proof belongs to model %q, not %q; rerun `humansh provider configure openrouter --model %s`", c.OpenRouter.StructuredOutputModel, c.OpenRouter.Model, c.OpenRouter.Model)
	}
	if !c.OpenRouter.StructuredOutputProven && c.OpenRouter.StructuredOutputModel != "" {
		return fmt.Errorf("OpenRouter structured-output proof model is present without a successful probe")
	}
	if strings.TrimRight(c.OpenRouter.BaseURL, "/") != "https://openrouter.ai/api/v1" {
		return fmt.Errorf("OpenRouter base_url must be https://openrouter.ai/api/v1 so credentials are never sent to another host")
	}
	seenProviders := make(map[llm.ProviderID]bool, len(c.Fallback.Order))
	if len(c.Fallback.Order) != 3 && len(c.Fallback.Order) != 4 {
		return fmt.Errorf("fallback order must list codex, claude, openrouter, and optionally cursor exactly once")
	}
	for _, provider := range c.Fallback.Order {
		switch provider {
		case llm.Codex, llm.Claude, llm.Cursor, llm.OpenRouter:
		default:
			return fmt.Errorf("fallback order contains unsupported provider %q", provider)
		}
		if seenProviders[provider] {
			return fmt.Errorf("fallback order contains duplicate provider %q", provider)
		}
		seenProviders[provider] = true
	}
	for _, required := range []llm.ProviderID{llm.Codex, llm.Claude, llm.OpenRouter} {
		if !seenProviders[required] {
			return fmt.Errorf("fallback order must include provider %q", required)
		}
	}
	if len(c.Fallback.Order) == 4 && !seenProviders[llm.Cursor] {
		return fmt.Errorf("four-provider fallback order must include cursor")
	}
	return nil
}

func validateExecutablePath(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 4096 || !utf8.ValidString(value) {
		return fmt.Errorf("path must be valid UTF-8 no longer than 4096 bytes")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("path cannot contain control characters")
		}
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("path must be absolute, or empty to select the provider CLI from PATH")
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("path must be clean (without . or .. components)")
	}
	return nil
}

func validateModel(value string) error {
	if len(value) > 256 || !utf8.ValidString(value) {
		return fmt.Errorf("model must be valid UTF-8 no longer than 256 bytes")
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("model cannot begin with - because provider CLIs could parse it as an option")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("model cannot contain whitespace or control characters")
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-._/:+@", r) {
			return fmt.Errorf("model contains unsupported characters")
		}
	}
	return nil
}

func validateBinding(value string) error {
	if value == "" || len(value) > 32 {
		return fmt.Errorf("binding must contain 1 to 32 bytes")
	}
	for _, r := range value {
		asciiAlphaNumeric := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !asciiAlphaNumeric && !strings.ContainsRune("^[]_+.,:/=-", r) {
			return fmt.Errorf("binding contains unsupported characters")
		}
	}
	return nil
}

// BindingLabel turns the bindkey notation stored in config into wording that
// can be followed directly in a terminal prompt. For example, ^G becomes
// "Ctrl-G" and ^X^M becomes "Ctrl-X then Enter".
func BindingLabel(binding string) string {
	keys := bindingKeys(binding)
	for index, key := range keys {
		switch key {
		case "^M":
			keys[index] = "Enter"
		case "^I":
			keys[index] = "Tab"
		case "^[":
			keys[index] = "Esc"
		case "^":
			keys[index] = "^"
		default:
			if strings.HasPrefix(key, "^") {
				keys[index] = "Ctrl-" + strings.TrimPrefix(key, "^")
			}
		}
	}
	return strings.Join(keys, " then ")
}

// ParseBinding accepts the bindkey notation stored in config as well as the
// friendlier key names shown by interactive setup. Examples include ^G,
// "Ctrl-G", "Ctrl-X Ctrl-T", and "Esc t".
func ParseBinding(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("binding cannot be empty")
	}
	if !strings.ContainsAny(value, " \t,") && strings.HasPrefix(value, "^") {
		if err := validateBinding(value); err != nil {
			return "", err
		}
		return value, nil
	}

	for {
		lower := strings.ToLower(value)
		index := strings.Index(lower, " then ")
		if index < 0 {
			break
		}
		value = value[:index] + " " + value[index+len(" then "):]
	}
	value = strings.NewReplacer(",", " ", "→", " ").Replace(value)
	tokens := strings.Fields(value)
	if len(tokens) == 0 {
		return "", fmt.Errorf("binding cannot be empty")
	}

	var notation strings.Builder
	for _, token := range tokens {
		lower := strings.ToLower(token)
		var key string
		switch lower {
		case "enter", "return":
			key = "^M"
		case "tab":
			key = "^I"
		case "esc", "escape":
			key = "^["
		default:
			for _, prefix := range []string{"ctrl-", "ctrl+", "control-", "control+"} {
				if strings.HasPrefix(lower, prefix) {
					remainder := token[len(prefix):]
					if len(remainder) != 1 {
						return "", fmt.Errorf("%q must name one key after Ctrl", token)
					}
					key = "^" + strings.ToUpper(remainder)
					break
				}
			}
			if key == "" {
				if len(token) != 1 {
					return "", fmt.Errorf("cannot understand key %q; try Ctrl-G, Ctrl-X Ctrl-T, Esc t, or ^X^T", token)
				}
				key = token
			}
		}
		notation.WriteString(key)
	}
	parsed := notation.String()
	if err := validateBinding(parsed); err != nil {
		return "", err
	}
	return parsed, nil
}

func bindingKeys(binding string) []string {
	keys := make([]string, 0, len(binding))
	for index := 0; index < len(binding); index++ {
		key := binding[index]
		if key == '^' && index+1 < len(binding) {
			index++
			keys = append(keys, "^"+strings.ToUpper(string(binding[index])))
			continue
		}
		keys = append(keys, string(key))
	}
	return keys
}

func bindingPrefix(first, second []string) bool {
	if len(first) >= len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

type ClassifierOverrides struct {
	Version                       int
	AlwaysCommands                []string
	AlwaysNaturalLanguagePrefixes []string
}

func DefaultOverrides() ClassifierOverrides { return ClassifierOverrides{Version: CurrentVersion} }
