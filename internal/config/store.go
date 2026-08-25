package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/shell"
)

type Paths struct {
	ConfigDir       string
	ConfigFile      string
	ClassifierFile  string
	DataDir         string
	InstallState    string
	ShellDir        string
	BashShellDir    string
	Binary          string
	CacheDir        string
	Credentials     string
	CodexAuthRecord string
	CodexConfigFile string
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	configBase := os.Getenv("XDG_CONFIG_HOME")
	if configBase == "" {
		configBase = filepath.Join(home, ".config")
	}
	dataBase := os.Getenv("XDG_DATA_HOME")
	if dataBase == "" {
		dataBase = filepath.Join(home, ".local", "share")
	}
	cacheBase := os.Getenv("XDG_CACHE_HOME")
	if cacheBase == "" {
		cacheBase = filepath.Join(home, ".cache")
	}
	configDir := filepath.Join(configBase, "humansh")
	dataDir := filepath.Join(dataBase, "humansh")
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	return Paths{
		ConfigDir:       configDir,
		ConfigFile:      filepath.Join(configDir, "config.toml"),
		ClassifierFile:  filepath.Join(configDir, "classifier.toml"),
		DataDir:         dataDir,
		InstallState:    filepath.Join(dataDir, "install-state.toml"),
		ShellDir:        filepath.Join(dataDir, "shell", "zsh"),
		BashShellDir:    filepath.Join(dataDir, "shell", "bash"),
		Binary:          filepath.Join(home, ".local", "bin", "humansh"),
		CacheDir:        filepath.Join(cacheBase, "humansh"),
		Credentials:     filepath.Join(configDir, "credentials.json"),
		CodexAuthRecord: filepath.Join(codexHome, "auth.json"),
		CodexConfigFile: filepath.Join(codexHome, "config.toml"),
	}, nil
}

// ReadCodexSelectedModel reads only the top-level model setting needed by the
// setup wizard. It does not interpret or apply any other Codex customization.
func ReadCodexSelectedModel(paths Paths) (string, error) {
	data, err := os.ReadFile(paths.CodexConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(data) > 1<<20 {
		return "", fmt.Errorf("Codex config exceeds 1 MiB")
	}
	section := ""
	model := ""
	found := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "" {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "model" {
			continue
		}
		raw = strings.TrimSpace(raw)
		if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("Codex top-level model must be a quoted string")
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("parse Codex top-level model: %w", err)
		}
		if found {
			return "", fmt.Errorf("Codex config contains duplicate top-level model settings")
		}
		if err := validateModel(value); err != nil {
			return "", fmt.Errorf("Codex top-level model: %w", err)
		}
		model = value
		found = true
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return model, nil
}

type Store interface {
	Load() (RuntimeConfig, error)
	SaveAtomic(RuntimeConfig) error
	LoadOverrides() (ClassifierOverrides, error)
	SaveOverridesAtomic(ClassifierOverrides) error
}

type FileStore struct{ Paths Paths }

func (s FileStore) Load() (RuntimeConfig, error) {
	if err := requirePrivateFile(s.Paths.ConfigFile); err != nil {
		return RuntimeConfig{}, err
	}
	return s.LoadDiagnostic()
}

// LoadDiagnostic parses config without accepting its permissions for runtime
// use. Doctor uses it only to preserve valid values while repairing modes.
func (s FileStore) LoadDiagnostic() (RuntimeConfig, error) {
	data, err := os.ReadFile(s.Paths.ConfigFile)
	if err != nil {
		return RuntimeConfig{}, err
	}
	return parseConfig(string(data))
}

func (s FileStore) SaveAtomic(cfg RuntimeConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(s.Paths.ConfigDir); err != nil {
		return err
	}
	if err := preserveManualFile(s.Paths.ConfigFile, func(data []byte) ([]byte, error) {
		existing, err := parseConfig(string(data))
		if err != nil {
			return nil, err
		}
		return []byte(renderConfig(existing)), nil
	}); err != nil {
		return err
	}
	return atomicWrite(s.Paths.ConfigFile, []byte(renderConfig(cfg)), 0o600)
}

func (s FileStore) SaveAndApply(cfg RuntimeConfig, apply func() error) error {
	previous, readErr := os.ReadFile(s.Paths.ConfigFile)
	hadPrevious := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := s.SaveAtomic(cfg); err != nil {
		return err
	}
	if err := apply(); err != nil {
		var rollbackErr error
		if hadPrevious {
			rollbackErr = atomicWrite(s.Paths.ConfigFile, previous, 0o600)
		} else {
			rollbackErr = os.Remove(s.Paths.ConfigFile)
			if errors.Is(rollbackErr, os.ErrNotExist) {
				rollbackErr = nil
			}
		}
		if rollbackErr != nil {
			return fmt.Errorf("apply configuration: %w; rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("apply configuration: %w; configuration was rolled back", err)
	}
	return nil
}

func (s FileStore) LoadOverrides() (ClassifierOverrides, error) {
	if err := requirePrivateFile(s.Paths.ClassifierFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ClassifierOverrides{}, err
	}
	return s.LoadOverridesDiagnostic()
}

func (s FileStore) LoadOverridesDiagnostic() (ClassifierOverrides, error) {
	data, err := os.ReadFile(s.Paths.ClassifierFile)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultOverrides(), nil
	}
	if err != nil {
		return ClassifierOverrides{}, err
	}
	return parseOverrides(string(data))
}

func (s FileStore) SaveOverridesAtomic(overrides ClassifierOverrides) error {
	if err := ValidateOverrides(overrides); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(s.Paths.ConfigDir); err != nil {
		return err
	}
	if err := preserveManualFile(s.Paths.ClassifierFile, func(data []byte) ([]byte, error) {
		existing, err := parseOverrides(string(data))
		if err != nil {
			return nil, err
		}
		return []byte(renderOverrides(existing)), nil
	}); err != nil {
		return err
	}
	return atomicWrite(s.Paths.ClassifierFile, []byte(renderOverrides(overrides)), 0o600)
}

func preserveManualFile(path string, canonicalize func([]byte) ([]byte, error)) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := requireFileMode(path, 0o600); err != nil {
		return err
	}
	canonical, err := canonicalize(data)
	if err != nil {
		return fmt.Errorf("refusing to overwrite malformed managed file %s: %w", path, err)
	}
	if bytes.Equal(data, canonical) {
		return nil
	}
	backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".humansh-backup-")
	if err != nil {
		return fmt.Errorf("preserve manually edited %s: %w", path, err)
	}
	backupPath := backup.Name()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(backupPath)
		}
	}()
	if err := backup.Chmod(0o600); err != nil {
		backup.Close()
		return err
	}
	if _, err := backup.Write(data); err != nil {
		backup.Close()
		return err
	}
	if err := backup.Sync(); err != nil {
		backup.Close()
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("managed directory %s must be a directory, not a symlink or special file", path)
	}
	return os.Chmod(path, 0o700)
}

func requirePrivateFile(path string) error {
	if err := requireFileMode(path, 0o600); err != nil {
		return err
	}
	if directory, err := os.Lstat(filepath.Dir(path)); err != nil {
		return err
	} else if directory.Mode()&os.ModeSymlink != 0 || !directory.IsDir() {
		return fmt.Errorf("managed directory %s must be a directory, not a symlink or special file", filepath.Dir(path))
	} else if directory.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("unsafe permissions on %s (mode %o); run `chmod 700 %s` or `humansh doctor --fix`", filepath.Dir(path), directory.Mode().Perm(), filepath.Dir(path))
	}
	return nil
}

func requireFileMode(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("managed file %s must be a regular file, not a symlink or special file", path)
	}
	if info.Mode().Perm() != mode {
		return fmt.Errorf("unsafe permissions on %s (mode %o); run `chmod %o %s` or `humansh doctor --fix`", path, info.Mode().Perm(), mode, path)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("managed directory %s must be a directory, not a symlink or special file", directory)
	}
	tmp, err := os.CreateTemp(directory, ".humansh-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	return errors.Join(syncErr, closeErr)
}

func renderConfig(c RuntimeConfig) string {
	return fmt.Sprintf(`version = %d
provider = %s
timeout_seconds = %d
ambiguity_policy = %s
working_context = %s

[shell]
name = %s
protocol = %s
smart_enter = %t
clear_line_binding = %s
force_translate_binding = %s
force_literal_binding = %s

[providers.codex]
model = %s
auth_mode = %s
subscription_auth_confirmed = %t

[providers.claude]
binary = %s
model = %s
auth_mode = %s

[providers.cursor]
binary = %s
model = %s
auth_mode = %s

[providers.openrouter]
model = %s
base_url = %s
credential_ref = %s
structured_output_proven = %t
structured_output_model = %s

[fallback]
enabled = %t
order = [%s]
allow_metered_openrouter = %t
`, c.Version, quote(string(c.Provider)), int(c.Timeout/time.Second), quote(c.AmbiguityPolicy), quote(c.WorkingContext),
		quote(string(c.Shell.Name)), quote(c.Shell.Protocol), c.Shell.SmartEnter, quote(c.Shell.ClearLineBinding), quote(c.Shell.ForceTranslateBinding), quote(c.Shell.ForceLiteralBinding),
		quote(c.Codex.Model), quote(c.Codex.AuthMode), c.Codex.SubscriptionAuthConfirmed,
		quote(c.Claude.Binary), quote(c.Claude.Model), quote(c.Claude.AuthMode),
		quote(c.Cursor.Binary), quote(c.Cursor.Model), quote(c.Cursor.AuthMode),
		quote(c.OpenRouter.Model), quote(c.OpenRouter.BaseURL), quote(c.OpenRouter.CredentialRef), c.OpenRouter.StructuredOutputProven, quote(c.OpenRouter.StructuredOutputModel),
		c.Fallback.Enabled, quoteListProvider(c.Fallback.Order), c.Fallback.AllowMeteredOpenRouter)
}

func parseConfig(data string) (RuntimeConfig, error) {
	values, err := parseTOMLSubset(data)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if err := rejectUnknownKeys(values, []string{
		"version", "provider", "timeout_seconds", "ambiguity_policy", "working_context",
		"shell.name", "shell.protocol", "shell.smart_enter", "shell.clear_line_binding", "shell.force_translate_binding", "shell.force_literal_binding",
		"providers.codex.model", "providers.codex.auth_mode", "providers.codex.subscription_auth_confirmed",
		"providers.claude.binary", "providers.claude.model", "providers.claude.auth_mode",
		"providers.cursor.binary", "providers.cursor.model", "providers.cursor.auth_mode",
		"providers.openrouter.model", "providers.openrouter.base_url", "providers.openrouter.credential_ref", "providers.openrouter.structured_output_proven", "providers.openrouter.structured_output_model",
		"fallback.enabled", "fallback.order", "fallback.allow_metered_openrouter",
	}); err != nil {
		return RuntimeConfig{}, err
	}
	c := Default()
	if c.Version, err = getInt(values, "version", c.Version); err != nil {
		return c, err
	}
	if value, ok := values["provider"]; ok {
		c.Provider = llm.ProviderID(value.scalar)
	}
	seconds, err := getInt(values, "timeout_seconds", int(c.Timeout/time.Second))
	if err != nil {
		return c, err
	}
	c.Timeout = time.Duration(seconds) * time.Second
	if value, ok := values["ambiguity_policy"]; ok {
		c.AmbiguityPolicy = value.scalar
	}
	if value, ok := values["working_context"]; ok {
		c.WorkingContext = value.scalar
	}
	if value, ok := values["shell.name"]; ok {
		c.Shell.Name = shell.ID(value.scalar)
	}
	if value, ok := values["shell.protocol"]; ok {
		c.Shell.Protocol = value.scalar
	}
	if c.Shell.SmartEnter, err = getBool(values, "shell.smart_enter", c.Shell.SmartEnter); err != nil {
		return c, err
	}
	if value, ok := values["shell.clear_line_binding"]; ok {
		c.Shell.ClearLineBinding = value.scalar
	}
	if value, ok := values["shell.force_translate_binding"]; ok {
		c.Shell.ForceTranslateBinding = value.scalar
	}
	if value, ok := values["shell.force_literal_binding"]; ok {
		c.Shell.ForceLiteralBinding = value.scalar
	}
	if value, ok := values["providers.codex.model"]; ok {
		c.Codex.Model = value.scalar
	}
	if value, ok := values["providers.codex.auth_mode"]; ok {
		c.Codex.AuthMode = value.scalar
	}
	if c.Codex.SubscriptionAuthConfirmed, err = getBool(values, "providers.codex.subscription_auth_confirmed", false); err != nil {
		return c, err
	}
	if value, ok := values["providers.claude.model"]; ok {
		c.Claude.Model = value.scalar
	}
	if value, ok := values["providers.claude.binary"]; ok {
		c.Claude.Binary = value.scalar
	}
	if value, ok := values["providers.claude.auth_mode"]; ok {
		c.Claude.AuthMode = value.scalar
	}
	if value, ok := values["providers.cursor.model"]; ok {
		c.Cursor.Model = value.scalar
	}
	if value, ok := values["providers.cursor.binary"]; ok {
		c.Cursor.Binary = value.scalar
	}
	if value, ok := values["providers.cursor.auth_mode"]; ok {
		c.Cursor.AuthMode = value.scalar
	}
	if value, ok := values["providers.openrouter.model"]; ok {
		c.OpenRouter.Model = value.scalar
	}
	if value, ok := values["providers.openrouter.base_url"]; ok {
		c.OpenRouter.BaseURL = value.scalar
	}
	if value, ok := values["providers.openrouter.credential_ref"]; ok {
		c.OpenRouter.CredentialRef = value.scalar
	}
	if c.OpenRouter.StructuredOutputProven, err = getBool(values, "providers.openrouter.structured_output_proven", false); err != nil {
		return c, err
	}
	if value, ok := values["providers.openrouter.structured_output_model"]; ok {
		c.OpenRouter.StructuredOutputModel = value.scalar
	}
	if c.Fallback.Enabled, err = getBool(values, "fallback.enabled", false); err != nil {
		return c, err
	}
	if value, ok := values["fallback.order"]; ok {
		c.Fallback.Order = make([]llm.ProviderID, len(value.list))
		for i, item := range value.list {
			c.Fallback.Order[i] = llm.ProviderID(item)
		}
	}
	if c.Fallback.AllowMeteredOpenRouter, err = getBool(values, "fallback.allow_metered_openrouter", false); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func renderOverrides(o ClassifierOverrides) string {
	return fmt.Sprintf("version = %d\nalways_commands = [%s]\nalways_natural_language_prefixes = [%s]\n", o.Version, quoteList(o.AlwaysCommands), quoteList(o.AlwaysNaturalLanguagePrefixes))
}

func parseOverrides(data string) (ClassifierOverrides, error) {
	values, err := parseTOMLSubset(data)
	if err != nil {
		return ClassifierOverrides{}, err
	}
	if err := rejectUnknownKeys(values, []string{"version", "always_commands", "always_natural_language_prefixes"}); err != nil {
		return ClassifierOverrides{}, err
	}
	o := DefaultOverrides()
	if o.Version, err = getInt(values, "version", o.Version); err != nil {
		return o, err
	}
	if value, ok := values["always_commands"]; ok {
		o.AlwaysCommands = value.list
	}
	if value, ok := values["always_natural_language_prefixes"]; ok {
		o.AlwaysNaturalLanguagePrefixes = value.list
	}
	return o, ValidateOverrides(o)
}

func ValidateOverrides(o ClassifierOverrides) error {
	if o.Version != CurrentVersion {
		return fmt.Errorf("unsupported classifier version %d", o.Version)
	}
	seen := map[string]struct{}{}
	for _, command := range o.AlwaysCommands {
		if command == "" || !utf8.ValidString(command) || strings.ContainsAny(command, " \t\r\n'\"`$|&;<>*?[](){}\\!~=#%^") {
			return fmt.Errorf("invalid command override %q", command)
		}
		if _, ok := seen[command]; ok {
			return fmt.Errorf("duplicate command override %q", command)
		}
		seen[command] = struct{}{}
	}
	seen = map[string]struct{}{}
	for _, prefix := range o.AlwaysNaturalLanguagePrefixes {
		normalized := strings.ToLower(strings.Join(strings.Fields(prefix), " "))
		if normalized == "" || !utf8.ValidString(prefix) || containsUnicodeControl(prefix) {
			return fmt.Errorf("invalid English prefix override %q", prefix)
		}
		if _, ok := seen[normalized]; ok {
			return fmt.Errorf("duplicate English prefix override %q", prefix)
		}
		seen[normalized] = struct{}{}
	}
	return nil
}

func OverrideConflicts(o ClassifierOverrides) []string {
	var conflicts []string
	for _, command := range o.AlwaysCommands {
		for _, prefix := range o.AlwaysNaturalLanguagePrefixes {
			fields := strings.Fields(prefix)
			if len(fields) > 0 && strings.EqualFold(command, fields[0]) {
				conflicts = append(conflicts, fmt.Sprintf("conflicting classifier overrides: command %q and English prefix %q; remove one with `print -rn -- %s | humansh classifier remove-command` or `print -rn -- %s | humansh classifier remove-english-prefix`", command, prefix, shellSingleQuote(command), shellSingleQuote(prefix)))
			}
		}
	}
	return conflicts
}

func containsUnicodeControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type tomlValue struct {
	scalar string
	list   []string
}

func parseTOMLSubset(data string) (map[string]tomlValue, error) {
	out := map[string]tomlValue{}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(data))
	var pendingKey, pendingValue string
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		if pendingKey != "" {
			pendingValue += " " + line
			if listComplete(pendingValue) {
				value, err := parseValue(pendingValue)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", lineNo, err)
				}
				if _, exists := out[pendingKey]; exists {
					return nil, fmt.Errorf("line %d: duplicate key %s", lineNo, pendingKey)
				}
				out[pendingKey] = value
				pendingKey, pendingValue = "", ""
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: expected key = value", lineNo)
		}
		key := strings.TrimSpace(parts[0])
		if section != "" {
			key = section + "." + key
		}
		raw := strings.TrimSpace(parts[1])
		if strings.HasPrefix(raw, "[") && !listComplete(raw) {
			pendingKey, pendingValue = key, raw
			continue
		}
		value, err := parseValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("line %d: duplicate key %s", lineNo, key)
		}
		out[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if pendingKey != "" {
		return nil, fmt.Errorf("unterminated array for %s", pendingKey)
	}
	return out, nil
}

func rejectUnknownKeys(values map[string]tomlValue, allowed []string) error {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}
	for key := range values {
		if !known[key] {
			return fmt.Errorf("unknown configuration key %s", key)
		}
	}
	return nil
}

func stripComment(line string) string {
	quoted, escaped := false, false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quoted {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == '#' && !quoted {
			return line[:i]
		}
	}
	return line
}

func parseValue(raw string) (tomlValue, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		if !strings.HasSuffix(raw, "]") {
			return tomlValue{}, fmt.Errorf("unterminated array")
		}
		inside := strings.TrimSpace(raw[1 : len(raw)-1])
		if inside == "" {
			return tomlValue{list: []string{}}, nil
		}
		parts, err := splitListItems(inside)
		if err != nil {
			return tomlValue{}, err
		}
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			value, err := unquote(part)
			if err != nil {
				return tomlValue{}, err
			}
			values = append(values, value)
		}
		return tomlValue{list: values}, nil
	}
	value, err := unquote(raw)
	if err != nil {
		return tomlValue{}, err
	}
	return tomlValue{scalar: value}, nil
}

func listComplete(raw string) bool {
	quoted, escaped := false, false
	for _, r := range raw {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quoted {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == ']' && !quoted {
			return true
		}
	}
	return false
}

func splitListItems(inside string) ([]string, error) {
	var parts []string
	quoted, escaped := false, false
	start := 0
	for index, r := range inside {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quoted {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == ',' && !quoted {
			part := strings.TrimSpace(inside[start:index])
			if part == "" {
				return nil, fmt.Errorf("array contains an empty item")
			}
			parts = append(parts, part)
			start = index + 1
		}
	}
	if quoted || escaped {
		return nil, fmt.Errorf("unterminated quoted array item")
	}
	last := strings.TrimSpace(inside[start:])
	if last != "" {
		parts = append(parts, last)
	} else if len(parts) == 0 {
		return nil, fmt.Errorf("array contains an empty item")
	}
	return parts, nil
}

func unquote(raw string) (string, error) {
	if strings.HasPrefix(raw, "\"") {
		return strconv.Unquote(raw)
	}
	return raw, nil
}

func getInt(values map[string]tomlValue, key string, fallback int) (int, error) {
	value, ok := values[key]
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value.scalar)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}
func getBool(values map[string]tomlValue, key string, fallback bool) (bool, error) {
	value, ok := values[key]
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value.scalar)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}
func quote(value string) string { return strconv.Quote(value) }
func quoteList(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = quote(value)
	}
	return strings.Join(parts, ", ")
}
func quoteListProvider(values []llm.ProviderID) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = quote(string(value))
	}
	return strings.Join(parts, ", ")
}
