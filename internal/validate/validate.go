package validate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/llm"
	"github.com/agenticlab-ai/humansh/internal/shell/protocol"
)

const MaxCommandBytes = 4096

type Validator struct{}

func (Validator) Response(response llm.TranslationResponse) error { return Response(response) }
func (Validator) Command(command string) error                    { return Command(command) }

var (
	presentationProse = regexp.MustCompile(`(?i)^(?:here(?:'s| is)(?: the)? command|the command is|you can run|run this|try this|use this command|command)\s*:`)
	multipleChoices   = regexp.MustCompile(`(?i)(?:;\s*(?:or|alternatively|otherwise)\b|\b(?:option|alternative)\s+[12]:)`)
	obfuscatedExec    = regexp.MustCompile(`(?i)(?:^|[;&|()]\s*|\s)(?:builtin\s+|command\s+)?eval(?:\s|$)|\b(?:python(?:3)?|perl|ruby|node)\s+(?:-[A-Za-z]*[ce]\b|--eval\b)[^;|]*(?:base64|fromhex|charcode|unescape)|\bprintf\b[^|;]*\\x[0-9a-f]{2}[^|;]*\|\s*(?:sh|bash|zsh)\b`)
)

func Response(response llm.TranslationResponse) error {
	if utf8.RuneCountInString(response.Explanation) > 500 || utf8.RuneCountInString(response.Clarification) > 500 || len(response.Assumptions) > 5 {
		return malformed("provider response exceeded its schema bounds", nil)
	}
	for _, field := range []struct{ name, value string }{{"explanation", response.Explanation}, {"clarification", response.Clarification}} {
		if err := responseText(field.name, field.value); err != nil {
			return err
		}
	}
	if response.Assumptions == nil {
		return malformed("provider response omitted the assumptions array", nil)
	}
	for _, assumption := range response.Assumptions {
		if utf8.RuneCountInString(assumption) > 200 {
			return malformed("provider assumption exceeded its schema bound", nil)
		}
		if err := responseText("assumption", assumption); err != nil {
			return err
		}
	}
	switch response.Status {
	case "ok":
		if response.Command == "" {
			return usererr.WithExit(protocol.ExitProviderMalformed, "provider_incomplete", "Provider ended before producing a command.", "Nothing was changed or executed.", true, nil, usererr.Fix{Description: "Fix: retry; if this continues, run", Command: "humansh provider test"})
		}
		if response.Clarification != "" {
			return malformed("ok response included a clarification", nil)
		}
	case "clarify":
		clarification := strings.TrimSpace(response.Clarification)
		if response.Command != "" || clarification == "" || !strings.HasSuffix(clarification, "?") {
			return malformed("clarification response has invalid fields", nil)
		}
	case "unsupported":
		if response.Command != "" || response.Clarification != "" || strings.TrimSpace(response.Explanation) == "" {
			return malformed("unsupported response has invalid fields", nil)
		}
	default:
		return malformed("provider returned an unknown status", nil)
	}
	return nil
}

func responseText(name, value string) error {
	if !utf8.ValidString(value) {
		return malformed("provider "+name+" contained invalid UTF-8", nil)
	}
	for _, r := range value {
		if forbiddenUnicode(r) {
			return rejected(fmt.Sprintf("provider %s contained forbidden control U+%04X", name, r), nil)
		}
	}
	return nil
}

func Command(command string) error {
	if strings.TrimSpace(command) == "" {
		return malformed("provider returned an empty command", nil)
	}
	if !utf8.ValidString(command) {
		return malformed("provider returned invalid UTF-8", nil)
	}
	if len(command) > MaxCommandBytes {
		return malformed("provider returned a command longer than 4096 bytes", nil)
	}
	if strings.ContainsAny(command, "\x00\r\n\x1b") {
		return rejected("provider returned terminal control or multiline content", nil)
	}
	for _, r := range command {
		if forbiddenUnicode(r) {
			return rejected(fmt.Sprintf("provider returned forbidden Unicode control U+%04X", r), nil)
		}
	}
	trimmed := strings.TrimSpace(command)
	if strings.Contains(trimmed, "```") {
		return rejected("provider returned Markdown fences", nil)
	}
	if strings.HasPrefix(trimmed, "$ ") || strings.HasPrefix(trimmed, "% ") || strings.HasPrefix(trimmed, "> ") {
		return rejected("provider returned a presentation prompt", nil)
	}
	if presentationProse.MatchString(trimmed) {
		return rejected("provider returned prose around the command", nil)
	}
	if multipleChoices.MatchString(trimmed) {
		return rejected("provider returned multiple command alternatives", nil)
	}
	if obfuscatedExec.MatchString(trimmed) {
		return rejected("provider returned obfuscated execution", nil)
	}
	return nil
}

func malformed(detail string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("%s", detail)
	}
	return usererr.WithExit(protocol.ExitProviderMalformed, "provider_malformed", "Provider did not finish with a usable command.", "Nothing was changed or executed.", true, fmt.Errorf("%s: %w", detail, cause), usererr.Fix{Description: "Fix: retry; if this continues, run", Command: "humansh provider test"})
}

func rejected(detail string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("%s", detail)
	}
	return usererr.WithExit(protocol.ExitPolicyRejected, "policy_rejected", "The provider returned a command that was not safe to place in your terminal.", "Nothing was changed or executed.", false, fmt.Errorf("%s: %w", detail, cause), usererr.Fix{Description: "Fix: retry, or diagnose with", Command: "humansh provider test"})
}

func forbiddenUnicode(r rune) bool {
	if unicode.IsControl(r) {
		return true
	}
	switch r {
	case '\u061c', '\u200b', '\u200c', '\u200d', '\u200e', '\u200f', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e', '\u2060', '\u2066', '\u2067', '\u2068', '\u2069', '\ufeff':
		return true
	default:
		return false
	}
}
