package classifier

import (
	"slices"
	"testing"
	"time"

	"github.com/agenticlab-ai/humansh/internal/config"
	"github.com/agenticlab-ai/humansh/internal/shell"
)

func TestPureClassifierP95Target(t *testing.T) {
	classifier := Classifier{}
	durations := make([]time.Duration, 1000)
	for index := range durations {
		started := time.Now()
		result := classifier.Classify(Input{Raw: "find all files modified today", Shell: "zsh", FirstTokenKind: shell.TokenCommand, Overrides: config.DefaultOverrides()})
		durations[index] = time.Since(started)
		if result.Outcome != Ambiguous {
			t.Fatalf("classification=%+v", result)
		}
	}
	slices.Sort(durations)
	p95 := durations[(len(durations)*95/100)-1]
	if p95 >= 10*time.Millisecond {
		t.Fatalf("pure classifier p95 %s exceeds 10ms target", p95)
	}
}

func TestNormativeExamples(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		kind shell.FirstTokenKind
		want Classification
	}{
		{"git", "git status", shell.TokenCommand, Literal},
		{"flags-path", "ls -lah ~/Downloads", shell.TokenCommand, Literal},
		{"assignment", "FOO=bar", shell.TokenUnresolved, Literal},
		{"pipeline", "cat file.txt | grep error", shell.TokenCommand, Literal},
		{"echo-tail", "echo show me the files", shell.TokenBuiltin, Literal},
		{"echo-quoted", `echo "show me the files"`, shell.TokenBuiltin, Literal},
		{"which", "which git", shell.TokenCommand, Literal},
		{"open-file", "open README.md", shell.TokenCommand, Literal},
		{"find-command", "find . -type f -mtime -1", shell.TokenCommand, Literal},
		{"instruction", "show me the largest files in this folder", shell.TokenUnresolved, Natural},
		{"question", "how do I see what is listening on port 3000", shell.TokenUnresolved, Natural},
		{"list", "list all files changed during the last two days", shell.TokenUnresolved, Natural},
		{"find-tail", "find all files modified today", shell.TokenCommand, Ambiguous},
		{"which-tail", "which process is using port 3000", shell.TokenCommand, Ambiguous},
		{"open-tail", "open the project folder", shell.TokenCommand, Ambiguous},
		{"sort-tail", "sort these files by size", shell.TokenCommand, Ambiguous},
		{"kill-tail", "kill whatever is using port 3000", shell.TokenBuiltin, Ambiguous},
		{"time-tail", "time the build", shell.TokenReserved, Ambiguous},
		{"watch-tail", "watch the logs", shell.TokenCommand, Ambiguous},
		{"top-tail", "top processes by memory", shell.TokenCommand, Ambiguous},
		{"who-tail", "who is using port 80", shell.TokenCommand, Ambiguous},
		{"make-tail", "make it faster", shell.TokenCommand, Ambiguous},
		{"head-tail", "head to the downloads folder", shell.TokenCommand, Ambiguous},
		{"test-tail", "test if the port is open", shell.TokenBuiltin, Ambiguous},
		{"typo", "gti status", shell.TokenUnresolved, Ambiguous},
		{"custom", "foo bar baz", shell.TokenUnresolved, Ambiguous},
		{"redirect", "not-a-command > existing-file", shell.TokenUnresolved, Literal},
		{"negative-dependency", "docker ps that were running", shell.TokenCommand, Literal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := (Classifier{}).Classify(Input{Raw: test.raw, Shell: "zsh", FirstTokenKind: test.kind, Overrides: config.DefaultOverrides()})
			if got.Outcome != test.want {
				t.Fatalf("Classify(%q) = %s (command=%d english=%d evidence=%+v), want %s", test.raw, got.Outcome, got.CommandScore, got.EnglishScore, got.Evidence, test.want)
			}
		})
	}
}

func TestGrammarLexiconLoadBearingRows(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"find all files modified today", "make it faster"} {
		result := (Classifier{}).Classify(Input{Raw: raw, FirstTokenKind: shell.TokenCommand})
		if !hasEvidence(result, "natural_language_tail") {
			t.Fatalf("%q did not receive natural_language_tail: %+v", raw, result.Evidence)
		}
	}
}

func TestNegativeListBlocksDependentRules(t *testing.T) {
	t.Parallel()
	result := (Classifier{}).Classify(Input{Raw: "docker ps that were running", FirstTokenKind: shell.TokenCommand})
	for _, code := range []string{"natural_language_tail", "natural_clause", "mostly_ordinary_words"} {
		if hasEvidence(result, code) {
			t.Fatalf("negative-list command received %s: %+v", code, result.Evidence)
		}
	}
}

func TestOverrides(t *testing.T) {
	t.Parallel()
	overrides := config.ClassifierOverrides{Version: 1, AlwaysCommands: []string{"deploy"}, AlwaysNaturalLanguagePrefixes: []string{"explain how to"}}
	classifier := Classifier{}
	if got := classifier.Classify(Input{Raw: "deploy production", FirstTokenKind: shell.TokenUnresolved, Overrides: overrides}); got.Outcome != Literal {
		t.Fatalf("command override = %s", got.Outcome)
	}
	if got := classifier.Classify(Input{Raw: "Explain   how to list files", FirstTokenKind: shell.TokenUnresolved, Overrides: overrides}); got.Outcome != Natural {
		t.Fatalf("English override = %s", got.Outcome)
	}
}

func TestDecisionCodeAlwaysUsesThresholdVocabulary(t *testing.T) {
	allowed := map[string]bool{
		"strong_command_weak_english": true,
		"strong_english_weak_command": true,
		"conflicting_strong_evidence": true,
		"insufficient_evidence":       true,
	}
	overrides := config.ClassifierOverrides{Version: 1, AlwaysCommands: []string{"deploy"}, AlwaysNaturalLanguagePrefixes: []string{"explain"}}
	for _, input := range []Input{
		{Raw: "", FirstTokenKind: shell.TokenEmpty},
		{Raw: "# comment", FirstTokenKind: shell.TokenUnknown},
		{Raw: "one\ntwo", FirstTokenKind: shell.TokenUnknown},
		{Raw: "deploy now", FirstTokenKind: shell.TokenUnknown, Overrides: overrides},
		{Raw: "explain files", FirstTokenKind: shell.TokenUnknown, Overrides: overrides},
		{Raw: "git status", FirstTokenKind: shell.TokenCommand},
	} {
		result := (Classifier{}).Classify(input)
		if !allowed[result.DecisionCode] {
			t.Errorf("raw=%q decision=%q evidence=%+v", input.Raw, result.DecisionCode, result.Evidence)
		}
		decisionIndex := -1
		for index, evidence := range result.Evidence {
			if evidence.Domain == DecisionEvidence {
				decisionIndex = index
				break
			}
		}
		if decisionIndex < 0 || result.Evidence[decisionIndex].Code != result.DecisionCode || result.Evidence[decisionIndex].Weight != 0 {
			t.Errorf("primary decision evidence is not first for raw=%q: %+v", input.Raw, result.Evidence)
		}
	}
}

func TestFocusedScannerAndEvidenceInvariants(t *testing.T) {
	classifier := Classifier{}
	tests := []struct {
		name      string
		first     Input
		second    *Input
		required  []string
		forbidden []string
	}{
		{name: "quoted-operators-have-no-unquoted-evidence", first: Input{Raw: `echo 'show me files | *.go --all'`, FirstTokenKind: shell.TokenBuiltin}, required: []string{"quoted_argument"}, forbidden: []string{"shell_operator", "glob_syntax", "conventional_flag", "natural_instruction_prefix"}},
		{name: "escaped-glob-is-not-expanded", first: Input{Raw: `print \*.go`, FirstTokenKind: shell.TokenBuiltin}, forbidden: []string{"glob_syntax"}},
		{name: "sentence-question-is-not-a-glob", first: Input{Raw: "show me files?", FirstTokenKind: shell.TokenUnresolved}, required: []string{"question_mark"}, forbidden: []string{"glob_syntax"}},
		{name: "filename-question-is-a-glob", first: Input{Raw: "print file?.txt", FirstTokenKind: shell.TokenBuiltin}, required: []string{"glob_syntax"}, forbidden: []string{"question_mark"}},
		{name: "malformed-quote-does-not-create-English-intent", first: Input{Raw: `echo "show me files`, FirstTokenKind: shell.TokenBuiltin}, forbidden: []string{"natural_instruction_prefix", "natural_language_tail", "mostly_ordinary_words"}},
		{name: "assignment-disqualifies-mostly-ordinary", first: Input{Raw: "FOO=bar show me files", FirstTokenKind: shell.TokenUnresolved}, required: []string{"assignment_prefix"}, forbidden: []string{"mostly_ordinary_words"}},
		{name: "non-head-assignment-disqualifies-English-tail", first: Input{Raw: "find target=value in folder", FirstTokenKind: shell.TokenCommand}, forbidden: []string{"natural_language_tail", "natural_clause", "mostly_ordinary_words"}},
		{name: "grammar-tail-counts-from-non-alphabetic-head", first: Input{Raw: "probe-tool all files", FirstTokenKind: shell.TokenCommand}, required: []string{"natural_language_tail"}},
		{name: "clause-matches-whole-words", first: Input{Raw: "probe into theater folder", FirstTokenKind: shell.TokenCommand}, forbidden: []string{"natural_clause"}},
		{name: "sentence-question-allows-trailing-space", first: Input{Raw: "show me files?  ", FirstTokenKind: shell.TokenUnresolved}, required: []string{"question_mark"}, forbidden: []string{"glob_syntax"}},
		{name: "semantic-whitespace", first: Input{Raw: "show me files changed today", FirstTokenKind: shell.TokenUnresolved}, second: &Input{Raw: "  show   me  files changed today  ", FirstTokenKind: shell.TokenUnresolved}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := test.first.Raw
			first := classifier.Classify(test.first)
			if test.first.Raw != before {
				t.Fatalf("classifier mutated raw input: before=%q after=%q", before, test.first.Raw)
			}
			for _, code := range test.required {
				if !hasEvidence(first, code) {
					t.Errorf("required evidence %q missing: %+v", code, first.Evidence)
				}
			}
			for _, code := range test.forbidden {
				if hasEvidence(first, code) {
					t.Errorf("forbidden evidence %q present: %+v", code, first.Evidence)
				}
			}
			if test.second != nil {
				second := classifier.Classify(*test.second)
				if first.Outcome != second.Outcome || first.CommandScore != second.CommandScore || first.EnglishScore != second.EnglishScore || first.DecisionCode != second.DecisionCode || !slices.Equal(first.Evidence, second.Evidence) {
					t.Fatalf("whitespace changed semantic result:\nfirst=%+v\nsecond=%+v", first, second)
				}
			}
		})
	}
}

func FuzzClassifier(f *testing.F) {
	f.Add("git status")
	f.Add("show me files")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			t.Skip()
		}
		result := (Classifier{}).Classify(Input{Raw: raw, FirstTokenKind: shell.TokenUnknown})
		switch result.Outcome {
		case Literal, Natural, Ambiguous:
		default:
			t.Fatalf("invalid outcome %q", result.Outcome)
		}
	})
}

func BenchmarkClassifier(b *testing.B) {
	classifier := Classifier{}
	in := Input{Raw: "find all files modified today", FirstTokenKind: shell.TokenCommand}
	for range b.N {
		_ = classifier.Classify(in)
	}
}

func hasEvidence(result Result, code string) bool {
	for _, evidence := range result.Evidence {
		if evidence.Code == code {
			return true
		}
	}
	return false
}
