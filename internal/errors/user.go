package usererr

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var debugSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:authorization\s*:\s*bearer|bearer)\s+[^\s,;]+`),
	regexp.MustCompile(`(?i)\b(?:OPENROUTER_API_KEY|ANTHROPIC_API_KEY|OPENAI_API_KEY|CODEX_API_KEY)\s*=\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`),
}

// Fix is an actionable, copyable repair step.
type Fix struct {
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
}

// Error is the redacted error envelope rendered by the CLI.
type Error struct {
	Code       string `json:"code"`
	ExitCode   int    `json:"exit_code"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Fixes      []Fix  `json:"fixes,omitempty"`
	Retryable  bool   `json:"retryable"`
	DebugCause error  `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Summary != "" {
		return e.Summary
	}
	return e.Title
}

func (e *Error) Unwrap() error { return e.DebugCause }

func New(code, title, summary string, retryable bool, cause error, fixes ...Fix) *Error {
	return &Error{Code: code, Title: title, Summary: summary, Retryable: retryable, DebugCause: cause, Fixes: fixes}
}

func WithExit(exitCode int, code, title, summary string, retryable bool, cause error, fixes ...Fix) *Error {
	return &Error{ExitCode: exitCode, Code: code, Title: title, Summary: summary, Retryable: retryable, DebugCause: cause, Fixes: fixes}
}

func As(err error) (*Error, bool) {
	var target *Error
	return target, errors.As(err, &target)
}

func Render(err *Error, debug bool) string {
	if err == nil {
		return ""
	}
	message := "humansh: " + err.Title
	if err.Summary != "" && err.Summary != err.Title {
		message += "\n" + err.Summary
	}
	for _, fix := range err.Fixes {
		message += "\n" + fix.Description
		if fix.Command != "" {
			message += ": `" + fix.Command + "`"
		}
	}
	if debug && err.DebugCause != nil {
		message += "\nDebug: " + RedactDebug(fmt.Sprint(err.DebugCause))
	}
	return message
}

// RedactDebug removes credential-shaped values and terminal controls from
// opt-in diagnostics. Callers must still avoid passing raw provider responses
// or user input as causes; this is the final defense before terminal output.
func RedactDebug(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, value)
	for _, pattern := range debugSecretPatterns {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}
