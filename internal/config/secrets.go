package config

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const openRouterService = "humansh.openrouter"

type credentialFile struct {
	OpenRouterAPIKey string `json:"openrouter_api_key"`
}

func LoadOpenRouterKey(paths Paths) (string, error) {
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		return key, nil
	}
	if runtime.GOOS == "darwin" {
		account := currentAccount()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "security", "find-generic-password", "-w", "-s", openRouterService, "-a", account)
		var stdout, stderr limitedSecretBuffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err == nil {
			return trimLineEnding(stdout.String()), nil
		}
	}
	if err := requirePrivateFile(paths.Credentials); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	data, err := os.ReadFile(paths.Credentials)
	if err != nil {
		return "", err
	}
	if len(data) > 64<<10 {
		return "", fmt.Errorf("credentials file exceeds 64 KiB")
	}
	var file credentialFile
	if err := json.Unmarshal(data, &file); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}
	return file.OpenRouterAPIKey, nil
}

// ConfigureOpenRouterKey persists a key without ever placing it in argv.
// On macOS, security(1) performs its own hidden TTY prompt when possible.
func ConfigureOpenRouterKey(paths Paths, in io.Reader, out, errOut io.Writer) error {
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		fmt.Fprintln(out, "OPENROUTER_API_KEY is set; humansh will use it without persisting it.")
		return nil
	}
	if runtime.GOOS == "darwin" && isProcessTTY(in, out) {
		fmt.Fprintln(out, "macOS Keychain will securely prompt for the OpenRouter API key.")
		cmd := exec.Command("security", "add-generic-password", "-U", "-s", openRouterService, "-a", currentAccount(), "-w")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errOut
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			fmt.Fprintln(errOut, "Keychain storage failed; falling back to a mode-0600 credentials file.")
		}
	}
	key, err := readSecret(in, out)
	if err != nil {
		return err
	}
	key = trimLineEnding(key)
	if err := ValidateOpenRouterCredential(key); err != nil {
		return err
	}
	_, err = PersistOpenRouterKey(paths, key, false, errOut)
	return err
}

// PersistOpenRouterKey saves an already-collected key without placing it in
// process arguments. Interactive setup uses this only after final confirmation.
// When requested on macOS, security(1) reads a command containing hex password
// data from stdin; the key is never placed in argv. If Keychain is unavailable,
// humansh falls back to its private mode-0600 credential file.
func PersistOpenRouterKey(paths Paths, key string, preferKeychain bool, errOut io.Writer) (string, error) {
	if err := ValidateOpenRouterCredential(key); err != nil {
		return "", err
	}
	if preferKeychain && runtime.GOOS == "darwin" {
		if err := persistOpenRouterKeychain(key); err == nil {
			return "macOS Keychain", nil
		}
		if errOut != nil {
			fmt.Fprintln(errOut, "macOS Keychain storage was unavailable; using a private credentials file instead.")
		}
	}
	data, err := json.MarshalIndent(credentialFile{OpenRouterAPIKey: key}, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := ensurePrivateDirectory(paths.ConfigDir); err != nil {
		return "", err
	}
	if err := atomicWrite(paths.Credentials, data, 0o600); err != nil {
		return "", err
	}
	return "private credentials file", nil
}

func persistOpenRouterKeychain(key string) error {
	// `security add-generic-password ... -w` without a password argument enters
	// prompt mode. Interactive command mode lets humansh supply the already-read
	// secret through stdin instead, while -X avoids quoting arbitrary key bytes.
	command := fmt.Sprintf("add-generic-password -U -s %s -a %s -X %s\n", openRouterService, currentAccount(), hex.EncodeToString([]byte(key)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "-i")
	cmd.Stdin = strings.NewReader(command)
	var stdout, stderr limitedSecretBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	return cmd.Run()
}

func ValidateOpenRouterCredential(key string) error {
	if key == "" {
		return fmt.Errorf("OpenRouter API key cannot be empty")
	}
	if len(key) > 16<<10 || !utf8.ValidString(key) {
		return fmt.Errorf("OpenRouter API key must be valid UTF-8 no longer than 16 KiB")
	}
	for _, r := range key {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("OpenRouter API key cannot contain whitespace or control characters")
		}
	}
	return nil
}

func trimLineEnding(value string) string {
	value = strings.TrimSuffix(value, "\n")
	return strings.TrimSuffix(value, "\r")
}

func DeleteOpenRouterKey(paths Paths) error {
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = exec.CommandContext(ctx, "security", "delete-generic-password", "-s", openRouterService, "-a", currentAccount()).Run()
		cancel()
	}
	if err := os.Remove(paths.Credentials); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readSecret(in io.Reader, out io.Writer) (string, error) {
	fmt.Fprint(out, "OpenRouter API key: ")
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		data, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(out)
		return string(data), err
	}
	const maxBytes = 16 << 10
	value := make([]byte, 0, 64)
	var next [1]byte
	for {
		n, err := io.ReadFull(in, next[:])
		if n == 1 {
			if next[0] == '\n' {
				return string(value), nil
			}
			if len(value) == maxBytes {
				return "", fmt.Errorf("OpenRouter API key input exceeds 16 KiB")
			}
			value = append(value, next[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return string(value), nil
			}
			return "", err
		}
	}
}

func currentAccount() string {
	if current, err := user.Current(); err == nil && safeSecurityToken(current.Username) {
		return current.Username
	}
	return "humansh"
}

func safeSecurityToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-._@/", r) {
			return false
		}
	}
	return true
}

func isProcessTTY(in io.Reader, out io.Writer) bool {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	return inOK && outOK && term.IsTerminal(int(inFile.Fd())) && term.IsTerminal(int(outFile.Fd()))
}

type limitedSecretBuffer struct{ bytes.Buffer }

func (b *limitedSecretBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return len(p), fmt.Errorf("security output too large")
	}
	return b.Buffer.Write(p)
}
