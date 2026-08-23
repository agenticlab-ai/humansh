package contextinfo

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// Local inspects only fixed host metadata that the composition root explicitly
// injects into the provider-neutral app workflow.
type Local struct{}

func (Local) WorkingDirectoryLabel(mode, cwd string) string {
	if mode == "none" {
		return ""
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if mode == "full" {
		return cwd
	}
	home, homeErr := os.UserHomeDir()
	currentUser, userErr := user.Current()
	if homeErr != nil || userErr != nil || currentUser == nil || currentUser.Username == "" {
		return ""
	}
	base := filepath.Base(cwd)
	if cwd == home || base == currentUser.Username {
		return "~"
	}
	return base
}

var toolAllowlist = []string{"awk", "brew", "curl", "docker", "fd", "find", "fzf", "gh", "git", "grep", "jq", "kubectl", "lsof", "make", "node", "npm", "pnpm", "python3", "rg", "sed", "sort", "ssh", "tar", "xargs", "yarn"}

func (Local) AvailableTools() []string {
	out := make([]string, 0, len(toolAllowlist))
	for _, name := range toolAllowlist {
		if _, err := exec.LookPath(name); err == nil {
			out = append(out, name)
		}
	}
	return out
}
