package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/validate"
)

func TestRealCodexReleaseMeasurements(t *testing.T) {
	if os.Getenv("HUMANSH_REAL_CODEX_MEASUREMENTS") != "1" {
		t.Skip("set HUMANSH_REAL_CODEX_MEASUREMENTS=1 to run authenticated, quota-consuming Codex release measurements")
	}
	sampleCount := 5
	if value := os.Getenv("HUMANSH_REAL_CODEX_SAMPLES"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 3 || parsed > 20 {
			t.Fatalf("HUMANSH_REAL_CODEX_SAMPLES must be an integer from 3 through 20, got %q", value)
		}
		sampleCount = parsed
	}
	adapter := Adapter{Config: realCodexConfig(t)}
	diagnostic := adapter.Diagnose(context.Background())
	if !diagnostic.Available {
		t.Fatalf("Codex is not ready for release measurements: %+v", diagnostic)
	}
	requests := []string{
		"print the current working directory",
		"list hidden files in the current directory",
		"show which process is listening on port 3000",
		"find Go files modified during the last day",
		"show disk usage for entries here sorted by size",
	}
	durations := make([]time.Duration, 0, sampleCount)
	for index := 0; index < sampleCount; index++ {
		started := time.Now()
		response, err := adapter.Translate(context.Background(), llm.TranslationRequest{
			Input: requests[index%len(requests)], Shell: "zsh", OS: "darwin", Architecture: "arm64",
		})
		if err != nil {
			t.Fatalf("sample %d failed: %v", index+1, err)
		}
		if err := validate.Response(response); err != nil {
			t.Fatalf("sample %d failed local response validation: %v", index+1, err)
		}
		if response.Status != "ok" {
			t.Fatalf("sample %d returned an incomplete response: status=%q", index+1, response.Status)
		}
		if err := validate.Command(response.Command); err != nil {
			t.Fatalf("sample %d failed local command validation: %v", index+1, err)
		}
		durations = append(durations, time.Since(started))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[nearestRank(len(durations), 50)]
	p95 := durations[nearestRank(len(durations), 95)]
	record := struct {
		Provider     string `json:"provider"`
		Model        string `json:"model"`
		Client       string `json:"client_version"`
		Samples      int    `json:"samples"`
		InputTokens  string `json:"input_tokens"`
		OutputTokens string `json:"output_tokens"`
		TotalTokens  string `json:"total_tokens"`
		P50MS        int64  `json:"p50_ms"`
		P95MS        int64  `json:"p95_ms"`
		Date         string `json:"date"`
	}{
		Provider: "codex", Model: displayModel(adapter.Config.Model), Client: diagnostic.Version, Samples: sampleCount,
		InputTokens: "unavailable", OutputTokens: "unavailable", TotalTokens: "unavailable",
		P50MS: p50.Milliseconds(), P95MS: p95.Milliseconds(), Date: time.Now().Format("2006-01-02"),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("release measurement (copy into docs/providers.md):\n%s", data)
}

func realCodexConfig(t *testing.T) Config {
	t.Helper()
	authRoot := os.Getenv("CODEX_HOME")
	if authRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		authRoot = filepath.Join(home, ".codex")
	}
	return Config{
		AuthRecordPath: filepath.Join(authRoot, "auth.json"),
		Model:          os.Getenv("HUMANSH_REAL_CODEX_MODEL"),
		Timeout:        60 * time.Second,
	}
}

func nearestRank(length, percentile int) int {
	return (length*percentile+99)/100 - 1
}

func TestNearestRank(t *testing.T) {
	for _, test := range []struct {
		length, percentile, want int
	}{{3, 50, 1}, {3, 95, 2}, {5, 50, 2}, {5, 95, 4}, {20, 95, 18}} {
		if got := nearestRank(test.length, test.percentile); got != test.want {
			t.Errorf("nearestRank(%d, %d)=%d want %d", test.length, test.percentile, got, test.want)
		}
	}
}

func displayModel(model string) string {
	if model == "" {
		return "Codex built-in default"
	}
	return model
}
