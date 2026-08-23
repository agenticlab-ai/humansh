package config

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Tests of the file fallback must never consult a developer's real Keychain.
// A pre-existing humansh credential would otherwise take precedence over the
// temporary file and make the tests both nondeterministic and secret-bearing.
func disableKeychainLookup(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	fakeBin := t.TempDir()
	security := filepath.Join(fakeBin, "security")
	if err := os.WriteFile(security, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCredentialFileFallback(t *testing.T) {
	disableKeychainLookup(t)
	paths := Paths{ConfigDir: t.TempDir()}
	paths.Credentials = filepath.Join(paths.ConfigDir, "credentials.json")
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	if err := ConfigureOpenRouterKey(paths, bytes.NewBufferString("sk-or-test\n"), &out, &out); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOpenRouterKey(paths)
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-or-test" {
		t.Fatalf("key=%q", key)
	}
	info, err := os.Stat(paths.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if bytes.Contains(out.Bytes(), []byte("sk-or-test")) {
		t.Fatal("secret echoed")
	}
}

func TestValidatedOpenRouterKeyIsNotSavedUntilPersisted(t *testing.T) {
	disableKeychainLookup(t)
	paths := Paths{ConfigDir: t.TempDir()}
	paths.Credentials = filepath.Join(paths.ConfigDir, "credentials.json")
	t.Setenv("OPENROUTER_API_KEY", "")
	key := "sk-or-staged"

	if err := ValidateOpenRouterCredential(key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Credentials); !os.IsNotExist(err) {
		t.Fatalf("validation wrote credentials: %v", err)
	}
	storage, err := PersistOpenRouterKey(paths, key, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if storage != "private credentials file" {
		t.Fatalf("storage=%q", storage)
	}
	loaded, err := LoadOpenRouterKey(paths)
	if err != nil || loaded != key {
		t.Fatalf("loaded=%q err=%v", loaded, err)
	}
}

func TestMacOSKeychainPersistenceKeepsSecretOutOfArgv(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS security command only")
	}
	root := t.TempDir()
	argsPath := filepath.Join(root, "args")
	inputPath := filepath.Join(root, "input")
	securityPath := filepath.Join(root, "security")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$HUMANSH_TEST_SECURITY_ARGS\"\n" +
		"IFS= read -r command\n" +
		"printf '%s\\n' \"$command\" > \"$HUMANSH_TEST_SECURITY_INPUT\"\n"
	if err := os.WriteFile(securityPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("HUMANSH_TEST_SECURITY_ARGS", argsPath)
	t.Setenv("HUMANSH_TEST_SECURITY_INPUT", inputPath)
	paths := Paths{ConfigDir: filepath.Join(root, "config"), Credentials: filepath.Join(root, "config", "credentials.json")}
	key := "sk-or-not-in-argv"

	storage, err := PersistOpenRouterKey(paths, key, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if storage != "macOS Keychain" {
		t.Fatalf("storage=%q", storage)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "-i\n" || bytes.Contains(args, []byte(key)) || bytes.Contains(args, []byte(hex.EncodeToString([]byte(key)))) {
		t.Fatalf("security argv exposed credential material: %q", args)
	}
	wantInput := "add-generic-password -U -s humansh.openrouter -a " + currentAccount() + " -X " + hex.EncodeToString([]byte(key)) + "\n"
	if string(input) != wantInput {
		t.Fatalf("security input=%q want=%q", input, wantInput)
	}
	if _, err := os.Stat(paths.Credentials); !os.IsNotExist(err) {
		t.Fatalf("successful Keychain storage also wrote a credential file: %v", err)
	}
}

func TestEnvironmentKeyHasPrecedence(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-secret")
	key, err := LoadOpenRouterKey(Paths{})
	if err != nil || key != "env-secret" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestConfigureOpenRouterKeyLeavesFollowingConfirmationUnread(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.Credentials = filepath.Join(paths.ConfigDir, "credentials.json")
	t.Setenv("OPENROUTER_API_KEY", "")
	input := strings.NewReader("test-only-key\ny\n")
	var output bytes.Buffer
	if err := ConfigureOpenRouterKey(paths, input, &output, &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	var saved credentialFile
	if err := json.Unmarshal(data, &saved); err != nil || saved.OpenRouterAPIKey != "test-only-key" {
		t.Fatalf("saved credential=%+v err=%v", saved, err)
	}
	confirmation, err := bufio.NewReader(input).ReadString('\n')
	if err != nil || confirmation != "y\n" {
		t.Fatalf("following input=%q err=%v", confirmation, err)
	}
}

func TestConfigureOpenRouterKeyBoundsNonTTYInput(t *testing.T) {
	paths := Paths{ConfigDir: t.TempDir()}
	paths.Credentials = filepath.Join(paths.ConfigDir, "credentials.json")
	t.Setenv("OPENROUTER_API_KEY", "")
	var output bytes.Buffer
	err := ConfigureOpenRouterKey(paths, strings.NewReader(strings.Repeat("x", (16<<10)+1)), &output, &output)
	if err == nil || !strings.Contains(err.Error(), "exceeds 16 KiB") {
		t.Fatalf("oversized secret error=%v", err)
	}
	if _, statErr := os.Stat(paths.Credentials); !os.IsNotExist(statErr) {
		t.Fatalf("oversized secret created credentials file: %v", statErr)
	}
}

func TestCredentialFallbackRejectsUnsafeInputAndSymlinks(t *testing.T) {
	disableKeychainLookup(t)
	configDir := t.TempDir()
	if err := os.Chmod(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := Paths{ConfigDir: configDir, Credentials: filepath.Join(configDir, "credentials.json")}
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	if err := ConfigureOpenRouterKey(paths, bytes.NewBufferString("bad key\n"), &out, &out); err == nil {
		t.Fatal("credential containing whitespace was persisted")
	}
	if _, err := os.Stat(paths.Credentials); !os.IsNotExist(err) {
		t.Fatalf("rejected credential created a file: %v", err)
	}
	target := filepath.Join(configDir, "target.json")
	if err := os.WriteFile(target, []byte(`{"openrouter_api_key":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.Credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOpenRouterKey(paths); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("credential symlink accepted: %v", err)
	}
}
