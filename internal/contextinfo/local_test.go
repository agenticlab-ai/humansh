package contextinfo

import (
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkingDirectoryLabelPrivacyModes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	local := Local{}
	if got := local.WorkingDirectoryLabel("none", filepath.Join(home, "private")); got != "" {
		t.Fatalf("none label=%q", got)
	}
	full := filepath.Join(home, "projects", "humansh")
	if got := local.WorkingDirectoryLabel("full", full); got != full {
		t.Fatalf("full label=%q", got)
	}
	if got := local.WorkingDirectoryLabel("basename", full); got != "humansh" {
		t.Fatalf("basename label=%q", got)
	}
	if got := local.WorkingDirectoryLabel("basename", home); got != "~" {
		t.Fatalf("home label=%q", got)
	}
	currentUser, err := user.Current()
	if err == nil && currentUser.Username != "" {
		usernamePath := filepath.Join(filepath.Dir(home), currentUser.Username)
		if got := local.WorkingDirectoryLabel("basename", usernamePath); got != "~" {
			t.Fatalf("username basename leaked as %q", got)
		}
	}
}

func TestAvailableToolsUsesOnlyFixedAllowlist(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"git", "rg", "not-allowed"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	if got, want := (Local{}).AvailableTools(), []string{"git", "rg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tools=%v want %v", got, want)
	}
}
