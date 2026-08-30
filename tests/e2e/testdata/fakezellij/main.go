package main

import (
	"fmt"
	"os"
	"strings"
)

const executedOutput = "HUMANSH_E2E_ZELLIJ_EXECUTED:<attach|-c|pyxis-codex|--|codex>"

func main() {
	switch strings.Join(os.Args[1:], "\x00") {
	case "--help":
		printRootHelp()
	case "attach\x00--help":
		printAttachHelp()
	case "attach\x00-c\x00pyxis-codex\x00--\x00codex":
		fmt.Println(executedOutput)
	default:
		fmt.Fprintf(os.Stderr, "unexpected zellij fixture invocation: %q\n", os.Args[1:])
		os.Exit(97)
	}
}

func printRootHelp() {
	fmt.Println(`A terminal workspace with batteries included

Usage: zellij [OPTIONS] [COMMAND]

Commands:
  attach  Attach to a session [alias: a]
  help    Print this message or the help of the given subcommand(s)

Options:
  -h, --help  Print help`)
}

func printAttachHelp() {
	fmt.Println(`Attach to a session

Usage: zellij attach [OPTIONS] [SESSION_NAME] [-- <INITIAL_COMMAND>...] [COMMAND]

Commands:
  options  Change the behaviour of zellij
  help     Print this message or the help of the given subcommand(s)

Arguments:
  [SESSION_NAME]        Name of the session to attach to
  [INITIAL_COMMAND]...  Command to run in the first pane of the session, if it is created

Options:
  -c, --create  Create a session if one does not exist
  -h, --help    Print help`)
}
