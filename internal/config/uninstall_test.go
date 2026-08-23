package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUninstallPreservesConfigurationAndIsIdempotent(t *testing.T) {
	home, paths := prepareUninstall(t)
	result, err := Uninstall(paths, UninstallOptions{})
	if err != nil || result.Purged {
		t.Fatalf("uninstall result=%+v err=%v", result, err)
	}
	for _, removed := range []string{paths.Binary, filepath.Join(paths.ShellDir, "humansh.zsh"), paths.InstallState} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Errorf("managed target remains %s: %v", removed, err)
		}
	}
	for _, preserved := range []string{paths.ConfigFile, paths.ClassifierFile, paths.Credentials} {
		if _, err := os.Stat(preserved); err != nil {
			t.Errorf("default uninstall removed %s: %v", preserved, err)
		}
	}
	startup, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil || strings.Contains(string(startup), "humansh") || !strings.Contains(string(startup), "keep-before\nkeep-after\n") {
		t.Fatalf("startup=%q err=%v", startup, err)
	}
	if _, err := Uninstall(paths, UninstallOptions{}); err != nil {
		t.Fatalf("repeated uninstall: %v", err)
	}
}

func TestUninstallPurgeRemovesOnlyOwnedDirectories(t *testing.T) {
	home, paths := prepareUninstall(t)
	installFakeSecurity(t, home)
	unrelated := filepath.Join(home, "config", "other", "keep")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(paths, UninstallOptions{Purge: true})
	if err != nil || !result.Purged {
		t.Fatalf("purge result=%+v err=%v", result, err)
	}
	for _, removed := range []string{paths.Binary, paths.ConfigDir, paths.DataDir} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Errorf("purged target remains %s: %v", removed, err)
		}
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatalf("purge damaged unrelated file: %v %q", err, data)
	}
}

func TestUninstallPreflightRejectsCorruptionWithoutChanges(t *testing.T) {
	home, paths := prepareUninstall(t)
	startup := filepath.Join(home, ".zshrc")
	corrupt := "keep\n# >>> humansh >>>\nunterminated\n"
	if err := os.WriteFile(startup, []byte(corrupt), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(paths, UninstallOptions{}); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("corrupt markers error=%v", err)
	}
	if data, err := os.ReadFile(startup); err != nil || string(data) != corrupt {
		t.Fatalf("failed preflight changed startup: %v %q", err, data)
	}
	for _, preserved := range []string{paths.Binary, filepath.Join(paths.ShellDir, "humansh.zsh"), paths.InstallState} {
		if _, err := os.Stat(preserved); err != nil {
			t.Errorf("failed preflight removed %s: %v", preserved, err)
		}
	}
}

func TestUninstallRejectsManagedSymlinkWithoutFollowingIt(t *testing.T) {
	_, paths := prepareUninstall(t)
	unrelated := filepath.Join(filepath.Dir(paths.Binary), "unrelated")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.Binary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(unrelated, paths.Binary); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(paths, UninstallOptions{}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("managed symlink error=%v", err)
	}
	if data, err := os.ReadFile(unrelated); err != nil || string(data) != "keep" {
		t.Fatalf("uninstall followed managed symlink: %v %q", err, data)
	}
	if info, err := os.Lstat(paths.Binary); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("failed preflight changed managed symlink: info=%v err=%v", info, err)
	}
}

func TestUninstallRejectsRedirectedInstallStateWithoutChanges(t *testing.T) {
	_, paths := prepareUninstall(t)
	state, err := LoadInstallState(paths.InstallState)
	if err != nil {
		t.Fatal(err)
	}
	state.BinaryPath = filepath.Join(filepath.Dir(paths.Binary), "unrelated")
	if err := atomicWrite(paths.InstallState, []byte(renderInstallState(state)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(paths, UninstallOptions{}); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("redirected state error=%v", err)
	}
	for _, preserved := range []string{paths.Binary, filepath.Join(paths.ShellDir, "humansh.zsh"), paths.InstallState} {
		if _, err := os.Stat(preserved); err != nil {
			t.Errorf("failed state preflight removed %s: %v", preserved, err)
		}
	}
}

func prepareUninstall(t *testing.T) (string, Paths) {
	t.Helper()
	home := setupHome(t)
	paths, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	startup := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(startup, []byte("keep-before\nkeep-after\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := FileStore{Paths: paths}
	if err := store.SaveAtomic(Default()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOverridesAtomic(ClassifierOverrides{Version: CurrentVersion, AlwaysCommands: []string{"deploy"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Credentials, []byte(`{"openrouter_api_key":"test-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(paths, Default(), "test"); err != nil {
		t.Fatal(err)
	}
	return home, paths
}

func installFakeSecurity(t *testing.T, home string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	fakeBin := filepath.Join(home, "fake-bin")
	if err := os.Mkdir(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	security := filepath.Join(fakeBin, "security")
	if err := os.WriteFile(security, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin")
}
