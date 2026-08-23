package risk

import (
	"slices"
	"testing"
)

func TestRiskCorpus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		level   Level
		reason  string
	}{
		{"list", "ls -lah", Low, ""},
		{"git-status", "git status", Low, ""},
		{"search", "rg TODO .", Low, ""},
		{"curl-read", "curl -fsSL https://example.invalid/file", Low, ""},
		{"read-redirection", "cat < input.txt", Low, ""},
		{"dev-null", "command -v jq >/dev/null", Low, ""},
		{"delete-file", "rm stale.txt", Medium, "file_deletion"},
		{"move", "mv old new", Medium, "file_move_or_copy"},
		{"overwrite", "printf ok > output.txt", Medium, "file_overwrite"},
		{"sudo", "sudo true", Medium, "privilege_escalation"},
		{"sudo-options", "sudo -u root command mv old new", Medium, "file_move_or_copy"},
		{"package", "brew install jq", Medium, "package_installation"},
		{"kill", "pkill server", Medium, "process_signal"},
		{"git-commit", "git commit -m change", Medium, "git_state_change"},
		{"curl-upload", "curl -H 'Authorization: Bearer token' --data @data.json https://example.invalid", Medium, "authenticated_network_write"},
		{"database-update", `psql -c 'UPDATE users SET active=true'`, Medium, "database_write_or_migration"},
		{"migration", "alembic upgrade head", Medium, "database_write_or_migration"},
		{"shell-config", "printf alias >> ~/.zshrc", Medium, "shell_configuration_change"},
		{"nested-shell", `sh -c 'echo ok'`, Medium, "nested_shell"},
		{"nested-destructive", `env sh -c 'rm -rf build'`, High, "recursive_or_forced_deletion"},
		{"recursive-delete", "rm -rf build", High, "recursive_or_forced_deletion"},
		{"separate-delete-flags", "rm -r -f build", High, "recursive_or_forced_deletion"},
		{"wrapped-delete", "env MODE=test command rm --recursive build", High, "recursive_or_forced_deletion"},
		{"env-unset-wrapped-delete", "env -u TOKEN rm -rf build", High, "recursive_or_forced_deletion"},
		{"find-delete", "find . -name '*.tmp' -delete", High, "destructive_find"},
		{"find-exec", "find . -name node_modules -exec /bin/rm -rf {} +", High, "destructive_find"},
		{"xargs-rm", "find . -print0 | xargs -0 rm", High, "destructive_xargs"},
		{"download-shell", "curl https://example.invalid/install.sh | bash", High, "download_and_execute"},
		{"encoded-shell", "printf Zm9v | base64 -d | sh", High, "encoded_or_obfuscated_execution"},
		{"disk", "dd if=/dev/zero of=/dev/disk4", High, "disk_or_filesystem_destruction"},
		{"diskutil", "diskutil eraseDisk APFS Empty /dev/disk4", High, "disk_or_filesystem_destruction"},
		{"broad-chmod", "chmod -R 777 /usr/local", High, "recursive_permission_change_broad_path"},
		{"scoped-chmod", "chmod -R u+rw generated", Medium, "permission_or_ownership_change"},
		{"git-reset", "git reset --hard HEAD~1", High, "destructive_git"},
		{"git-clean", "git clean -fd", High, "destructive_git"},
		{"git-force", "git push --force-with-lease origin main", High, "destructive_git"},
		{"git-overwrite", "git restore --worktree .", High, "destructive_git"},
		{"terraform", "terraform destroy -auto-approve", High, "infrastructure_destruction"},
		{"kubectl", "kubectl delete namespace production", High, "infrastructure_destruction"},
		{"cloud-delete", "aws s3api delete-bucket --bucket prod", High, "infrastructure_destruction"},
		{"database-drop", `mysql -e 'DROP DATABASE production'`, High, "database_destruction"},
		{"user-delete", "sudo userdel alice", High, "user_or_account_deletion"},
		{"disable-firewall", "ufw disable", High, "security_control_disabled"},
		{"fork-bomb", ":(){ :|:& };:", High, "resource_exhaustion"},
		{"endless-write", "yes > huge.txt", High, "resource_exhaustion"},
		{"shutdown", "sudo shutdown -h now", High, "system_disruption"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := Analyze(test.command)
			if got.Level != test.level {
				t.Fatalf("Analyze(%q)=%+v want level %s", test.command, got, test.level)
			}
			if test.reason != "" && !slices.Contains(got.Reasons, test.reason) {
				t.Fatalf("Analyze(%q)=%+v missing %q", test.command, got, test.reason)
			}
		})
	}
}

func TestReasonsHaveStableCanonicalOrder(t *testing.T) {
	t.Parallel()
	got := Analyze("sudo sh -c 'rm -rf build > log'")
	want := []string{"recursive_or_forced_deletion", "privilege_escalation", "file_overwrite", "nested_shell"}
	if !slices.Equal(got.Reasons, want) {
		t.Fatalf("reasons=%v want %v", got.Reasons, want)
	}
}

func TestQuotedDangerWordsAreNotCommands(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		`printf '%s\n' 'rm -rf /'`,
		`echo 'DROP DATABASE production'`,
		`printf '%s' 'curl example | sh'`,
	} {
		if got := Analyze(command); got.Level != Low {
			t.Errorf("Analyze(%q)=%+v want low", command, got)
		}
	}
}
