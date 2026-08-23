package classifier

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/humansh/humansh/internal/config"
	"github.com/humansh/humansh/internal/shell"
)

type Classification string

const (
	Literal   Classification = "literal"
	Natural   Classification = "natural_language"
	Ambiguous Classification = "ambiguous"
)

type EvidenceDomain string

const (
	CommandEvidence  EvidenceDomain = "command"
	EnglishEvidence  EvidenceDomain = "english"
	DecisionEvidence EvidenceDomain = "decision"
)

type Evidence struct {
	Domain EvidenceDomain `json:"domain"`
	Code   string         `json:"code"`
	Weight int            `json:"weight"`
	Detail string         `json:"detail,omitempty"`
}

type Input struct {
	Raw            string
	Shell          string
	FirstTokenKind shell.FirstTokenKind
	Overrides      config.ClassifierOverrides
}

type Result struct {
	Version        int                  `json:"version"`
	FirstTokenKind shell.FirstTokenKind `json:"first_token_kind,omitempty"`
	Outcome        Classification       `json:"outcome"`
	CommandScore   int                  `json:"command_score"`
	EnglishScore   int                  `json:"english_score"`
	DecisionCode   string               `json:"decision_code"`
	Evidence       []Evidence           `json:"evidence"`
}

type Classifier struct{}

var (
	assignmentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	flagRE       = regexp.MustCompile(`^--?[A-Za-z0-9]`)
	fileExtRE    = regexp.MustCompile(`(?i)^[A-Za-z0-9_.-]+\.[A-Za-z0-9]{1,10}$`)
)

var instructionPrefixes = []string{
	"show me", "tell me", "please", "help me", "can you", "could you", "i want to", "find me", "list the",
}

var questionPrefixes = []string{"how do i", "what is", "what are", "where is"}

var grammarLexicon = wordSet(`
a all an any each every no some the this that these those whatever whichever
he her hers him his i it its me mine my our ours she their theirs them they us we what which who whom whose you your yours
am are be been being can could did do does had has have is may might must shall should was were will would
about after at before by during for from if in into of on over through to under until with without
`)

var stopwords = wordSet(`the a an my me those these this that is are was were in during of to from by for it all`)

var negativeTailHeads = wordSet(`echo print printf man git docker kubectl npm pnpm yarn cargo brew gh humansh codex claude cursor cursor-agent agent`)

var naturalClauses = []string{
	"is using", "are using", "that were", "in this folder", "from the last", "modified today", "changed today",
	"changed during", "by size", "by memory", "is listening", "were running", "to the", "if the", "it faster",
}

func wordSet(words string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, word := range strings.Fields(words) {
		out[word] = struct{}{}
	}
	return out
}

func (Classifier) Classify(in Input) Result {
	raw := in.Raw
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return hard(in.FirstTokenKind, Literal, "empty_input")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return hard(in.FirstTokenKind, Literal, "multiline_input")
	}
	if strings.HasPrefix(strings.TrimLeftFunc(raw, unicode.IsSpace), "#") {
		return hard(in.FirstTokenKind, Literal, "leading_comment")
	}

	scan := scanLine(raw)
	first := ""
	if len(scan.tokens) > 0 {
		first = scan.tokens[0].text
	}
	commandOverride := containsExact(in.Overrides.AlwaysCommands, first)
	englishOverride := matchesPrefix(in.Overrides.AlwaysNaturalLanguagePrefixes, normalizeWords(trimmed))
	if commandOverride && englishOverride {
		return hard(in.FirstTokenKind, Ambiguous, "conflicting_user_overrides")
	}
	if commandOverride {
		return hard(in.FirstTokenKind, Literal, "configured_command_override")
	}
	if englishOverride {
		return hard(in.FirstTokenKind, Natural, "configured_english_prefix")
	}

	var commandEvidence, englishEvidence []Evidence
	add := func(dst *[]Evidence, domain EvidenceDomain, code string, weight int, detail string) {
		*dst = append(*dst, Evidence{Domain: domain, Code: code, Weight: weight, Detail: detail})
	}
	if resolved(in.FirstTokenKind) {
		add(&commandEvidence, CommandEvidence, "resolved_first_token", 5, "first token resolves in the active shell")
	}
	if scan.shellOperator {
		add(&commandEvidence, CommandEvidence, "shell_operator", 5, "contains an unquoted shell operator")
	}
	if explicitCommandPath(first) {
		add(&commandEvidence, CommandEvidence, "explicit_command_path", 5, "command begins with an explicit path")
	}
	if scan.assignmentPrefix {
		add(&commandEvidence, CommandEvidence, "assignment_prefix", 5, "contains a shell assignment prefix")
	}
	if shellControl(first) {
		add(&commandEvidence, CommandEvidence, "shell_control_construct", 5, "begins with a shell control construct")
	}
	if scan.commandSubstitution {
		add(&commandEvidence, CommandEvidence, "command_or_process_substitution", 4, "contains command or process substitution")
	}
	if scan.parameterExpansion {
		add(&commandEvidence, CommandEvidence, "parameter_expansion", 3, "contains parameter expansion")
	}
	if scan.flag {
		add(&commandEvidence, CommandEvidence, "conventional_flag", 3, "contains a conventional command flag")
	}
	if scan.glob {
		add(&commandEvidence, CommandEvidence, "glob_syntax", 3, "contains unquoted glob syntax")
	}
	if scan.quotedArgument && (resolved(in.FirstTokenKind) || explicitCommandPath(first)) {
		add(&commandEvidence, CommandEvidence, "quoted_argument", 2, "contains a quoted command argument")
	}
	if scan.pathArgument {
		add(&commandEvidence, CommandEvidence, "path_argument", 2, "contains a path-like argument")
	}

	normalized := normalizeWords(trimmed)
	words := ordinaryWords(scan.tokens)
	instruction := hasAnyPrefix(normalized, instructionPrefixes)
	question := hasAnyPrefix(normalized, questionPrefixes) || grammaticalWhich(normalized)
	if instruction {
		add(&englishEvidence, EnglishEvidence, "natural_instruction_prefix", 5, "begins with an explicit natural-language request")
	}
	if question {
		add(&englishEvidence, EnglishEvidence, "natural_question_prefix", 5, "begins with a grammatical question")
	}
	if strings.HasSuffix(strings.TrimSpace(trimmed), "?") && !scan.questionGlob {
		add(&englishEvidence, EnglishEvidence, "question_mark", 3, "ends with sentence punctuation")
	}
	noShellMarkers := !scan.shellOperator && !scan.flag && !scan.pathArgument && !scan.assignmentPrefix && !scan.commandSubstitution && !scan.parameterExpansion && !scan.glob && !scan.containsEquals
	ordinaryStructure := (instruction || in.FirstTokenKind == shell.TokenUnresolved || in.FirstTokenKind == shell.TokenUnknown) && len(words) >= 4 && noShellMarkers
	if ordinaryStructure {
		add(&englishEvidence, EnglishEvidence, "ordinary_sentence_structure", 3, "contains at least four ordinary words in sentence order")
	}
	tail := resolved(in.FirstTokenKind) && !setHas(negativeTailHeads, strings.ToLower(first)) && grammarTail(scan.tokens) && noShellMarkers
	if tail {
		add(&englishEvidence, EnglishEvidence, "natural_language_tail", 4, "resolved command is followed by a grammar-bearing English tail")
	}
	clause := containsClause(normalized) && (instruction || in.FirstTokenKind == shell.TokenUnresolved || in.FirstTokenKind == shell.TokenUnknown || tail)
	if clause {
		add(&englishEvidence, EnglishEvidence, "natural_clause", 3, "contains a natural-language clause")
	}
	if in.FirstTokenKind == shell.TokenUnresolved || in.FirstTokenKind == shell.TokenUnknown {
		add(&englishEvidence, EnglishEvidence, "unresolved_first_token", 2, "first token is unresolved in the active shell")
	}
	structural := instruction || question || ordinaryStructure || tail || clause
	if structural && mostlyOrdinary(scan.tokens) && noShellMarkers && !scan.containsEquals {
		add(&englishEvidence, EnglishEvidence, "mostly_ordinary_words", 2, "tail is predominantly ordinary alphabetic words")
	}
	if structural && stopwordCount(words) >= 2 {
		add(&englishEvidence, EnglishEvidence, "stopword_or_pronoun_density", 1, "contains multiple English function words")
	}

	result := Result{Version: 1, FirstTokenKind: in.FirstTokenKind, Evidence: append(commandEvidence, englishEvidence...)}
	for _, evidence := range commandEvidence {
		result.CommandScore += evidence.Weight
	}
	for _, evidence := range englishEvidence {
		result.EnglishScore += evidence.Weight
	}
	result.Outcome, result.DecisionCode = decide(result.CommandScore, result.EnglishScore)
	result.Evidence = append(result.Evidence, Evidence{Domain: DecisionEvidence, Code: result.DecisionCode})
	if resolved(in.FirstTokenKind) && result.EnglishScore >= 3 {
		result.Evidence = append(result.Evidence, Evidence{Domain: DecisionEvidence, Code: "known_command_with_natural_language_tail"})
	}
	if (in.FirstTokenKind == shell.TokenUnresolved || in.FirstTokenKind == shell.TokenUnknown) && result.CommandScore < 5 && result.EnglishScore < 5 && len(scan.tokens) <= 3 {
		result.Evidence = append(result.Evidence, Evidence{Domain: DecisionEvidence, Code: "unresolved_command_like_input"})
	}
	return result
}

func decide(commandScore, englishScore int) (Classification, string) {
	switch {
	case commandScore >= 5 && englishScore <= 2:
		return Literal, "strong_command_weak_english"
	case englishScore >= 5 && commandScore <= 2:
		return Natural, "strong_english_weak_command"
	case commandScore >= 5 && englishScore >= 5:
		return Ambiguous, "conflicting_strong_evidence"
	default:
		return Ambiguous, "insufficient_evidence"
	}
}

func hard(kind shell.FirstTokenKind, outcome Classification, code string) Result {
	_, decisionCode := decide(0, 0)
	return Result{Version: 1, FirstTokenKind: kind, Outcome: outcome, DecisionCode: decisionCode, Evidence: []Evidence{{Domain: DecisionEvidence, Code: decisionCode}, {Domain: DecisionEvidence, Code: code}}}
}

func resolved(kind shell.FirstTokenKind) bool {
	switch kind {
	case shell.TokenAlias, shell.TokenFunction, shell.TokenBuiltin, shell.TokenReserved, shell.TokenCommand:
		return true
	default:
		return false
	}
}

func containsExact(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func matchesPrefix(prefixes []string, normalized string) bool {
	for _, prefix := range prefixes {
		p := normalizeWords(prefix)
		if normalized == p || strings.HasPrefix(normalized, p+" ") {
			return true
		}
	}
	return false
}

func normalizeWords(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if value == prefix || strings.HasPrefix(value, prefix+" ") {
			return true
		}
	}
	return false
}

func grammaticalWhich(value string) bool {
	if !strings.HasPrefix(value, "which ") {
		return false
	}
	return strings.Contains(value, " is ") || strings.Contains(value, " are ") || strings.Contains(value, " uses ") || strings.Contains(value, " using ") || strings.Contains(value, " listening ")
}

func setHas(set map[string]struct{}, value string) bool { _, ok := set[value]; return ok }

func explicitCommandPath(first string) bool {
	return strings.HasPrefix(first, "./") || strings.HasPrefix(first, "../") || strings.HasPrefix(first, "/") || strings.HasPrefix(first, "~/")
}

func shellControl(first string) bool {
	switch first {
	case "if", "for", "while", "until", "case", "repeat", "select", "function", "{", "(":
		return true
	default:
		return false
	}
}

func grammarTail(tokens []token) bool {
	if len(tokens) < 3 {
		return false
	}
	ordinary, grammar := 0, false
	for _, token := range tokens[1:] {
		if token.quoted {
			continue
		}
		word := strings.ToLower(strings.Trim(token.text, ".,!?():"))
		if isAlpha(word) {
			ordinary++
			grammar = grammar || setHas(grammarLexicon, word)
		}
	}
	return ordinary >= 2 && grammar
}

func containsClause(normalized string) bool {
	words := strings.Fields(normalized)
	for _, clause := range naturalClauses {
		clauseWords := strings.Fields(clause)
		for start := 0; start+len(clauseWords) <= len(words); start++ {
			matched := true
			for offset, word := range clauseWords {
				if words[start+offset] != word {
					matched = false
					break
				}
			}
			if matched {
				return true
			}
		}
	}
	return false
}

func ordinaryWords(tokens []token) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		word := strings.Trim(token.text, ".,!?():")
		if isAlpha(word) {
			out = append(out, strings.ToLower(word))
		}
	}
	return out
}

func mostlyOrdinary(tokens []token) bool {
	if len(tokens) < 3 {
		return false
	}
	ordinary := 0
	for _, token := range tokens[1:] {
		if isAlpha(strings.Trim(token.text, ".,!?():")) {
			ordinary++
		}
	}
	return ordinary >= 2 && ordinary*4 >= (len(tokens)-1)*3
}

func stopwordCount(words []string) int {
	count := 0
	for _, word := range words {
		if setHas(stopwords, word) || setHas(grammarLexicon, word) {
			count++
		}
	}
	return count
}

func isAlpha(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

type token struct {
	text   string
	quoted bool
}

type scanResult struct {
	tokens              []token
	shellOperator       bool
	assignmentPrefix    bool
	commandSubstitution bool
	parameterExpansion  bool
	flag                bool
	glob                bool
	questionGlob        bool
	quotedArgument      bool
	pathArgument        bool
	containsEquals      bool
}

func scanLine(raw string) scanResult {
	var result scanResult
	var current strings.Builder
	quote := rune(0)
	escaped := false
	quotedToken := false
	flush := func() {
		if current.Len() == 0 {
			return
		}
		value := current.String()
		result.tokens = append(result.tokens, token{text: value, quoted: quotedToken})
		current.Reset()
		quotedToken = false
	}
	runes := []rune(raw)
	for i, r := range runes {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			current.WriteRune(r)
			continue
		}
		if quote != 0 {
			current.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			quotedToken = true
			current.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		if strings.ContainsRune("|&;<>", r) {
			result.shellOperator = true
		}
		if r == '`' || (r == '$' && i+1 < len(runes) && runes[i+1] == '(') || ((r == '<' || r == '>') && i+1 < len(runes) && runes[i+1] == '(') {
			result.commandSubstitution = true
		}
		if r == '$' && i+1 < len(runes) && (runes[i+1] == '{' || unicode.IsLetter(runes[i+1]) || runes[i+1] == '_') {
			result.parameterExpansion = true
		}
		if r == '*' || r == '[' || r == ']' {
			result.glob = true
		}
		if r == '?' && hasNonSpaceAfter(runes, i) {
			result.glob = true
			result.questionGlob = true
		}
		if r == '=' {
			result.containsEquals = true
		}
		current.WriteRune(r)
	}
	flush()
	for i, token := range result.tokens {
		clean := strings.Trim(token.text, "'\"")
		if i == 0 && assignmentRE.MatchString(clean) {
			result.assignmentPrefix = true
		}
		if i > 0 && !token.quoted && flagRE.MatchString(clean) {
			result.flag = true
		}
		if i > 0 && token.quoted {
			result.quotedArgument = true
		}
		if i > 0 && (strings.HasPrefix(clean, "~") || strings.Contains(clean, "/") || fileExtRE.MatchString(filepath.Base(clean))) {
			result.pathArgument = true
		}
	}
	return result
}

func hasNonSpaceAfter(runes []rune, index int) bool {
	for _, r := range runes[index+1:] {
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}
