package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(70)
	}
	mode := os.Args[1]
	if unavailable := os.Getenv("HUMANSH_FAKE_UNAVAILABLE"); unavailable != "" {
		if _, err := os.Stat(unavailable); err == nil {
			os.Exit(127)
		}
	}
	inputBytes, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	input := string(inputBytes)
	if path := os.Getenv("HUMANSH_FAKE_CALLS"); path != "" {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%s|%s\n", mode, strings.ReplaceAll(input, "\n", "\\n"))
			_, _ = fmt.Fprintf(file, "argv|%s|%q\n", mode, os.Args[2:])
			_ = file.Close()
		}
	}
	if mode == "analyze" {
		switch {
		case strings.Contains(input, "rm -rf"):
			os.Exit(14)
		case strings.HasPrefix(input, "mv "):
			os.Exit(13)
		default:
			os.Exit(10)
		}
	}
	if mode == "classify" {
		switch input {
		case "show me files", "move a file", "delete build", "configured English prefix files", "auth please", "unsupported please", "incomplete please", "policy please", "unicode please", "slow please", "split streams please", "empty success please", "parent cd", "parent export", "unknown result":
			// The real binary appends the provider label so the widget can render
			// its pre-blocking status without a shell-startup lookup.
			fmt.Print("translate Codex")
		case "ambiguous words", "echo show me the files", "which process is using port 3000", "open the project folder", "gti status":
			fmt.Print("ambiguous")
		default:
			fmt.Print("literal")
		}
		return
	}
	if mode == "translate" && input == "ambiguous words" {
		fmt.Print("print -r -- FORCED")
		os.Exit(10)
	}
	if mode != "smart" && mode != "translate" {
		os.Exit(70)
	}
	if mode == "smart" && strings.HasPrefix(input, `print -r -- "PARENT_`) {
		// The real binary classifies these resolved Zsh builtins as literal. Keep
		// the PTY fixture faithful now that both CR and LF Enter paths are gated.
		os.Exit(0)
	}
	switch input {
	case "git status", "which git", "configured-literal project", "mv old new":
		os.Exit(0)
	case "show me files":
		fmt.Print("print -r -- GENERATED_LOW")
		os.Exit(10)
	case "move a file":
		fmt.Print("mv old new")
		os.Exit(13)
	case "delete build":
		fmt.Print("rm -rf build")
		os.Exit(14)
	case "ambiguous words":
		fmt.Fprint(os.Stderr, "Not sure whether this is English or a command. Next: press Ctrl-G to translate it, or press Ctrl-X then Enter to run it unchanged.")
		os.Exit(11)
	case "echo show me the files", "which process is using port 3000", "open the project folder", "gti status":
		fmt.Fprint(os.Stderr, "Not sure whether this is English or a command. Next: press Ctrl-G to translate it, or press Ctrl-X then Enter to run it unchanged.")
		os.Exit(11)
	case "configured English prefix files":
		fmt.Print("print -r -- OVERRIDE_GENERATED")
		os.Exit(10)
	case "auth please":
		fmt.Fprint(os.Stderr, "humansh: provider-managed authentication failed. Check: run `humansh provider test codex`.")
		os.Exit(22)
	case "unsupported please":
		fmt.Fprint(os.Stderr, "This request cannot be represented as one shell command.")
		os.Exit(15)
	case "incomplete please":
		fmt.Fprint(os.Stderr, "Provider ended before producing a command. Nothing was changed or executed.")
		os.Exit(25)
	case "policy please":
		fmt.Fprint(os.Stderr, "The provider returned a command that was not safe to place in your terminal.")
		os.Exit(26)
	case "unicode please":
		fmt.Print("print -r -- café")
		os.Exit(10)
	case "slow please":
		time.Sleep(2 * time.Second)
		fmt.Print("print -r -- SLOW")
		os.Exit(10)
	case "split streams please":
		fmt.Fprint(os.Stderr, "provider diagnostic must not enter the command buffer")
		fmt.Print("print -r -- SPLIT")
		os.Exit(10)
	case "empty success please":
		os.Exit(10)
	case "parent cd":
		fmt.Printf("cd -- %q", os.Getenv("HUMANSH_FAKE_TARGET"))
		os.Exit(13)
	case "parent export":
		fmt.Print("export HUMANSH_PARENT_EFFECT=ok")
		os.Exit(13)
	case "unknown result":
		os.Exit(42)
	default:
		fmt.Fprint(os.Stderr, "unexpected fake input")
		os.Exit(70)
	}
}
