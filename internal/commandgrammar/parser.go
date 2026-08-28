package commandgrammar

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxHelpParseBytes = 256 << 10

var (
	ansiCSI       = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	ansiOSC       = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	optionNameRE  = regexp.MustCompile(`--?[A-Za-z0-9][A-Za-z0-9_-]*`)
	negatedOptRE  = regexp.MustCompile(`--\[no-\]([A-Za-z0-9][A-Za-z0-9_-]*)`)
	commandRowRE  = regexp.MustCompile(`^\s{2,}([A-Za-z0-9][A-Za-z0-9._+-]*(?:\s*,\s*[A-Za-z0-9][A-Za-z0-9._+-]*)*)\s{2,}\S`)
	braceChoiceRE = regexp.MustCompile(`\{([A-Za-z0-9._+-]+(?:\s*,\s*[A-Za-z0-9._+-]+)+)\}`)
	commandMetaRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:SUB)?COMMANDS?(?:$|[^A-Za-z0-9_])`)
)

var metavarWords = map[string]struct{}{
	"arg": {}, "args": {}, "bool": {}, "command": {}, "count": {}, "dir": {}, "directory": {},
	"duration": {}, "file": {}, "float": {}, "int": {}, "name": {}, "number": {}, "path": {},
	"string": {}, "text": {}, "uint": {}, "value": {}, "values": {},
}

// ParseHelp normalizes common help/manual layouts into a generic command node.
// It extracts syntax only from usage, command, and option regions; prose is not
// treated as grammar.
func ParseHelp(data []byte, complete bool) (NodeSpec, error) {
	if len(data) == 0 || len(data) > maxHelpParseBytes || bytes.IndexByte(data, 0) >= 0 {
		return NodeSpec{}, errors.New("help output is empty, binary, or too large")
	}
	if !utf8.Valid(data) {
		return NodeSpec{}, errors.New("help output is not UTF-8")
	}
	text := normalizeHelp(string(data))
	if strings.TrimSpace(text) == "" {
		return NodeSpec{}, errors.New("help output is empty after normalization")
	}

	node := NodeSpec{
		Options:             make(map[string]OptionSpec),
		Subcommands:         make(map[string]struct{}),
		SubcommandsComplete: false,
		Complete:            complete,
	}
	var commandSection, optionSection, synopsisSection, usageContinuation bool
	var sawStructure, sawUsage, sawCommandMarker bool
	type commandCandidate struct {
		indent int
		names  []string
	}
	var commandCandidates []commandCandidate
	flushCommandCandidates := func() {
		if len(commandCandidates) == 0 {
			return
		}
		commandIndent := commandCandidates[0].indent
		for _, candidate := range commandCandidates[1:] {
			if candidate.indent < commandIndent {
				commandIndent = candidate.indent
			}
		}
		for _, candidate := range commandCandidates {
			if candidate.indent != commandIndent {
				continue
			}
			for _, name := range candidate.names {
				node.Subcommands[name] = struct{}{}
			}
		}
		commandCandidates = nil
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" {
			usageContinuation = false
			continue
		}

		if isCommandHeader(trimmed) {
			flushCommandCandidates()
			commandSection, optionSection, synopsisSection, usageContinuation = true, false, false, false
			sawStructure, sawCommandMarker = true, true
			node.SubcommandsComplete = node.SubcommandsComplete || completeCommandHeader(trimmed)
			continue
		}
		if isOptionHeader(trimmed) {
			flushCommandCandidates()
			commandSection, optionSection, synopsisSection, usageContinuation = false, true, false, false
			node.OptionsKnown, sawStructure = true, true
			continue
		}
		if isSynopsisHeader(trimmed) {
			flushCommandCandidates()
			commandSection, optionSection, synopsisSection, usageContinuation = false, false, true, false
			sawStructure, sawUsage = true, true
			continue
		}
		if isSectionHeader(trimmed) {
			flushCommandCandidates()
			commandSection, optionSection, synopsisSection, usageContinuation = false, false, false, false
		}

		usageLine := strings.HasPrefix(lower, "usage:") || strings.HasPrefix(lower, "usage ")
		continuedUsage := usageContinuation && len(line) > 0 && unicode.IsSpace(rune(line[0]))
		if usageLine {
			usageContinuation = true
		} else if usageContinuation && !continuedUsage {
			usageContinuation = false
		}
		if usageLine || continuedUsage || synopsisSection {
			sawStructure, sawUsage, node.OptionsKnown = true, true, true
			parseUsageOptions(line, node.Options)
			if containsCommandMarker(line) || hasPositionalBraceChoice(line) {
				sawCommandMarker = true
			}
		}

		if (optionSection || sawUsage) && indentedOptionLine(line) {
			definition := optionDefinition(line)
			mergeOptions(node.Options, parseOptionGroup(definition))
		}
		if commandSection {
			names := append(commandRow(line), isolatedBraceNames(line)...)
			if len(names) > 0 {
				commandCandidates = append(commandCandidates, commandCandidate{indent: commandIndent(line), names: names})
			}
		}
	}
	flushCommandCandidates()

	if len(node.Subcommands) > 0 {
		node.SubcommandState = SubcommandsListed
	} else if sawCommandMarker {
		node.SubcommandState = SubcommandsUnknown
	} else if sawUsage {
		node.SubcommandState = SubcommandsNone
	} else {
		node.SubcommandState = SubcommandsUnknown
	}
	if !sawStructure || len(node.Options) == 0 && len(node.Subcommands) == 0 && !sawUsage {
		return NodeSpec{}, errors.New("no supported help structure found")
	}
	return node, nil
}

func commandIndent(line string) int {
	indent := 0
	for _, r := range line {
		if !unicode.IsSpace(r) {
			break
		}
		indent++
	}
	return indent
}

func indentedOptionLine(line string) bool {
	if len(line) == 0 || !unicode.IsSpace(rune(line[0])) {
		return false
	}
	return strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), "-")
}

func normalizeHelp(value string) string {
	value = ansiOSC.ReplaceAllString(value, "")
	value = ansiCSI.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	runes := make([]rune, 0, len(value))
	for _, r := range value {
		switch r {
		case '\b':
			if len(runes) > 0 {
				runes = runes[:len(runes)-1]
			}
		case '\t':
			runes = append(runes, ' ', ' ', ' ', ' ')
		default:
			if r == '\n' || r >= 0x20 {
				runes = append(runes, r)
			}
		}
	}
	return string(runes)
}

func isCommandHeader(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(trimmed, ":")))
	if !strings.Contains(lower, "command") || len(lower) > 100 || strings.ContainsAny(lower, "[]<>") {
		return false
	}
	switch lower {
	case "commands", "available commands", "subcommands", "all commands", "supported commands", "built-in commands":
		return true
	}
	if !strings.HasSuffix(trimmed, ":") {
		return false
	}
	if strings.HasPrefix(lower, "these are common ") && containsWord(lower, "commands") || lower == "the commands are" {
		return true
	}
	heading := lower
	if open := strings.IndexByte(heading, '('); open >= 0 {
		if !strings.HasSuffix(heading, ")") {
			return false
		}
		heading = strings.TrimSpace(heading[:open])
	}
	fields := strings.Fields(heading)
	return len(fields) == 2 && fields[1] == "commands"
}

func completeCommandHeader(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ":")))
	switch lower {
	case "commands", "available commands", "subcommands", "all commands", "the commands are", "supported commands":
		return true
	default:
		return false
	}
}

func isOptionHeader(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ":")))
	switch lower {
	case "options", "option", "flags", "global options", "global flags", "optional arguments", "arguments":
		return true
	default:
		return false
	}
}

func isSynopsisHeader(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ":")))
	return lower == "synopsis" || lower == "usage"
}

func isSectionHeader(value string) bool {
	raw := strings.TrimSpace(value)
	colonTerminated := strings.HasSuffix(raw, ":")
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, ":"))
	if len(trimmed) == 0 || len(trimmed) > 60 || strings.ContainsAny(trimmed, "[]<>") {
		return false
	}
	letters, upper := 0, 0
	for _, r := range trimmed {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	return letters > 0 && (letters == upper || colonTerminated && sectionTitle(trimmed))
}

func sectionTitle(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || strings.ContainsRune("&()/_.-", r) {
			continue
		}
		return false
	}
	return true
}

func containsWord(value, target string) bool {
	for _, field := range strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if field == target {
			return true
		}
	}
	return false
}

func containsCommandMarker(value string) bool {
	if commandMetaRE.MatchString(value) {
		return true
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"<command>", "[command]", "<commands>", "[commands]", " subcommand", " command ["} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func commandRow(line string) []string {
	if len(line) == 0 || !unicode.IsSpace(rune(line[0])) {
		return nil
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if !strings.ContainsAny(trimmed, " \t") {
		return validCommandList(strings.TrimSuffix(trimmed, ","))
	}
	if strings.Contains(trimmed, ",") && !strings.Contains(trimmed, "  ") && !strings.Contains(trimmed, "\t") {
		return validCommandList(strings.TrimSuffix(trimmed, ","))
	}
	match := commandRowRE.FindStringSubmatch(line)
	if len(match) != 2 {
		return nil
	}
	return validCommandList(match[1])
}

func validCommandList(value string) []string {
	var out []string
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if validCommandName(candidate) {
			out = append(out, candidate)
		} else {
			return nil
		}
	}
	return out
}

func validCommandName(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._+-", r) {
			return false
		}
	}
	return true
}

// Braced choices are ambiguous in usage text: argparse uses the same spelling
// for subcommands and ordinary positional/option enums. They are therefore a
// marker of unknown positional structure, never proof that a word is safe to
// pass back to the executable. Names are accepted only inside an explicit
// command section.
func hasPositionalBraceChoice(line string) bool {
	for _, match := range braceChoiceRE.FindAllStringSubmatchIndex(line, -1) {
		if braceIsOptionValue(line[:match[0]]) {
			continue
		}
		return true
	}
	return false
}

func braceIsOptionValue(prefix string) bool {
	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return false
	}
	previous := fields[len(fields)-1]
	if strings.HasSuffix(previous, "]") || strings.HasSuffix(previous, ")") || strings.HasSuffix(previous, "}") {
		return false
	}
	previous = strings.TrimLeft(previous, "[({")
	return strings.HasPrefix(previous, "-")
}

// isolatedBraceNames accepts a brace list only when it is the entire indented
// command row. A brace enum in a command description is an operand domain, not
// evidence that any enum member is safe to pass to a nested help probe.
func isolatedBraceNames(line string) []string {
	if len(line) == 0 || !unicode.IsSpace(rune(line[0])) {
		return nil
	}
	trimmed := strings.TrimSpace(line)
	match := braceChoiceRE.FindStringSubmatch(trimmed)
	if len(match) != 2 || match[0] != trimmed {
		return nil
	}
	return validCommandList(match[1])
}

func parseUsageOptions(line string, destination map[string]OptionSpec) {
	groups := bracketGroups(line)
	if len(groups) == 0 {
		mergeOptions(destination, parseOptionGroup(line))
		return
	}
	for _, group := range groups {
		if strings.Contains(group, "-") {
			mergeOptions(destination, parseOptionGroup(group))
		}
	}
}

func bracketGroups(value string) []string {
	var out []string
	depth, start := 0, -1
	for index, r := range value {
		switch r {
		case '[':
			if depth == 0 {
				start = index
			}
			depth++
		case ']':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, value[start:index+1])
				start = -1
			}
		}
	}
	return out
}

func optionDefinition(line string) string {
	trimmed := strings.TrimSpace(line)
	for index := 0; index+1 < len(trimmed); index++ {
		if trimmed[index] != '\t' && (trimmed[index] != ' ' || trimmed[index+1] != ' ') {
			continue
		}
		end := index
		for end < len(trimmed) && (trimmed[end] == ' ' || trimmed[end] == '\t') {
			end++
		}
		remainder := strings.TrimSpace(trimmed[end:])
		if remainder == "" || strings.HasPrefix(remainder, "-") || beginsMetavar(remainder) {
			index = end - 1
			continue
		}
		return strings.TrimSpace(trimmed[:index])
	}
	return trimmed
}

func parseOptionGroup(definition string) map[string]OptionSpec {
	out := make(map[string]OptionSpec)
	definition = negatedOptRE.ReplaceAllString(definition, "--$1, --no-$1")
	matches := optionMatches(definition)
	if len(matches) == 0 {
		return out
	}
	groupMode := strings.Contains(definition, ",") || strings.Contains(definition, "|")
	groupValue := NoValue
	groupSeparate, groupAttached := false, false
	for index, match := range matches {
		name := definition[match[0]:match[1]]
		end := len(definition)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		fragment := definition[match[1]:end]
		spec := optionFromFragment(name, fragment)
		out[name] = mergeOption(out[name], spec)
		if spec.Value > groupValue {
			groupValue = spec.Value
		}
		groupSeparate = groupSeparate || spec.AllowSeparate
		groupAttached = groupAttached || spec.AllowAttached
	}
	if groupMode && groupValue != NoValue {
		for name, spec := range out {
			if spec.Value == NoValue {
				spec.Value = groupValue
				spec.AllowSeparate = groupSeparate
				if strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "--") {
					spec.AllowAttached = groupAttached
				}
				out[name] = spec
			}
		}
	}
	if _, hasHelp := out["--help"]; hasHelp {
		if short, ok := out["-h"]; ok {
			short.Terminal = true
			out["-h"] = short
		}
	}
	return out
}

func optionMatches(definition string) [][]int {
	raw := optionNameRE.FindAllStringIndex(definition, -1)
	out := make([][]int, 0, len(raw))
	for _, match := range raw {
		if match[0] > 0 {
			before := rune(definition[match[0]-1])
			if !unicode.IsSpace(before) && !strings.ContainsRune("[({,|/", before) {
				continue
			}
		}
		out = append(out, match)
	}
	return out
}

func optionFromFragment(name, fragment string) OptionSpec {
	spec := OptionSpec{Terminal: name == "--help" || name == "--version"}
	fragment = strings.TrimRight(fragment, " \t,|)")
	if fragment == "" {
		return spec
	}
	withoutSeparators := strings.TrimLeft(fragment, ",|")
	separate := len(withoutSeparators) > 0 && (withoutSeparators[0] == ' ' || withoutSeparators[0] == '\t')
	trimmed := strings.TrimLeft(withoutSeparators, " \t")
	optional := strings.HasPrefix(trimmed, "[")
	attached := strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "[=")
	if attached {
		spec.AllowAttached = true
		if optional {
			spec.Value = OptionalValue
		} else {
			spec.Value = RequiredValue
		}
		return spec
	}
	if separate && beginsMetavar(trimmed) {
		spec.AllowSeparate = true
		if optional {
			spec.Value = OptionalValue
		} else {
			spec.Value = RequiredValue
		}
		return spec
	}
	if !separate && strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "--") && beginsMetavar(trimmed) {
		spec.AllowAttached = true
		if optional {
			spec.Value = OptionalValue
		} else {
			spec.Value = RequiredValue
		}
	}
	return spec
}

func beginsMetavar(value string) bool {
	value = strings.TrimLeft(value, "[,=(")
	if value == "" {
		return false
	}
	if value[0] == '<' {
		return true
	}
	field := strings.Trim(strings.Fields(value)[0], "[]<>{},=()")
	if field == "" {
		return false
	}
	if field == strings.ToUpper(field) && strings.IndexFunc(field, unicode.IsLetter) >= 0 {
		return true
	}
	_, ok := metavarWords[strings.ToLower(field)]
	return ok
}

func mergeOptions(destination, source map[string]OptionSpec) {
	for name, spec := range source {
		destination[name] = mergeOption(destination[name], spec)
	}
}

func mergeOption(left, right OptionSpec) OptionSpec {
	if right.Value > left.Value {
		left.Value = right.Value
	}
	left.AllowSeparate = left.AllowSeparate || right.AllowSeparate
	left.AllowAttached = left.AllowAttached || right.AllowAttached
	left.Terminal = left.Terminal || right.Terminal
	return left
}
