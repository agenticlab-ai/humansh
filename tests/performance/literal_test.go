package performance_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// TestLiteralProcessStartupRegressionCeiling guards against a large regression
// in the cost of the literal path, which runs on every Enter press.
//
// It asserts on the median rather than the slowest sample. `go test ./...` runs
// packages concurrently, and the PTY suites in particular saturate the machine,
// so any single sample can stall for far longer than the code under test takes.
// A worst-of-N assertion turns that scheduling noise into a flaky failure in the
// default `make verify` path. The median stays stable under load while still
// moving decisively if startup genuinely regresses.
func TestLiteralProcessStartupRegressionCeiling(t *testing.T) {
	const samples = 15
	binary, helpCommand, environment := buildBinary(t)
	durations := make([]time.Duration, samples)
	for index := range durations {
		started := time.Now()
		command := exec.Command(binary, "smart", "--protocol", "zle-v1", "--shell", "zsh", "--first-token-kind", "command", "--resolved-command-path", helpCommand)
		command.Env = environment
		command.Stdin = bytes.NewBufferString("fixturevcs status")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("literal smart call: %v: %s", err, output)
		} else if len(output) != 0 {
			t.Fatalf("literal smart call emitted output: %q", output)
		}
		durations[index] = time.Since(started)
	}
	slices.Sort(durations)
	median := durations[samples/2]
	if median > time.Second {
		t.Fatalf("literal process median startup %s exceeds the generous 1s CI regression ceiling (slowest sample %s)", median, durations[samples-1])
	}
	t.Logf("literal process startup: median %s, slowest %s over %d samples", median, durations[samples-1], samples)
}

func BenchmarkLiteralProcessStartup(b *testing.B) {
	binary, helpCommand, environment := buildBinary(b)
	warmup := exec.Command(binary, "smart", "--protocol", "zle-v1", "--shell", "zsh", "--first-token-kind", "command", "--resolved-command-path", helpCommand)
	warmup.Env = environment
	warmup.Stdin = bytes.NewBufferString("fixturevcs status")
	if err := warmup.Run(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		command := exec.Command(binary, "smart", "--protocol", "zle-v1", "--shell", "zsh", "--first-token-kind", "command", "--resolved-command-path", helpCommand)
		command.Env = environment
		command.Stdin = bytes.NewBufferString("fixturevcs status")
		if err := command.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClassifierProcessStartup(b *testing.B) {
	binary, _, environment := buildBinary(b)
	run := func() {
		command := exec.Command(binary, "classify", "--json", "--shell", "zsh", "--first-token-kind", "command")
		command.Env = environment
		command.Stdin = bytes.NewBufferString("find all files modified today")
		command.Stdout = io.Discard
		command.Stderr = io.Discard
		if err := command.Run(); err != nil {
			b.Fatal(err)
		}
	}
	run()
	b.ResetTimer()
	for range b.N {
		run()
	}
}

func buildBinary(tb testing.TB) (string, string, []string) {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	tempDir := tb.TempDir()
	binary := filepath.Join(tempDir, "humansh")
	build := exec.Command("go", "build", "-o", binary, "./cmd/humansh")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		tb.Fatalf("build humansh: %v: %s", err, output)
	}
	helpCommand := filepath.Join(tempDir, "fixturevcs")
	buildHelp := exec.Command("go", "build", "-o", helpCommand, "./internal/commandgrammar/testdata/helpfixture")
	buildHelp.Dir = repository
	if output, err := buildHelp.CombinedOutput(); err != nil {
		tb.Fatalf("build command-help fixture: %v: %s", err, output)
	}
	home := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		tb.Fatal(err)
	}
	environment := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"CODEX_HOME=" + filepath.Join(home, "codex"),
	}
	for _, key := range []string{"PATH", "TMPDIR"} {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return binary, helpCommand, environment
}
