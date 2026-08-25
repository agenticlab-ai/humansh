package validate

import (
	"strings"
	"testing"
	"unicode/utf8"

	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/llm"
)

func TestExitMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		exit int
	}{
		{"empty-ok", Response(llm.TranslationResponse{Status: "ok", Assumptions: []string{}}), 25},
		{"missing-assumptions", Response(llm.TranslationResponse{Status: "ok", Command: "ls"}), 25},
		{"newline", Command("echo ok\nrm x"), 26},
		{"ansi", Command("echo \x1b[31m"), 26},
		{"markdown", Command("echo ```"), 26},
		{"prompt", Command("$ ls"), 26},
		{"surrounding-prose", Command("Here is the command: ls"), 26},
		{"alternatives", Command("ls; alternatively: pwd"), 26},
		{"eval", Command("command eval \"$payload\""), 26},
		{"encoded-interpreter", Command("python3 -c 'import base64; exec(base64.b64decode(x))'"), 26},
		{"clarification-control", Response(llm.TranslationResponse{Status: "clarify", Clarification: "which\x1b[31m?", Assumptions: []string{}}), 26},
		{"explanation-newline", Response(llm.TranslationResponse{Status: "unsupported", Explanation: "not one\ncommand", Assumptions: []string{}}), 26},
		{"assumption-bidi", Response(llm.TranslationResponse{Status: "ok", Command: "ls", Assumptions: []string{"safe\u202e"}}), 26},
		{"too-long", Command(strings.Repeat("x", 4097)), 25},
		{"invalid-utf8", Command(string([]byte{0xff, 0xfe})), 25},
		{"clarification-invalid-utf8", Response(llm.TranslationResponse{Status: "clarify", Clarification: string([]byte{0xff}), Assumptions: []string{}}), 25},
	}
	for _, test := range tests {
		typed, ok := usererr.As(test.err)
		if !ok || typed.ExitCode != test.exit {
			t.Errorf("%s error=%#v exit=%d", test.name, test.err, test.exit)
		}
	}
}

func TestCommandAcceptsOnePhysicalCommandLine(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"printf '%s' 'cats or dogs'",
		"echo 'Here is the command:'",
		"print -r -- café",
	} {
		if !utf8.ValidString(command) {
			t.Fatalf("bad test fixture %q", command)
		}
		if err := Command(command); err != nil {
			t.Errorf("Command(%q): %v", command, err)
		}
	}
}

func TestValidResponses(t *testing.T) {
	t.Parallel()
	for _, response := range []llm.TranslationResponse{{Status: "ok", Command: "ls", Explanation: "Lists files.", Assumptions: []string{}}, {Status: "clarify", Clarification: "Which directory?", Assumptions: []string{}}, {Status: "unsupported", Explanation: "Not a shell task.", Assumptions: []string{}}} {
		if err := Response(response); err != nil {
			t.Errorf("Response(%+v): %v", response, err)
		}
	}
}

func TestResponseBoundsCountUnicodeCharactersAndClarificationIsAQuestion(t *testing.T) {
	t.Parallel()
	valid := llm.TranslationResponse{Status: "unsupported", Explanation: strings.Repeat("é", 500), Assumptions: []string{strings.Repeat("é", 200)}}
	if err := Response(valid); err != nil {
		t.Fatalf("valid Unicode character bounds rejected: %v", err)
	}
	for name, response := range map[string]llm.TranslationResponse{
		"explanation-over-limit": {Status: "unsupported", Explanation: strings.Repeat("é", 501), Assumptions: []string{}},
		"assumption-over-limit":  {Status: "ok", Command: "ls", Assumptions: []string{strings.Repeat("é", 201)}},
		"not-a-question":         {Status: "clarify", Clarification: "Choose a directory", Assumptions: []string{}},
	} {
		if err := Response(response); err == nil {
			t.Errorf("%s response accepted", name)
		}
	}
}
