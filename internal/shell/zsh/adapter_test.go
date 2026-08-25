package zsh

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agenticlab-ai/humansh/internal/shell"
	"github.com/agenticlab-ai/humansh/internal/shell/contracttest"
	"github.com/agenticlab-ai/humansh/internal/shell/protocol"
)

func TestSyntaxCheckDoesNotExecute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	command := "echo $(touch " + marker + ") > " + filepath.Join(dir, "output")
	if err := (Adapter{}).ValidateGenerated(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("syntax check executed command: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "output")); !os.IsNotExist(err) {
		t.Fatal("syntax check performed redirection")
	}
}

func TestSyntaxError(t *testing.T) {
	t.Parallel()
	if err := (Adapter{}).ValidateGenerated(context.Background(), "if then"); err == nil {
		t.Fatal("invalid syntax accepted")
	}
}

func TestShellContract(t *testing.T) {
	t.Parallel()
	contracttest.Run(t, Adapter{}, shell.Zsh, protocol.Version, true)
}
