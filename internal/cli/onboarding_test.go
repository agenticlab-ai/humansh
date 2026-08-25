package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/humansh/humansh/internal/config"
	"github.com/humansh/humansh/internal/shell"
	"github.com/humansh/humansh/internal/shell/protocol"
)

func TestOnboardingBeforeSetupShowsTheNextStep(t *testing.T) {
	isolatedEnv(t)
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"onboarding"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != protocol.ExitConfig || !strings.Contains(errOut.String(), "available after shell setup is complete") || !strings.Contains(errOut.String(), "humansh setup") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}

func TestZshOnboardingTeachesTwoStepReviewWithConfiguredBindings(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Shell.ForceTranslateBinding = "^R"
	cfg.Shell.ClearLineBinding = "^U"
	var out bytes.Buffer
	ui := newSetupUI(IO{In: strings.NewReader(""), Out: &out}, false)

	printOnboardingFlow([]shell.ID{shell.Zsh}, "", cfg, ui)

	for _, want := range []string{
		"Getting started with Humansh",
		"Zsh quick start",
		onboardingExample,
		"Press Enter once to translate it",
		"Nothing has run yet",
		"press Enter again to execute it",
		"press Ctrl-U to clear the line",
		"humansh onboarding [zsh|bash]",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Zsh onboarding missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Bash quick start") {
		t.Fatalf("Zsh-only onboarding included Bash:\n%s", out.String())
	}
}

func TestZshOnboardingUsesForceTranslationWhenSmartEnterIsOff(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Shell.SmartEnter = false
	cfg.Shell.ForceTranslateBinding = "^X^T"
	var out bytes.Buffer
	ui := newSetupUI(IO{In: strings.NewReader(""), Out: &out}, false)

	printOnboardingFlow([]shell.ID{shell.Zsh}, shell.Zsh, cfg, ui)

	if !strings.Contains(out.String(), "Press Ctrl-X then Ctrl-T to translate it. Smart Enter is off") || strings.Contains(out.String(), "Press Enter once to translate it") {
		t.Fatalf("Zsh onboarding ignored configured Smart Enter mode:\n%s", out.String())
	}
}

func TestOnboardingOffersOptionalBashWalkthrough(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Shell.ForceTranslateBinding = "^R"
	for _, test := range []struct {
		answer   string
		wantBash bool
	}{
		{answer: "n\n", wantBash: false},
		{answer: "y\n", wantBash: true},
	} {
		var out bytes.Buffer
		ui := newSetupUI(IO{In: strings.NewReader(test.answer), Out: &out}, true)
		printOnboardingFlow([]shell.ID{shell.Zsh, shell.Bash}, "", cfg, ui)

		if !strings.Contains(out.String(), "Show the Bash walkthrough too? [y/N]") {
			t.Fatalf("Bash walkthrough was not offered:\n%s", out.String())
		}
		hasBash := strings.Contains(out.String(), "Bash quick start")
		if hasBash != test.wantBash {
			t.Fatalf("answer=%q Bash guide=%t want %t:\n%s", test.answer, hasBash, test.wantBash, out.String())
		}
		if test.wantBash {
			for _, want := range []string{"Press Ctrl-R to translate it", "do not press Enter yet", "press Enter to execute it"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Bash onboarding missing %q:\n%s", want, out.String())
				}
			}
		}
	}
}
