package contracttest

import (
	"context"
	"testing"
	"time"

	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/exitcode"
	"github.com/agenticlab-ai/humansh/internal/llm"
)

type FailureCase func(context.Context) error

type Cases struct {
	Provider  llm.Provider
	ID        llm.ProviderID
	Malformed FailureCase
	Oversized FailureCase
}

// Run exercises behavior that every provider adapter must expose through the
// same contract. Transport-specific suites still verify their detailed wire
// shape and status catalogs.
func Run(t *testing.T, cases Cases) {
	t.Helper()
	if cases.Provider.ID() != cases.ID {
		t.Fatalf("provider ID=%q want %q", cases.Provider.ID(), cases.ID)
	}
	diagnostic := cases.Provider.Diagnose(context.Background())
	if !diagnostic.Installed || !diagnostic.Configured || !diagnostic.Authenticated || !diagnostic.Available || diagnostic.AuthMode == "" {
		t.Fatalf("provider diagnostic does not satisfy available contract: %+v", diagnostic)
	}
	response, err := cases.Provider.Translate(context.Background(), llm.TranslationRequest{Input: "list files", Shell: "zsh", OS: "test", Architecture: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Command == "" || response.Clarification != "" || response.Assumptions == nil {
		t.Fatalf("provider response does not satisfy common contract: %+v", response)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err = cases.Provider.Translate(cancelled, llm.TranslationRequest{Input: "cancel", Shell: "zsh"})
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("cancelled request error=%v elapsed=%s", err, time.Since(started))
	}
	if _, ok := usererr.As(err); !ok {
		t.Fatalf("cancelled request did not return a typed user error: %#v", err)
	}

	for _, failure := range []struct {
		name  string
		check FailureCase
	}{{"malformed", cases.Malformed}, {"oversized", cases.Oversized}} {
		t.Run(failure.name, func(t *testing.T) {
			check := failure.check
			if check == nil {
				t.Fatal("provider contract omitted required failure case")
			}
			err := check(context.Background())
			typed, ok := usererr.As(err)
			if !ok || typed.ExitCode != exitcode.ProviderMalformed {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}
