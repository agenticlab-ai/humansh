package bash

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/humansh/humansh/internal/processrunner"
	"github.com/humansh/humansh/internal/shell"
	"github.com/humansh/humansh/internal/shell/contracttest"
	"github.com/humansh/humansh/internal/shell/protocol"
)

type versionRunner struct{ version string }

func (runner versionRunner) Run(context.Context, processrunner.Spec) (processrunner.Result, error) {
	return processrunner.Result{Stdout: []byte(runner.version)}, nil
}

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

func TestAdapterContract(t *testing.T) {
	t.Parallel()
	adapter := Adapter{}
	if adapter.ID() != shell.Bash || adapter.PromptProfile().Shell != "bash" {
		t.Fatalf("adapter identity=%q profile=%q", adapter.ID(), adapter.PromptProfile().Shell)
	}
	if protocols := adapter.SupportedProtocols(); len(protocols) != 1 || protocols[0] != protocol.ReadlineVersion {
		t.Fatalf("protocols=%v", protocols)
	}
	if asset, ok := adapter.IntegrationAsset(); !ok || len(asset) == 0 {
		t.Fatal("Bash integration asset missing")
	}
	diagnostic := adapter.Diagnose(context.Background())
	if !diagnostic.Installed || diagnostic.Version == "" {
		t.Fatalf("local diagnostic=%+v", diagnostic)
	}
	if err := adapter.ValidateGenerated(context.Background(), "printf '%s\\n' safe"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.ValidateGenerated(context.Background(), "if"); err == nil {
		t.Fatal("incomplete Bash accepted")
	}
}

func TestSharedShellContract(t *testing.T) {
	t.Parallel()
	adapter := Adapter{Runner: versionRunner{version: "GNU bash, version 5.2.37(1)-release"}}
	contracttest.Run(t, adapter, shell.Bash, protocol.ReadlineVersion, false)
}

func TestDiagnoseRequiresBashFourPointThree(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.2.57", "4.2.53"} {
		old := (Adapter{Runner: versionRunner{version: "GNU bash, version " + version + "(1)-release"}}).Diagnose(context.Background())
		if old.Available || !old.Installed {
			t.Fatalf("Bash %s diagnostic=%+v", version, old)
		}
	}
	minimum := (Adapter{Runner: versionRunner{version: "GNU bash, version 4.3.0(1)-release"}}).Diagnose(context.Background())
	if !minimum.Available || !minimum.Installed {
		t.Fatalf("Bash 4.3 diagnostic=%+v", minimum)
	}
	modern := (Adapter{Runner: versionRunner{version: "GNU bash, version 5.2.37(1)-release"}}).Diagnose(context.Background())
	if !modern.Available || !modern.Installed {
		t.Fatalf("Bash 5 diagnostic=%+v", modern)
	}
}
