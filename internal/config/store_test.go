package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/humansh/humansh/assets"
	"github.com/humansh/humansh/internal/shell"
	"github.com/humansh/humansh/internal/shell/protocol"
)

func TestConfigRoundTrip(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.toml")
	paths.ClassifierFile = filepath.Join(paths.ConfigDir, "classifier.toml")
	store := FileStore{Paths: paths}
	cfg := Default()
	cfg.Shell.ClearLineBinding = "^U"
	cfg.Shell.ForceTranslateBinding = "^X^T"
	cfg.Claude.Binary = "/opt/homebrew/bin/claude"
	cfg.Cursor.Binary = "/opt/homebrew/bin/cursor-agent"
	cfg.Cursor.Model = "cursor-model"
	cfg.OpenRouter.Model = "provider/model"
	cfg.OpenRouter.StructuredOutputProven = true
	cfg.OpenRouter.StructuredOutputModel = cfg.OpenRouter.Model
	if err := store.SaveAtomic(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Shell.ClearLineBinding != "^U" || loaded.Shell.ForceTranslateBinding != "^X^T" || loaded.Claude.Binary != cfg.Claude.Binary || loaded.Cursor.Binary != cfg.Cursor.Binary || loaded.Cursor.Model != cfg.Cursor.Model || loaded.Timeout != cfg.Timeout || !loaded.OpenRouter.StructuredOutputProven || loaded.OpenRouter.StructuredOutputModel != cfg.OpenRouter.Model {
		t.Fatalf("loaded=%+v", loaded)
	}
	info, _ := os.Stat(paths.ConfigFile)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestVersionOneConfigWithoutCursorSectionRemainsValid(t *testing.T) {
	legacy := renderConfig(Default())
	legacy = strings.Replace(legacy, `[providers.cursor]
binary = ""
model = ""
auth_mode = "account"

`, "", 1)
	legacy = strings.Replace(legacy, `order = ["codex", "claude", "cursor", "openrouter"]`, `order = ["codex", "claude", "openrouter"]`, 1)
	cfg, err := parseConfig(legacy)
	if err != nil {
		t.Fatalf("existing version-one config was rejected: %v\n%s", err, legacy)
	}
	if cfg.Cursor.AuthMode != "account" || cfg.Cursor.Binary != "" || cfg.Cursor.Model != "" {
		t.Fatalf("Cursor defaults were not supplied for an existing config: %+v", cfg.Cursor)
	}
}

func TestClassifierOverrideArraysRoundTripQuotedPunctuation(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.ClassifierFile = filepath.Join(paths.ConfigDir, "classifier.toml")
	store := FileStore{Paths: paths}
	want := DefaultOverrides()
	want.AlwaysNaturalLanguagePrefixes = []string{"explain, then", "find ] things", `show "quoted" things`}
	if err := store.SaveOverridesAtomic(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AlwaysCommands) != 0 || !reflect.DeepEqual(got.AlwaysNaturalLanguagePrefixes, want.AlwaysNaturalLanguagePrefixes) {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

func TestSavesPreserveManuallyFormattedTypedFiles(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.toml")
	paths.ClassifierFile = filepath.Join(paths.ConfigDir, "classifier.toml")
	store := FileStore{Paths: paths}
	manualConfig := "# keep this explanation\n" + renderConfig(Default())
	manualOverrides := "# keep this classifier note\n" + renderOverrides(DefaultOverrides())
	if err := os.WriteFile(paths.ConfigFile, []byte(manualConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ClassifierFile, []byte(manualOverrides), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Timeout += time.Second
	if err := store.SaveAtomic(cfg); err != nil {
		t.Fatal(err)
	}
	overrides := DefaultOverrides()
	overrides.AlwaysCommands = []string{"deploy"}
	if err := store.SaveOverridesAtomic(overrides); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{paths.ConfigFile: manualConfig, paths.ClassifierFile: manualOverrides} {
		backups, err := filepath.Glob(path + ".humansh-backup-*")
		if err != nil || len(backups) != 1 {
			t.Fatalf("backups for %s = %v, err=%v", path, backups, err)
		}
		data, err := os.ReadFile(backups[0])
		if err != nil || string(data) != want {
			t.Fatalf("backup %s data=%q err=%v", backups[0], data, err)
		}
		if info, err := os.Stat(backups[0]); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("backup %s mode=%v err=%v", backups[0], info, err)
		}
	}
}

func TestSaveAndApplyRollsBack(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.toml")
	store := FileStore{Paths: paths}
	original := Default()
	if err := store.SaveAtomic(original); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Provider = "claude"
	if err := store.SaveAndApply(changed, func() error { return fmt.Errorf("simulated apply failure") }); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("error=%v", err)
	}
	loaded, err := store.Load()
	if err != nil || loaded.Provider != original.Provider {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}

	missingPaths := Paths{ConfigDir: t.TempDir()}
	missingPaths.ConfigFile = filepath.Join(missingPaths.ConfigDir, "config.toml")
	missingStore := FileStore{Paths: missingPaths}
	_ = missingStore.SaveAndApply(Default(), func() error { return fmt.Errorf("simulated apply failure") })
	if _, err := os.Stat(missingPaths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("failed first apply left config behind: %v", err)
	}
}

func TestTypedFilesRejectUnknownAndDuplicateKeys(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"unknown":   "version = 1\nfuture_setting = true\n",
		"duplicate": "version = 1\nversion = 1\n",
	} {
		if _, err := parseConfig(data); err == nil {
			t.Errorf("%s config accepted", name)
		}
	}
	if _, err := parseOverrides("version = 1\nunknown = []\n"); err == nil {
		t.Fatal("unknown override key accepted")
	}
}

func TestUnsupportedConfigVersionsFailClosedWithoutMigration(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	if err := os.Chmod(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.toml")
	store := FileStore{Paths: paths}
	for _, version := range []string{"0", "2"} {
		data := strings.Replace(renderConfig(Default()), "version = 1", "version = "+version, 1)
		if err := os.WriteFile(paths.ConfigFile, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "unsupported config version") {
			t.Fatalf("version %s error=%v", version, err)
		}
		after, err := os.ReadFile(paths.ConfigFile)
		if err != nil || string(after) != data {
			t.Fatalf("unsupported version %s was rewritten: err=%v data=%q", version, err, after)
		}
	}
}

func TestLoadedRuntimeSnapshotsDoNotShareMutableSlices(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.toml")
	store := FileStore{Paths: paths}
	if err := store.SaveAtomic(Default()); err != nil {
		t.Fatal(err)
	}
	first, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	first.Fallback.Order[0] = "changed"
	if second.Fallback.Order[0] == "changed" {
		t.Fatal("independent runtime snapshots share a mutable fallback slice")
	}
}

func TestReadCodexSelectedModelUsesOnlyValidatedTopLevelSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	paths := Paths{CodexConfigFile: path}
	data := "model = \"gpt-test\" # selected model\n[profiles.other]\nmodel = \"ignored\"\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if model, err := ReadCodexSelectedModel(paths); err != nil || model != "gpt-test" {
		t.Fatalf("model=%q err=%v", model, err)
	}
	if err := os.WriteFile(path, []byte("model = unquoted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodexSelectedModel(paths); err == nil {
		t.Fatal("unquoted Codex model accepted")
	}
	if err := os.WriteFile(path, []byte("model = \"gpt-test;unsafe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodexSelectedModel(paths); err == nil {
		t.Fatal("unsafe Codex model accepted")
	}
}

func TestLoadsRejectUnsafeConfigPermissions(t *testing.T) {
	configDir := t.TempDir()
	if err := os.Chmod(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := Paths{ConfigDir: configDir, ConfigFile: filepath.Join(configDir, "config.toml"), ClassifierFile: filepath.Join(configDir, "classifier.toml")}
	store := FileStore{Paths: paths}
	if err := os.WriteFile(paths.ConfigFile, []byte(renderConfig(Default())), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("unsafe config error=%v", err)
	}
	if err := os.WriteFile(paths.ClassifierFile, []byte(renderOverrides(DefaultOverrides())), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadOverrides(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("unsafe overrides error=%v", err)
	}
}

func TestPermissionRepairRefusesManagedSymlinks(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "unrelated")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.ConfigFile); err != nil {
		t.Fatal(err)
	}
	if err := RepairPermissions(paths); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("permission repair followed managed symlink: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("unrelated target mode changed: info=%v err=%v", info, err)
	}
}

func TestRuntimeLoadRejectsSymlinkedPrivateDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(target, "config.toml")
	if err := os.WriteFile(configFile, []byte(renderConfig(Default())), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(target, linked); err != nil {
		t.Fatal(err)
	}
	store := FileStore{Paths: Paths{ConfigDir: linked, ConfigFile: filepath.Join(linked, "config.toml")}}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked private directory accepted: %v", err)
	}
}

func TestInstallStateValidation(t *testing.T) {
	setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInstallState(paths.InstallState); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(paths.InstallState)
	bad := strings.Replace(string(data), "version = 2\n", "version = 99\n", 1)
	if err := os.WriteFile(paths.InstallState, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadInstallState(paths.InstallState); err == nil {
		t.Fatal("unsupported install-state version accepted")
	}
}

func TestSetupMigratesVersionOneStateToMultipleShells(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	state, err := Setup(paths, Default(), "test")
	if err != nil {
		t.Fatal(err)
	}
	legacy := state
	legacy.Version = 1
	legacy.Integrations = nil
	if err := os.WriteFile(paths.InstallState, []byte(renderInstallState(legacy)), 0o600); err != nil {
		t.Fatal(err)
	}
	loadedLegacy, err := LoadInstallState(paths.InstallState)
	if err != nil || !reflect.DeepEqual(loadedLegacy.ShellIDs(), []shell.ID{shell.Zsh}) {
		t.Fatalf("legacy state=%+v err=%v", loadedLegacy, err)
	}
	migrated, err := SetupWithOptions(paths, Default(), "test", SetupOptions{Shells: []shell.ID{shell.Zsh, shell.Bash}})
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Version != 2 || !reflect.DeepEqual(migrated.ShellIDs(), []shell.ID{shell.Zsh, shell.Bash}) {
		t.Fatalf("migrated state=%+v", migrated)
	}
	for _, startup := range []string{filepath.Join(home, ".zshrc"), filepath.Join(home, ".bashrc")} {
		data, readErr := os.ReadFile(startup)
		if readErr != nil || !strings.Contains(string(data), managedStart) {
			t.Errorf("startup %s=%q err=%v", startup, data, readErr)
		}
	}
}

func TestDoctorRepairAndUninstallCoverEveryInstalledShell(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	ids := []shell.ID{shell.Zsh, shell.Bash}
	if _, err := SetupWithOptions(paths, cfg, "test", SetupOptions{Shells: ids}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if issues := Doctor(paths, cfg, "test"); len(issues) != 0 {
		t.Fatalf("healthy multi-shell doctor issues=%v", issues)
	}
	bashAsset := filepath.Join(paths.BashShellDir, "humansh.bash")
	if err := os.WriteFile(bashAsset, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if issues := Doctor(paths, cfg, "test"); len(issues) == 0 || !strings.Contains(strings.Join(issues, " "), "Bash shell asset hash mismatch") {
		t.Fatalf("Bash corruption was not diagnosed: %v", issues)
	}
	if _, err := SetupWithOptions(paths, cfg, "test", SetupOptions{Shells: ids, Repair: true}); err != nil {
		t.Fatal(err)
	}
	if issues := Doctor(paths, cfg, "test"); len(issues) != 0 {
		t.Fatalf("multi-shell repair issues=%v", issues)
	}
	if _, err := Uninstall(paths, UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(paths.ShellDir, "humansh.zsh"), filepath.Join(paths.BashShellDir, "humansh.bash"), paths.InstallState, paths.Binary} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("multi-shell uninstall retained %s: %v", path, err)
		}
	}
	for _, startup := range []string{filepath.Join(home, ".zshrc"), filepath.Join(home, ".bashrc")} {
		data, err := os.ReadFile(startup)
		if err != nil || strings.Contains(string(data), managedStart) {
			t.Errorf("startup %s after uninstall=%q err=%v", startup, data, err)
		}
	}
}

func TestSetupManagedBlockAndImmutableAsset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte("source /opt/zsh-syntax-highlighting.zsh\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Shell.ClearLineBinding = "^U"
	cfg.Shell.ForceTranslateBinding = "^X^T"
	cfg.Shell.SmartEnter = false
	state, err := Setup(paths, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "HUMANSH_SMART_ENTER='0'") || !strings.Contains(text, "HUMANSH_CLEAR_LINE_BINDING='^U'") || !strings.Contains(text, "HUMANSH_FORCE_TRANSLATE_BINDING='^X^T'") {
		t.Fatalf("block:\n%s", text)
	}
	// The provider label must be exported here so the integration never has to
	// spawn humansh at shell startup to render "Translating with <provider>…".
	if !strings.Contains(text, "HUMANSH_PROVIDER_LABEL='Codex'") {
		t.Fatalf("managed block must export the provider label:\n%s", text)
	}
	if strings.Index(text, managedStart) > strings.Index(text, "zsh-syntax-highlighting") {
		t.Fatal("managed block must precede zsh-syntax-highlighting")
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(assets.ZshIntegration))
	if state.ShellAssetSHA256 != wantHash {
		t.Fatalf("hash=%s", state.ShellAssetSHA256)
	}
	cfg.Shell.ForceTranslateBinding = "^X^G"
	cfg.Shell.SmartEnter = true
	state2, err := Setup(paths, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if state2.ShellAssetSHA256 != state.ShellAssetSHA256 {
		t.Fatal("preference rewrote hashed asset")
	}
	updatedStartup, err := os.ReadFile(zshrc)
	if err != nil || !strings.Contains(string(updatedStartup), "HUMANSH_SMART_ENTER='1'") || !strings.Contains(string(updatedStartup), "HUMANSH_FORCE_TRANSLATE_BINDING='^X^G'") {
		t.Fatalf("re-rendered block did not enable smart Enter with the updated binding: err=%v\n%s", err, updatedStartup)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	issues := Doctor(paths, cfg, "test")
	if len(issues) != 0 {
		t.Fatalf("doctor issues=%v", issues)
	}
}

func TestSetupMigratesZshIntegrationToBashTransactionally(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("keep-zsh\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("keep-bash\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	bashConfig := Default()
	bashConfig.Shell.Name = shell.Bash
	bashConfig.Shell.Protocol = protocol.ReadlineVersion
	bashConfig.Shell.SmartEnter = false
	priorPreview, err := PreviewPreviousStartupChange(paths, bashConfig, false)
	if err != nil || priorPreview == nil || !priorPreview.Changed() {
		t.Fatalf("prior startup preview=%+v err=%v", priorPreview, err)
	}
	priorPatch := ""
	for _, line := range priorPreview.PatchLines() {
		priorPatch += string(line.Kind) + line.Text + "\n"
	}
	if !strings.Contains(priorPatch, "-source ") || !strings.Contains(priorPatch, "humansh.zsh") {
		t.Fatalf("prior removal patch=%q", priorPatch)
	}
	state, err := SetupWithOptions(paths, bashConfig, "test", SetupOptions{ReviewedPreviousStartup: priorPreview})
	if err != nil {
		t.Fatal(err)
	}
	if state.Shell != "bash" || state.Protocol != protocol.ReadlineVersion || state.ShellAssetPath != filepath.Join(paths.BashShellDir, "humansh.bash") {
		t.Fatalf("Bash install state=%+v", state)
	}
	zshrc, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil || strings.TrimSpace(string(zshrc)) != "keep-zsh" {
		t.Fatalf("old Zsh integration was not cleanly removed: %q err=%v", zshrc, err)
	}
	bashrc, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil || !strings.Contains(string(bashrc), "keep-bash") || !strings.Contains(string(bashrc), "/shell/bash/humansh.bash") {
		t.Fatalf("Bash integration was not installed: %q err=%v", bashrc, err)
	}
	if _, err := os.Stat(filepath.Join(paths.ShellDir, "humansh.zsh")); !os.IsNotExist(err) {
		t.Fatalf("old Zsh asset remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.BashShellDir, "humansh.bash")); err != nil {
		t.Fatalf("Bash asset missing: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if issues := Doctor(paths, bashConfig, "test"); len(issues) != 0 {
		t.Fatalf("Bash doctor issues=%v", issues)
	}
	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{NoShellChange: true}); err == nil || !strings.Contains(err.Error(), "cannot switch") {
		t.Fatalf("unsafe no-shell-change migration accepted: %v", err)
	}
	if _, err := Uninstall(paths, UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(filepath.Join(home, ".bashrc")); err != nil || strings.TrimSpace(string(after)) != "keep-bash" {
		t.Fatalf("Bash uninstall startup=%q err=%v", after, err)
	}
}

func TestStartupPreviewMatchesReviewedApply(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(startup, []byte("export PRIVATE_VALUE=do-not-print\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewStartupChange(paths, Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Changed() || preview.Path != startup || preview.TargetPath != startup {
		t.Fatalf("preview=%+v", preview)
	}
	patchText := ""
	for _, line := range preview.PatchLines() {
		patchText += string(line.Kind) + line.Text + "\n"
	}
	if !strings.Contains(patchText, "+"+managedStart) || !strings.Contains(patchText, "+source ") || strings.Contains(patchText, "PRIVATE_VALUE") {
		t.Fatalf("managed patch=%q", patchText)
	}

	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{ReviewedStartup: &preview}); err != nil {
		t.Fatal(err)
	}
	applied, err := os.ReadFile(startup)
	if err != nil || string(applied) != string(preview.after) {
		t.Fatalf("reviewed bytes were not applied exactly: err=%v\nwant=%q\ngot=%q", err, preview.after, applied)
	}
	second, err := PreviewStartupChange(paths, Default(), false)
	if err != nil || second.Changed() {
		t.Fatalf("applied startup should need no second patch: changed=%t err=%v", second.Changed(), err)
	}
}

func TestSetupRejectsStartupEditedAfterReview(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewStartupChange(paths, Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	const userEdit = "# changed in another process after review\n"
	if err := os.WriteFile(startup, []byte(userEdit), 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{ReviewedStartup: &preview}); err == nil || !strings.Contains(err.Error(), "changed after the setup review") {
		t.Fatalf("stale review accepted: %v", err)
	}
	data, err := os.ReadFile(startup)
	if err != nil || string(data) != userEdit {
		t.Fatalf("concurrent user edit was changed: err=%v data=%q", err, data)
	}
	for _, path := range []string{filepath.Join(paths.ShellDir, "humansh.zsh"), paths.InstallState} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale review left managed file %s: %v", path, err)
		}
	}
}

func TestSetupHandlesExistingEmptyStartup(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(startup, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(startup)
	if err != nil || strings.Count(string(data), managedStart) != 1 || strings.Count(string(data), managedEnd) != 1 {
		t.Fatalf("empty startup was not initialized exactly once: err=%v data=%q", err, data)
	}
	if info, err := os.Stat(startup); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("startup mode=%v err=%v", info, err)
	}
	if backups, err := filepath.Glob(startup + ".humansh-backup-*"); err != nil || len(backups) != 0 {
		t.Fatalf("empty startup should not need a backup: backups=%v err=%v", backups, err)
	}
}

func TestManagedBlockIncludesPathOnlyWhenMissing(t *testing.T) {
	paths := Paths{Binary: "/tmp/test-home/.local/bin/humansh", ShellDir: "/tmp/test-home/data/shell/zsh"}
	missing := managedBlockFor(paths, Default(), true)
	present := managedBlockFor(paths, Default(), false)
	if !strings.Contains(missing, "export PATH=") {
		t.Fatalf("missing PATH block omitted export:\n%s", missing)
	}
	if strings.Contains(present, "export PATH=") {
		t.Fatalf("already-configured PATH was duplicated:\n%s", present)
	}
}

func TestSetupPreservesStartupFileSymlink(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	dotfiles := filepath.Join(home, "dotfiles")
	if err := os.Mkdir(dotfiles, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dotfiles, "zshrc")
	if err := os.WriteFile(target, []byte("keep\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	if err := os.Symlink(filepath.Join("dotfiles", "zshrc"), startup); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewStartupChange(paths, Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Path != startup || preview.TargetPath != resolvedTarget || !preview.Changed() {
		t.Fatalf("symlink preview=%+v", preview)
	}
	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{ReviewedStartup: &preview}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(startup); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("startup symlink was replaced: info=%v err=%v", info, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), managedStart) || !strings.Contains(string(data), "keep") {
		t.Fatalf("target was not safely updated: err=%v data=%s", err, data)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o640 {
		t.Fatalf("target mode=%o", info.Mode().Perm())
	}
}

func TestSetupIsByteIdempotentAndPreservesLeadingWhitespace(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(startup, []byte("\n\n# user heading\nexport USER_SETTING=1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(startup)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(first), "\n\n# user heading") {
		t.Fatalf("leading whitespace changed:\n%s", first)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(startup)
	if err != nil || string(first) != string(second) {
		t.Fatalf("setup not byte-idempotent: err=%v\nfirst=%q\nsecond=%q", err, first, second)
	}
}

func TestManagedBlockShellQuotesDerivedPaths(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home'with`touch PWNED`")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("zsh", "-f", "-c", `source "$HOME/.zshrc"`)
	cmd.Dir = root
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("source managed block: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "PWNED")); !os.IsNotExist(err) {
		t.Fatalf("derived path executed command substitution: %v", err)
	}
}

func TestUnsafeBindingRejected(t *testing.T) {
	for _, binding := range []string{"'; touch /tmp/pwn; '", "^G extra", "^G;echo", "é"} {
		for _, set := range []func(*RuntimeConfig){
			func(cfg *RuntimeConfig) { cfg.Shell.ClearLineBinding = binding },
			func(cfg *RuntimeConfig) { cfg.Shell.ForceTranslateBinding = binding },
			func(cfg *RuntimeConfig) { cfg.Shell.ForceLiteralBinding = binding },
		} {
			cfg := Default()
			set(&cfg)
			if cfg.Validate() == nil {
				t.Errorf("unsafe binding %q accepted", binding)
			}
		}
	}
	cfg := Default()
	cfg.Shell.ForceLiteralBinding = cfg.Shell.ForceTranslateBinding
	if cfg.Validate() == nil {
		t.Fatal("identical force bindings accepted")
	}
}

func TestTypedProviderConfigurationRejectsUnsafeOrStaleValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RuntimeConfig)
	}{
		{"codex-auth-mode", func(c *RuntimeConfig) { c.Codex.AuthMode = "api" }},
		{"claude-auth-mode", func(c *RuntimeConfig) { c.Claude.AuthMode = "api" }},
		{"claude-relative-binary", func(c *RuntimeConfig) { c.Claude.Binary = "bin/claude" }},
		{"claude-unclean-binary", func(c *RuntimeConfig) { c.Claude.Binary = "/opt/../bin/claude" }},
		{"cursor-auth-mode", func(c *RuntimeConfig) { c.Cursor.AuthMode = "api" }},
		{"cursor-relative-binary", func(c *RuntimeConfig) { c.Cursor.Binary = "bin/cursor-agent" }},
		{"model-control", func(c *RuntimeConfig) { c.Codex.Model = "model\nother" }},
		{"model-option", func(c *RuntimeConfig) { c.Cursor.Model = "--force" }},
		{"openrouter-model-without-provider", func(c *RuntimeConfig) { c.OpenRouter.Model = "model-only" }},
		{"credential-ref", func(c *RuntimeConfig) { c.OpenRouter.CredentialRef = "other" }},
		{"incomplete-fallback-order", func(c *RuntimeConfig) { c.Fallback.Order = c.Fallback.Order[:2] }},
		{"stale-openrouter-proof", func(c *RuntimeConfig) {
			c.OpenRouter.Model = "provider/new"
			c.OpenRouter.StructuredOutputProven = true
			c.OpenRouter.StructuredOutputModel = "provider/old"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Default()
			test.mutate(&cfg)
			if cfg.Validate() == nil {
				t.Fatalf("invalid config accepted: %+v", cfg)
			}
		})
	}
}

func TestOverrideConflictsIdentifyExactEntries(t *testing.T) {
	overrides := ClassifierOverrides{Version: 1, AlwaysCommands: []string{"deploy"}, AlwaysNaturalLanguagePrefixes: []string{"Deploy to production"}}
	conflicts := OverrideConflicts(overrides)
	if len(conflicts) != 1 || !strings.Contains(conflicts[0], `command "deploy"`) || !strings.Contains(conflicts[0], `English prefix "Deploy to production"`) {
		t.Fatalf("conflicts=%v", conflicts)
	}
}

func TestOverrideValidationRejectsControlsAndQuotesFixesForShell(t *testing.T) {
	for _, overrides := range []ClassifierOverrides{
		{Version: 1, AlwaysCommands: []string{`deploy\\prod`}},
		{Version: 1, AlwaysCommands: []string{"deploy!"}},
		{Version: 1, AlwaysNaturalLanguagePrefixes: []string{"show\tme"}},
		{Version: 1, AlwaysNaturalLanguagePrefixes: []string{string([]byte{0xff})}},
	} {
		if err := ValidateOverrides(overrides); err == nil {
			t.Errorf("unsafe overrides accepted: %#v", overrides)
		}
	}
	overrides := ClassifierOverrides{Version: 1, AlwaysCommands: []string{"deploy"}, AlwaysNaturalLanguagePrefixes: []string{"deploy $(touch /tmp/never)"}}
	conflicts := OverrideConflicts(overrides)
	if len(conflicts) != 1 || !strings.Contains(conflicts[0], `print -rn -- 'deploy $(touch /tmp/never)'`) || strings.Contains(conflicts[0], `print -rn -- "deploy $(touch /tmp/never)"`) {
		t.Fatalf("conflict fix is not safely single-quoted: %v", conflicts)
	}
}

func TestSetupNoShellChangePrintPath(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{NoShellChange: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatalf("no-shell-change created .zshrc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.ShellDir, "humansh.zsh")); err != nil {
		t.Fatalf("shell asset not installed: %v", err)
	}
	if !strings.Contains(ManagedBlock(paths, Default()), "source ") {
		t.Fatal("manual managed block omitted source line")
	}
}

func TestSetupRepairCorruptedManagedBlock(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	zshrc := filepath.Join(home, ".zshrc")
	corrupted := "keep-before\n" + managedStart + "\nexport HUMANSH_SMART_ENTER='0'\nkeep-after\n"
	if err := os.WriteFile(zshrc, []byte(corrupted), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err == nil {
		t.Fatal("normal setup silently accepted a partial managed block")
	}
	preview, err := PreviewStartupChange(paths, Default(), true)
	if err != nil {
		t.Fatal(err)
	}
	patchText := ""
	for _, line := range preview.PatchLines() {
		patchText += string(line.Kind) + line.Text + "\n"
	}
	if !strings.Contains(patchText, "-export HUMANSH_SMART_ENTER='0'") || !strings.Contains(patchText, "+source ") {
		t.Fatalf("repair patch omitted managed changes: %q", patchText)
	}
	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{Repair: true, ReviewedStartup: &preview}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, managedStart) != 1 || strings.Count(text, managedEnd) != 1 || !strings.Contains(text, "keep-before") || !strings.Contains(text, "keep-after") {
		t.Fatalf("repair did not preserve unrelated content or create one block:\n%s", text)
	}
	if info, _ := os.Stat(zshrc); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestSetupRepairPreservesAndDoesNotPreviewUnknownMarkerContent(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	content := managedStart + "\nexport HUMANSH_SMART_ENTER='0'\nexport PRIVATE_TOKEN='do-not-preview'\n" + managedEnd + "\n"
	if err := os.WriteFile(startup, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewStartupChange(paths, Default(), false); err == nil || !strings.Contains(err.Error(), "unrecognized line") {
		t.Fatalf("normal preview accepted unknown marker content: %v", err)
	}
	preview, err := PreviewStartupChange(paths, Default(), true)
	if err != nil {
		t.Fatal(err)
	}
	patchText := ""
	for _, line := range preview.PatchLines() {
		patchText += string(line.Kind) + line.Text + "\n"
	}
	if strings.Contains(patchText, "PRIVATE_TOKEN") || strings.Contains(patchText, "do-not-preview") {
		t.Fatalf("repair preview exposed unknown marker content: %q", patchText)
	}
	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{Repair: true, ReviewedStartup: &preview}); err != nil {
		t.Fatal(err)
	}
	applied, err := os.ReadFile(startup)
	if err != nil || !strings.Contains(string(applied), "export PRIVATE_TOKEN='do-not-preview'") {
		t.Fatalf("repair discarded unknown marker content: err=%v data=%q", err, applied)
	}
}

func TestSetupRejectsAndRepairRemovesDuplicateManagedBlocks(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	block := ManagedBlock(paths, Default())
	startup := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(startup, []byte("keep\n"+block+block), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err == nil {
		t.Fatal("normal setup accepted duplicate managed blocks")
	}
	if _, err := SetupWithOptions(paths, Default(), "test", SetupOptions{Repair: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(startup)
	if err != nil || strings.Count(string(data), managedStart) != 1 || strings.Count(string(data), managedEnd) != 1 || !strings.Contains(string(data), "keep") {
		t.Fatalf("duplicate repair failed: err=%v data=%s", err, data)
	}
}

func TestSetupRefusesNonWritableStartup(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(zshrc, []byte("keep\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewStartupChange(paths, Default(), false); err == nil || !IsStartupAccessError(err) {
		t.Fatalf("preview did not classify startup access failure: %v", err)
	}
	if _, err := Setup(paths, Default(), "test"); err == nil || !strings.Contains(err.Error(), "not owner-writable") {
		t.Fatalf("error=%v", err)
	}
	data, _ := os.ReadFile(zshrc)
	if string(data) != "keep\n" {
		t.Fatalf("startup changed: %q", data)
	}
	for _, path := range []string{filepath.Join(paths.ShellDir, "humansh.zsh"), paths.InstallState} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed setup left managed file %s: %v", path, err)
		}
	}
}

func TestDoctorDetectsManagedBlockDrift(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if _, err := Setup(paths, cfg, "test"); err != nil {
		t.Fatal(err)
	}
	zshrc := filepath.Join(home, ".zshrc")
	data, _ := os.ReadFile(zshrc)
	drifted := strings.Replace(string(data), "HUMANSH_SMART_ENTER='1'", "HUMANSH_SMART_ENTER='0'", 1)
	if err := os.WriteFile(zshrc, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	issues := Doctor(paths, cfg, "test")
	if len(issues) == 0 || !strings.Contains(strings.Join(issues, " "), "differs from validated configuration") {
		t.Fatalf("issues=%v", issues)
	}
}

func TestSetupPlacesBlockBeforeSyntaxHighlightingAndBacksUpStartup(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	startup := filepath.Join(home, ".zshrc")
	original := "export KEEP=1\nsource /opt/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh\n"
	if err := os.WriteFile(startup, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, cfg, "test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(startup)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Index(text, managedStart) < 0 || strings.Index(text, managedStart) > strings.Index(text, "zsh-syntax-highlighting") {
		t.Fatalf("managed block was not placed before syntax highlighting:\n%s", text)
	}
	if !strings.Contains(text, "export KEEP=1") {
		t.Fatalf("unrelated startup content was lost:\n%s", text)
	}
	info, err := os.Stat(startup)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("startup mode=%v err=%v", info, err)
	}
	backups, err := filepath.Glob(startup + ".humansh-backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || string(backup) != original {
		t.Fatalf("backup=%q err=%v", backup, err)
	}
}

func TestDoctorDetectsAndRepairRepositionsAfterDirectEnterClobber(t *testing.T) {
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if _, err := Setup(paths, cfg, "test"); err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	file, err := os.OpenFile(startup, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("bindkey -M viins '^M' accept-line\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	issues := Doctor(paths, cfg, "test")
	if !containsIssue(issues, "bindkey command after humansh") || !containsIssue(issues, "emacs, viins, or vicmd") {
		t.Fatalf("doctor issues=%v", issues)
	}
	if _, err := SetupWithOptions(paths, cfg, "test", SetupOptions{Repair: true}); err != nil {
		t.Fatal(err)
	}
	if issues := Doctor(paths, cfg, "test"); containsIssue(issues, "bindkey command after humansh") {
		t.Fatalf("repair did not reposition managed block: %v", issues)
	}
}

func containsIssue(issues []string, fragment string) bool {
	return strings.Contains(strings.Join(issues, "\n"), fragment)
}

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	return home
}
