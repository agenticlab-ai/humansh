package providerutil

import "testing"

func TestVersionFloor(t *testing.T) {
	for _, c := range []struct {
		reported     string
		min          [3]int
		meets, parse bool
	}{
		{"codex-cli-exec 0.149.0", [3]int{0, 148, 0}, true, true},
		{"codex-cli-exec 0.150.0", [3]int{0, 148, 0}, true, true},
		{"codex-cli-exec 1.0.0", [3]int{0, 148, 0}, true, true},
		{"codex-cli-exec 0.147.9", [3]int{0, 148, 0}, false, true},
		{"codex-cli-exec 0.148.0", [3]int{0, 148, 0}, true, true},
		{"2.1.238 (Claude Code)", [3]int{2, 1, 238}, true, true},
		{"2.1.239 (Claude Code)", [3]int{2, 1, 238}, true, true},
		{"2.2.0 (Claude Code)", [3]int{2, 1, 238}, true, true},
		{"3.0.0 (Claude Code)", [3]int{2, 1, 238}, true, true},
		{"2.1.237 (Claude Code)", [3]int{2, 1, 238}, false, true},
		{"2.0.999 (Claude Code)", [3]int{2, 1, 238}, false, true},
		{"2.1 (Claude Code)", [3]int{2, 1, 0}, true, true},
		{"unknown build", [3]int{2, 1, 238}, false, false},
		{"", [3]int{2, 1, 238}, false, false},
	} {
		meets, parsed := VersionFloor(c.reported, c.min)
		if meets != c.meets || parsed != c.parse {
			t.Errorf("VersionFloor(%q,%v) = %v,%v want %v,%v", c.reported, c.min, meets, parsed, c.meets, c.parse)
		}
	}
}
