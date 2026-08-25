package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/agenticlab-ai/humansh/internal/bootstrap"
	"github.com/agenticlab-ai/humansh/internal/config"
	"github.com/agenticlab-ai/humansh/internal/shell"
	"github.com/agenticlab-ai/humansh/internal/shell/protocol"
)

const onboardingExample = "list all the files in this directory"

func runOnboarding(_ context.Context, args []string, rt bootstrap.Runtime, streams IO) int {
	if len(args) > 1 {
		fmt.Fprintln(streams.Err, "Usage: humansh onboarding [zsh|bash]")
		return 2
	}

	state, err := config.LoadInstallState(rt.Paths.InstallState)
	if err != nil {
		fmt.Fprintln(streams.Err, "Humansh shell onboarding is available after shell setup is complete.")
		fmt.Fprintln(streams.Err, "Next: run `humansh setup`, then run `humansh onboarding` again.")
		return protocol.ExitConfig
	}
	configured := state.ShellIDs()
	if len(configured) == 0 {
		fmt.Fprintln(streams.Err, "No Humansh shell integration is configured.")
		fmt.Fprintln(streams.Err, "Next: run `humansh setup`, then run `humansh onboarding` again.")
		return protocol.ExitConfig
	}

	var requested shell.ID
	if len(args) == 1 {
		requested = shell.ID(strings.ToLower(args[0]))
		if requested != shell.Zsh && requested != shell.Bash {
			fmt.Fprintf(streams.Err, "Unknown shell %q. Choose zsh or bash.\n", args[0])
			return 2
		}
		if !onboardingHasShell(configured, requested) {
			fmt.Fprintf(streams.Err, "%s onboarding is unavailable because its Humansh integration is not configured.\n", shellDisplayName(requested))
			fmt.Fprintf(streams.Err, "Next: run `humansh setup --shell %s`, then try again.\n", requested)
			return protocol.ExitConfig
		}
	}

	interactive := readerIsTerminal(streams.In) && writerIsTerminal(streams.Out)
	ui := newSetupUI(streams, interactive)
	printOnboardingFlow(configured, requested, rt.Config, ui)
	return 0
}

func printOnboardingFlow(configured []shell.ID, requested shell.ID, cfg config.RuntimeConfig, ui *setupUI) {
	fmt.Fprintln(ui.streams.Out)
	fmt.Fprintln(ui.streams.Out, ui.paint(ansiBold+ansiCyan, "Getting started with Humansh"))
	fmt.Fprintln(ui.streams.Out, ui.paint(ansiDim, "Translate first, review the generated command, then choose whether to run it."))

	if requested != "" {
		printShellOnboarding(requested, cfg, ui)
		printOnboardingFooter(ui)
		return
	}

	hasZsh := onboardingHasShell(configured, shell.Zsh)
	hasBash := onboardingHasShell(configured, shell.Bash)
	if hasZsh {
		printShellOnboarding(shell.Zsh, cfg, ui)
	}
	if hasBash && !hasZsh {
		printShellOnboarding(shell.Bash, cfg, ui)
	} else if hasBash {
		if ui.interactive {
			showBash, err := ui.askYesNo("Show the Bash walkthrough too?", false)
			if err == nil && showBash {
				printShellOnboarding(shell.Bash, cfg, ui)
			}
		} else {
			fmt.Fprintln(ui.streams.Out)
			fmt.Fprintln(ui.streams.Out, "  Bash is configured too. Run `humansh onboarding bash` for its walkthrough.")
		}
	}
	printOnboardingFooter(ui)
}

func printShellOnboarding(id shell.ID, cfg config.RuntimeConfig, ui *setupUI) {
	name := shellDisplayName(id)
	fmt.Fprintln(ui.streams.Out)
	fmt.Fprintln(ui.streams.Out, ui.paint(ansiBold, name+" quick start"))
	fmt.Fprintf(ui.streams.Out, "  If %s was already open during installation, open a new %s terminal first.\n", name, name)
	fmt.Fprintln(ui.streams.Out)
	fmt.Fprintln(ui.streams.Out, "  1. Type this at the shell prompt, but do not execute it:")
	fmt.Fprintln(ui.streams.Out, "       "+ui.paint(ansiCyan, onboardingExample))

	translationKey := config.BindingLabel(cfg.Shell.ForceTranslateBinding)
	if id == shell.Zsh && cfg.Shell.SmartEnter {
		fmt.Fprintln(ui.streams.Out, "  2. Press Enter once to translate it.")
	} else if id == shell.Bash {
		fmt.Fprintf(ui.streams.Out, "  2. Press %s to translate it — do not press Enter yet; Bash keeps Enter for normal commands.\n", ui.paint(ansiBold, translationKey))
	} else {
		fmt.Fprintf(ui.streams.Out, "  2. Press %s to translate it. Smart Enter is off.\n", ui.paint(ansiBold, translationKey))
	}

	fmt.Fprintln(ui.streams.Out, "  3. Humansh replaces your request with a shell command for review. Nothing has run yet.")
	if id == shell.Zsh && cfg.Shell.SmartEnter {
		fmt.Fprintln(ui.streams.Out, "  4. If the command looks right, press Enter again to execute it.")
	} else {
		fmt.Fprintln(ui.streams.Out, "  4. If the command looks right, press Enter to execute it.")
	}
	fmt.Fprintf(ui.streams.Out, "     Otherwise, edit it or press %s to clear the line.\n", ui.paint(ansiBold, config.BindingLabel(cfg.Shell.ClearLineBinding)))
}

func printOnboardingFooter(ui *setupUI) {
	fmt.Fprintln(ui.streams.Out)
	fmt.Fprintln(ui.streams.Out, "  You can repeat this guide anytime with `humansh onboarding [zsh|bash]`.")
}

func onboardingHasShell(configured []shell.ID, target shell.ID) bool {
	for _, id := range configured {
		if id == target {
			return true
		}
	}
	return false
}
