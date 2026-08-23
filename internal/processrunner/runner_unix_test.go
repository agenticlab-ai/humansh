//go:build darwin || linux

package processrunner

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestMinimalEnvIsAllowlistedAndDeterministic(t *testing.T) {
	t.Setenv("HUMANSH_SECRET_MARKER", "must-not-leak")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid")
	t.Setenv("ALL_PROXY", "socks5://proxy.invalid")
	first := MinimalEnv("/tmp/humansh-runner-test", map[string]string{"EXPLICIT_TEST": "yes"})
	second := MinimalEnv("/tmp/humansh-runner-test", map[string]string{"EXPLICIT_TEST": "yes"})
	if strings.Join(first, "\x00") != strings.Join(second, "\x00") || !sort.StringsAreSorted(first) {
		t.Fatalf("environment is not deterministic: %v / %v", first, second)
	}
	joined := strings.Join(first, "\n")
	for _, required := range []string{"TMPDIR=/tmp/humansh-runner-test", "EXPLICIT_TEST=yes", "HTTPS_PROXY=http://proxy.invalid", "ALL_PROXY=socks5://proxy.invalid"} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing %q in %v", required, first)
		}
	}
	if strings.Contains(joined, "HUMANSH_SECRET_MARKER") || strings.Contains(joined, "must-not-leak") {
		t.Fatalf("unrelated secret leaked: %v", first)
	}
}

func TestExecRunnerBoundsOutput(t *testing.T) {
	path := "/usr/bin/printf"
	if _, err := os.Stat(path); err != nil {
		path = "/bin/printf"
	}
	result, err := (ExecRunner{}).Run(context.Background(), Spec{Path: path, Args: []string{"%s", strings.Repeat("x", 128)}, Env: MinimalEnv(t.TempDir(), nil), MaxStdout: 32, MaxStderr: 32})
	if err == nil || !strings.Contains(err.Error(), "capture limit") || len(result.Stdout) != 32 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestExecRunnerCancellationStopsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := (ExecRunner{}).Run(ctx, Spec{Path: "/bin/sh", Args: []string{"-c", "sleep 10 & wait"}, Env: MinimalEnv(t.TempDir(), nil), MaxStdout: 32, MaxStderr: 32})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}
