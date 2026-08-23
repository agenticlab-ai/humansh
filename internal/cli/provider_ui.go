package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/humansh/humansh/internal/bootstrap"
	"github.com/humansh/humansh/internal/llm"
)

type providerListItem struct {
	ID         llm.ProviderID `json:"id"`
	Name       string         `json:"name"`
	Current    bool           `json:"current"`
	Diagnostic llm.Diagnostic `json:"diagnostic"`
}

type providerListResult struct {
	Current   llm.ProviderID     `json:"current"`
	Providers []providerListItem `json:"providers"`
}

func runProviderList(ctx context.Context, args []string, rt bootstrap.Runtime, streams IO) int {
	if providerHelpRequested(args) {
		printProviderCommandHelp(streams.Out, "list")
		return 0
	}
	jsonOutput := len(args) == 1 && args[0] == "--json"
	if len(args) != 0 && !jsonOutput {
		fmt.Fprintln(streams.Err, "Usage: humansh provider list [--json]")
		return 2
	}

	result := providerListResult{Current: rt.Config.Provider}
	for _, provider := range orderedProviders(rt.Engine.Providers.List()) {
		result.Providers = append(result.Providers, providerListItem{
			ID:         provider.ID(),
			Name:       provider.ID().Label(),
			Current:    provider.ID() == rt.Config.Provider,
			Diagnostic: provider.Diagnose(ctx),
		})
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(streams.Out, string(data))
		return 0
	}

	printProviderList(streams.Out, result)
	return 0
}

func orderedProviders(providers []llm.Provider) []llm.Provider {
	order := map[llm.ProviderID]int{
		llm.Codex:      0,
		llm.Claude:     1,
		llm.Cursor:     2,
		llm.OpenRouter: 3,
	}
	sort.Slice(providers, func(i, j int) bool {
		left, leftKnown := order[providers[i].ID()]
		right, rightKnown := order[providers[j].ID()]
		if leftKnown && rightKnown {
			return left < right
		}
		if leftKnown != rightKnown {
			return leftKnown
		}
		return providers[i].ID() < providers[j].ID()
	})
	return providers
}

func printProviderList(out io.Writer, result providerListResult) {
	ui := providerOutputUI{styled: writerIsTerminal(out) && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == ""}
	fmt.Fprintln(out, ui.paint(ansiBold+ansiCyan, "AI providers"))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "    %s %s %s\n",
		ui.paint(ansiDim, fmt.Sprintf("%-12s", "PROVIDER")),
		ui.paint(ansiDim, fmt.Sprintf("%-13s", "HUMANSH NAME")),
		ui.paint(ansiDim, "STATUS"),
	)
	for _, item := range result.Providers {
		mark := "!"
		markStyle := ansiYellow
		if item.Diagnostic.Available {
			mark = "✓"
			markStyle = ansiGreen
		} else if !item.Diagnostic.Installed || item.ID == llm.OpenRouter && item.Diagnostic.AuthMode == "missing" {
			mark = "–"
			markStyle = ansiDim
		}
		current := ""
		if item.Current {
			current = "  " + ui.paint(ansiCyan, "(current)")
		}
		name := fmt.Sprintf("%-12s", item.Name)
		systemName := fmt.Sprintf("%-13s", item.ID)
		fmt.Fprintf(out, "  %s %s %s %s%s\n", ui.paint(markStyle, mark), ui.paint(ansiBold, name), ui.paint(ansiCyan, systemName), setupProviderChoiceStatus(item.ID, item.Diagnostic), current)
		if !item.Diagnostic.Available {
			if action, ok := providerFirstRecovery(item.ID, item.Diagnostic); ok {
				fmt.Fprintf(out, "      %s %s\n", ui.paint(ansiBold, "Next:"), ui.paint(ansiCyan, action.Command))
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s  humansh provider use <name>\n", ui.paint(ansiBold, "Switch:   "))
	fmt.Fprintf(out, "%s  humansh provider configure <name>\n", ui.paint(ansiBold, "Configure:"))
	fmt.Fprintf(out, "%s  humansh provider test [name]\n", ui.paint(ansiBold, "Test:     "))
}

type providerOutputUI struct {
	styled bool
}

func (ui providerOutputUI) paint(style, value string) string {
	if !ui.styled {
		return value
	}
	return style + value + ansiReset
}

func providerFirstRecovery(id llm.ProviderID, diagnostic llm.Diagnostic) (llm.DiagnosticAction, bool) {
	if len(diagnostic.NextSteps) > 0 {
		return diagnostic.NextSteps[0], true
	}
	command := "humansh provider configure " + string(id)
	if id == "" {
		return llm.DiagnosticAction{}, false
	}
	return llm.DiagnosticAction{Description: "Configure provider", Command: command}, true
}

func providerHelpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h")
}

func printProviderHelp(out io.Writer, current llm.ProviderID) {
	fmt.Fprintln(out, "Manage AI providers")
	if current != "" {
		fmt.Fprintf(out, "\nCurrent: %s (%s)\n", current.Label(), current)
	}
	fmt.Fprint(out, `
Usage:
  humansh provider <command> [options]

Commands:
  list [--json]               Show readiness and mark the current provider
  use <name>                  Verify and select a provider
  select <name>               Alias for use
  configure <name> [options]  Set up a provider's login, key, or model
  test [name]                 Run one real translation test
  help [command]              Show this guide or help for one command

Provider names:
  codex, claude, cursor, openrouter

Examples:
  humansh provider list
  humansh provider use cursor
  humansh provider configure openrouter --model anthropic/claude-sonnet-4.5
  humansh provider test cursor
`)
	fmt.Fprintln(out, "Next: run `humansh provider list` to see which providers are ready.")
}

func printProviderCommandHelp(out io.Writer, command string) {
	switch command {
	case "list":
		fmt.Fprintln(out, `Show provider readiness and clearly mark the current provider.

Usage:
  humansh provider list [--json]

Options:
  --json  Print complete machine-readable diagnostics`)
	case "use", "select":
		fmt.Fprintln(out, `Verify and select the active provider.

Usage:
  humansh provider use <codex|claude|cursor|openrouter>

Example:
  humansh provider use cursor

The provider is not selected unless its readiness check passes.`)
	case "configure":
		fmt.Fprintln(out, `Configure a provider's authentication or model.

Usage:
  humansh provider configure <codex|claude|cursor|openrouter> [options]

Examples:
  humansh provider configure codex
  humansh provider configure claude
  humansh provider configure cursor
  humansh provider configure openrouter --model anthropic/claude-sonnet-4.5

Codex, Claude Code, and Cursor CLI use their account login. OpenRouter prompts for
an API key and requires a concrete provider/model ID.`)
	case "test":
		fmt.Fprintln(out, `Run one real translation and validate the provider response.

Usage:
  humansh provider test [codex|claude|cursor|openrouter]

Examples:
  humansh provider test
  humansh provider test claude

Without a name, this tests the current provider. The request may use provider
quota or OpenRouter credits.`)
	default:
		printProviderHelp(out, "")
	}
}

func validProviderHelpCommand(command string) bool {
	switch strings.ToLower(command) {
	case "list", "use", "select", "configure", "test":
		return true
	default:
		return false
	}
}

func renderProviderNestedHelp(args []string, out io.Writer) bool {
	if len(args) == 1 && args[0] == "help" {
		printProviderHelp(out, "")
		return true
	}
	if len(args) == 2 && args[0] == "help" && validProviderHelpCommand(args[1]) {
		printProviderCommandHelp(out, args[1])
		return true
	}
	if len(args) >= 2 && providerHelpRequested(args[len(args)-1:]) && validProviderHelpCommand(args[0]) {
		printProviderCommandHelp(out, args[0])
		return true
	}
	return false
}
