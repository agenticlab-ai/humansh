package commandgrammar

import (
	"context"
	"strings"
	"testing"
)

func TestParseHelpCommonLayouts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		help         string
		commands     []string
		options      map[string]OptionSpec
		state        SubcommandState
		optionsKnown bool
	}{
		{
			name: "cobra",
			help: `Usage:
  fixturevcs [command]

Available Commands:
  commit      Record changes
  remote      Manage remotes
  status      Show status

Flags:
  -C, --directory string  run from a directory
      --color[=WHEN]      colorize output
  -h, --help              help for fixturevcs
`,
			commands: []string{"commit", "remote", "status"},
			options: map[string]OptionSpec{
				"-C":          {Value: RequiredValue, AllowSeparate: true},
				"--directory": {Value: RequiredValue, AllowSeparate: true},
				"--color":     {Value: OptionalValue, AllowAttached: true},
				"-h":          {Terminal: true},
				"--help":      {Terminal: true},
			},
			state: SubcommandsListed, optionsKnown: true,
		},
		{
			name: "git-like-common-commands",
			help: `usage: fixturevcs [--version] [--help] [-C <path>]
                  <command> [<args>]

These are common FixtureVCS commands used in various situations:
   commit     Record changes
   remote     Manage remotes
   status     Show status
`,
			commands: []string{"commit", "remote", "status"},
			options: map[string]OptionSpec{
				"--version": {Terminal: true},
				"--help":    {Terminal: true},
				"-C":        {Value: RequiredValue, AllowSeparate: true},
			},
			state: SubcommandsListed, optionsKnown: true,
		},
		{
			name: "argparse-choices-remain-unprobed",
			help: `usage: fixturevcs [-h] {commit,remote,status} ...

positional arguments:
  {commit,remote,status}

options:
  -h, --help  show this help message
`,
			options: map[string]OptionSpec{
				"-h": {Terminal: true}, "--help": {Terminal: true},
			},
			state: SubcommandsUnknown, optionsKnown: true,
		},
		{
			name: "click",
			help: `Usage: fixturevcs [OPTIONS] COMMAND [ARGS]...

Options:
  --config PATH  configuration file
  --help         show this message and exit

Commands:
  inspect  inspect state
  serve    run the service
`,
			commands: []string{"inspect", "serve"},
			options: map[string]OptionSpec{
				"--config": {Value: RequiredValue, AllowSeparate: true},
				"--help":   {Terminal: true},
			},
			state: SubcommandsListed, optionsKnown: true,
		},
		{
			name: "clap",
			help: `Usage: fixturevcs [OPTIONS] <COMMAND>

Commands:
  inspect  inspect state
  serve    run the service

Options:
  -q, --quiet        quiet output
      --jobs <COUNT> worker count
  -h, --help         Print help
`,
			commands: []string{"inspect", "serve"},
			options: map[string]OptionSpec{
				"-q": {}, "--quiet": {},
				"--jobs": {Value: RequiredValue, AllowSeparate: true},
				"-h":     {Terminal: true}, "--help": {Terminal: true},
			},
			state: SubcommandsListed, optionsKnown: true,
		},
		{
			name: "go-tool",
			help: `Go is a tool for managing source code.

Usage:
  gotool <command> [arguments]

The commands are:
  build       compile packages
  test        test packages
`,
			commands:     []string{"build", "test"},
			state:        SubcommandsListed,
			optionsKnown: true,
		},
		{
			name: "title-case-command-groups-and-bare-rows",
			help: `Usage: fixturectl [OPTIONS] COMMAND

Basic Commands (Beginner):
  create
  get       display resources

Other Commands:
  inspect, explain

Examples:
  create  is an example sentence, not another command
`,
			commands:     []string{"create", "get", "inspect", "explain"},
			state:        SubcommandsListed,
			optionsKnown: true,
		},
		{
			name: "wrapped-command-list",
			help: `Usage: package-tool <command>

All commands:
  access, adduser, audit,
  cache, ci, completion
`,
			commands:     []string{"access", "adduser", "audit", "cache", "ci", "completion"},
			state:        SubcommandsListed,
			optionsKnown: true,
		},
		{
			name: "man-overstrikes-and-ansi",
			help: "S\bSY\bYN\bNO\bOP\bPS\bSI\bIS\n    fixturevcs [--format=<VALUE>] FILE\n\n\x1b[1mOPTIONS\x1b[0m\n    -m <TEXT>, --message=<TEXT>\n        message text\n",
			options: map[string]OptionSpec{
				"--format":  {Value: RequiredValue, AllowAttached: true},
				"-m":        {Value: RequiredValue, AllowSeparate: true},
				"--message": {Value: RequiredValue, AllowAttached: true},
			},
			state: SubcommandsNone, optionsKnown: true,
		},
		{
			name: "headerless-options-negated-alias-and-optional-short-value",
			help: `usage: fixturevcs operation [<options>] <name> <url>
    -f, --[no-]fetch                    fetch after adding
    -t <BRANCH>, --track <BRANCH>       track a branch
    -u[<MODE>], --untracked-files[=<MODE>]
`,
			options: map[string]OptionSpec{
				"-f":                {},
				"--fetch":           {},
				"--no-fetch":        {},
				"-t":                {Value: RequiredValue, AllowSeparate: true},
				"--track":           {Value: RequiredValue, AllowSeparate: true},
				"-u":                {Value: OptionalValue, AllowAttached: true},
				"--untracked-files": {Value: OptionalValue, AllowAttached: true},
			},
			state: SubcommandsNone, optionsKnown: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			node, err := ParseHelp([]byte(test.help), true)
			if err != nil {
				t.Fatal(err)
			}
			if node.SubcommandState != test.state || node.OptionsKnown != test.optionsKnown || !node.Complete {
				t.Fatalf("node metadata=%+v", node)
			}
			for _, command := range test.commands {
				if _, ok := node.Subcommands[command]; !ok {
					t.Errorf("missing command %q in %v", command, node.Subcommands)
				}
			}
			for name, want := range test.options {
				if got, ok := node.Options[name]; !ok || got != want {
					t.Errorf("option %s=%+v,%t; want %+v", name, got, ok, want)
				}
			}
		})
	}
}

func TestParseHelpDoesNotPromoteArgumentChoicesOrExecutableSuffixes(t *testing.T) {
	t.Parallel()
	node, err := ParseHelp([]byte(`Usage: my-tool [--color {auto,always,never}] {json,yaml} FILE

Options:
  --color {auto,always,never}  color mode
`), true)
	if err != nil {
		t.Fatal(err)
	}
	if node.SubcommandState != SubcommandsUnknown {
		t.Fatalf("subcommand state=%v, want unknown positional structure", node.SubcommandState)
	}
	if len(node.Subcommands) != 0 {
		t.Fatalf("argument choices became executable subcommands: %v", node.Subcommands)
	}
	if _, exists := node.Options["-tool"]; exists {
		t.Fatalf("executable-name suffix became an option: %v", node.Options)
	}
	if _, exists := node.Options["--color"]; !exists {
		t.Fatalf("documented option missing: %v", node.Options)
	}

	plain, err := ParseHelp([]byte("Usage: my-tool FILE\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := plain.Options["-tool"]; exists {
		t.Fatalf("unbracketed executable-name suffix became an option: %v", plain.Options)
	}
}

func TestCommandDescriptionBraceEnumNeverBecomesAProbePrefix(t *testing.T) {
	t.Parallel()
	node, err := ParseHelp([]byte(`Usage: fixture [COMMAND]

Commands:
  render  output as {json,yaml}
`), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, undocumented := range []string{"json", "yaml"} {
		if _, exists := node.Subcommands[undocumented]; exists {
			t.Fatalf("description enum %q became a subcommand: %v", undocumented, node.Subcommands)
		}
	}

	session := &fakeHelpSession{nodes: map[string]HelpResult{
		"": {Node: node, Status: HelpOK},
	}}
	analysis := NewAnalyzer(&fakeHelpSource{session: session}).Analyze(context.Background(), invocation("fixture json please"))
	if analysis.StopReason != StopUndocumentedSubcommand || analysis.Boundary != 1 {
		t.Fatalf("analysis=%+v", analysis)
	}
	if got := strings.Join(session.calls, ","); got != "" {
		t.Fatalf("description enum reached a nested help probe: %q", got)
	}
}

func TestWrappedCommandDescriptionNeverBecomesAProbePrefix(t *testing.T) {
	t.Parallel()
	node, err := ParseHelp([]byte(`Usage: fixture [COMMAND]

Commands:
  render  Render output in a format that may wrap
          across  multiple lines
  status  Show status
`), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"render", "status"} {
		if _, exists := node.Subcommands[command]; !exists {
			t.Fatalf("documented command %q missing: %v", command, node.Subcommands)
		}
	}
	if _, exists := node.Subcommands["across"]; exists {
		t.Fatalf("wrapped description became a subcommand: %v", node.Subcommands)
	}

	session := &fakeHelpSession{nodes: map[string]HelpResult{
		"": {Node: node, Status: HelpOK},
	}}
	analysis := NewAnalyzer(&fakeHelpSource{session: session}).Analyze(context.Background(), invocation("fixture across please"))
	if analysis.StopReason != StopUndocumentedSubcommand || analysis.Boundary != 1 {
		t.Fatalf("analysis=%+v", analysis)
	}
	if got := strings.Join(session.calls, ","); got != "" {
		t.Fatalf("wrapped description reached a nested help probe: %q", got)
	}
}

func TestCommandSentenceNeverOpensAProbeableSection(t *testing.T) {
	t.Parallel()
	node, err := ParseHelp([]byte(`Usage: fixture FILE

Commands may be written across multiple lines.
  across  multiple examples
`), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := node.Subcommands["across"]; exists {
		t.Fatalf("prose sentence opened a command section: %v", node.Subcommands)
	}

	session := &fakeHelpSession{nodes: map[string]HelpResult{
		"": {Node: node, Status: HelpOK},
	}}
	analysis := NewAnalyzer(&fakeHelpSource{session: session}).Analyze(context.Background(), invocation("fixture across please"))
	if analysis.RoleAt(1) != RolePositional {
		t.Fatalf("prose-derived word was not left positional: %+v", analysis)
	}
	if got := strings.Join(session.calls, ","); got != "" {
		t.Fatalf("prose-derived word reached a nested help probe: %q", got)
	}
}

func TestParseHelpDistinguishesCompleteAndCommonCommandLists(t *testing.T) {
	t.Parallel()
	complete, err := ParseHelp([]byte("Usage: tool COMMAND\n\nAvailable Commands:\n  run  run it\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := ParseHelp([]byte("Usage: tool COMMAND\n\nThese are common tool commands:\n  run  run it\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if !complete.SubcommandsComplete || partial.SubcommandsComplete {
		t.Fatalf("complete=%t common=%t", complete.SubcommandsComplete, partial.SubcommandsComplete)
	}
}

func TestParseHelpRejectsProseAndBinaryData(t *testing.T) {
	t.Parallel()
	for _, value := range [][]byte{
		[]byte("This prose happens to mention --force and the status command."),
		[]byte("Usage: fixture\x00hidden"),
		[]byte(strings.Repeat("x", maxHelpParseBytes+1)),
	} {
		if node, err := ParseHelp(value, true); err == nil {
			t.Fatalf("ParseHelp accepted unsupported input: %+v", node)
		}
	}
}

func TestParseHelpMarksTruncatedOutputIncomplete(t *testing.T) {
	t.Parallel()
	node, err := ParseHelp([]byte("Usage: fixture [--help] FILE\n"), false)
	if err != nil || node.Complete {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func TestParseHelpRecognizesDynamicBareCommandMetavar(t *testing.T) {
	t.Parallel()
	node, err := ParseHelp([]byte("Usage: fixture [OPTIONS] COMMAND [ARGS]...\n"), true)
	if err != nil || node.SubcommandState != SubcommandsUnknown {
		t.Fatalf("node=%+v err=%v", node, err)
	}
}

func FuzzParseHelpNeverPanics(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("Usage: fixture [OPTIONS] <COMMAND>\nCommands:\n  run  run it\n"),
		[]byte("OPTIONS\n  -m <TEXT>, --message=<TEXT>\n"),
		[]byte("\x1b[31mUsage:\x1b[0m fixture FILE\n"),
		{0, 1, 2, 3},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value []byte) {
		if len(value) > maxHelpParseBytes+1 {
			t.Skip()
		}
		_, _ = ParseHelp(value, true)
	})
}
