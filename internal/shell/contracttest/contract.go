package contracttest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agenticlab-ai/humansh/internal/shell"
)

func Run(t *testing.T, adapter shell.Adapter, expectedID shell.ID, expectedProtocol string, conditionalAccept bool) {
	t.Helper()
	if adapter.ID() != expectedID {
		t.Fatalf("shell ID=%q want %q", adapter.ID(), expectedID)
	}
	diagnostic := adapter.Diagnose(context.Background())
	if !diagnostic.Installed || !diagnostic.Available || diagnostic.Version == "" {
		t.Fatalf("shell diagnostic does not satisfy available contract: %+v", diagnostic)
	}
	capabilities := adapter.Capabilities()
	if !capabilities.InspectEditableBuffer || !capabilities.ReplaceEditableBuffer || !capabilities.ExplicitPrefixMode || capabilities.ConditionalAccept != conditionalAccept {
		t.Fatalf("shell lacks required interactive capabilities: %+v", capabilities)
	}
	if profile := adapter.PromptProfile(); profile.Shell == "" {
		t.Fatal("shell omitted its provider-neutral prompt profile")
	}
	protocols := adapter.SupportedProtocols()
	if len(protocols) != 1 || protocols[0] != expectedProtocol {
		t.Fatalf("protocols=%v", protocols)
	}
	asset, ok := adapter.IntegrationAsset()
	if !ok || len(asset) == 0 {
		t.Fatal("interactive shell omitted its integration asset")
	}
	normalized, err := adapter.NormalizeGenerated("  printf safe  ")
	if err != nil || normalized != "printf safe" {
		t.Fatalf("normalized=%q err=%v", normalized, err)
	}
	if err := adapter.ValidateGenerated(context.Background(), normalized); err != nil {
		t.Fatalf("safe generated command rejected: %v", err)
	}
	if err := adapter.ValidateGenerated(context.Background(), "if"); err == nil {
		t.Fatal("syntactically incomplete generated command accepted")
	}
	sentinel := filepath.Join(t.TempDir(), "validation-must-not-execute")
	if err := adapter.ValidateGenerated(context.Background(), fmt.Sprintf("touch -- %q", sentinel)); err != nil {
		t.Fatalf("valid no-execution probe rejected: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("shell validation executed its input: %v", err)
	}
}
