package providerutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	usererr "github.com/agenticlab-ai/humansh/internal/errors"
	"github.com/agenticlab-ai/humansh/internal/exitcode"
	"github.com/agenticlab-ai/humansh/internal/llm"
)

func DecodeResponse(data []byte) (llm.TranslationResponse, error) {
	if err := RejectDuplicateJSONKeys(data); err != nil {
		return llm.TranslationResponse{}, Malformed("invalid structured JSON", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return llm.TranslationResponse{}, Malformed("invalid structured JSON", err)
	}
	for _, name := range []string{"status", "command", "explanation", "clarification", "assumptions"} {
		value, ok := fields[name]
		if !ok {
			return llm.TranslationResponse{}, Malformed("structured JSON omitted required field "+name, nil)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return llm.TranslationResponse{}, Malformed("structured JSON field "+name+" was null", nil)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var response llm.TranslationResponse
	if err := decoder.Decode(&response); err != nil {
		return response, Malformed("invalid structured JSON", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return response, Malformed("multiple JSON values", err)
	}
	return response, nil
}

// RejectDuplicateJSONKeys applies to provider-specific outer envelopes as well
// as the common response object. Encoding/json otherwise accepts duplicate
// members and silently keeps one, which makes an untrusted response ambiguous.
func RejectDuplicateJSONKeys(data []byte) error { return rejectDuplicateJSONKeys(data) }

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var consume func() error
	consume = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = true
				if err := consume(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := consume(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
	}
	return consume()
}

func Missing(provider, install, login string) error {
	fixes := make([]usererr.Fix, 0, 3)
	if install != "" {
		fixes = append(fixes, usererr.Fix{Description: "Install", Command: install})
	}
	if login != "" {
		fixes = append(fixes, usererr.Fix{Description: "Then sign in with", Command: login})
	}
	fixes = append(fixes, usererr.Fix{Description: "Diagnose with", Command: "humansh doctor"})
	return usererr.WithExit(exitcode.ProviderUnavailable, "provider_missing", provider+" is not installed.", "Nothing was changed or executed.", false, nil, fixes...)
}

func Auth(provider, repair, check string, cause error) error {
	return usererr.WithExit(exitcode.ProviderAuth, "provider_auth", provider+" is not logged in with the required subscription authentication.", "Nothing was changed or executed.", false, cause,
		usererr.Fix{Description: "Fix", Command: repair}, usererr.Fix{Description: "Check", Command: check})
}

func Temporary(provider string, cause error) error {
	return usererr.WithExit(exitcode.ProviderTemporary, "provider_temporary", provider+" could not complete the translation.", "Nothing was changed or executed.", true, cause,
		usererr.Fix{Description: "Retry, or diagnose with", Command: "humansh provider test"})
}

// TemporaryOrTimeout is the common failure boundary for provider operations
// governed by the configured timeout. It prevents every adapter from inventing
// different (or generic) timeout wording and always gives the user a copyable
// way to increase the limit.
func TemporaryOrTimeout(provider llm.ProviderID, timeout time.Duration, cause error) error {
	if errors.Is(cause, context.DeadlineExceeded) {
		return Timeout(provider, timeout, cause)
	}
	return Temporary(provider.Label(), cause)
}

// Timeout reports the configured limit and suggests the next valid larger
// value. Runtime configuration accepts whole seconds from 3 through 60.
func Timeout(provider llm.ProviderID, timeout time.Duration, cause error) error {
	seconds := int((timeout + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	fixes := make([]usererr.Fix, 0, 2)
	if seconds < 60 {
		suggested := seconds * 2
		if suggested < 45 {
			suggested = 45
		}
		if suggested > 60 {
			suggested = 60
		}
		fixes = append(fixes, usererr.Fix{
			Description: fmt.Sprintf("Increase the timeout to %d seconds with", suggested),
			Command:     fmt.Sprintf("humansh config set timeout_seconds %d", suggested),
		})
	} else {
		fixes = append(fixes, usererr.Fix{Description: "The timeout is already at the 60-second maximum"})
	}
	fixes = append(fixes, usererr.Fix{
		Description: "Then retry the command, or test this provider with",
		Command:     "humansh provider test " + string(provider),
	})
	return usererr.WithExit(
		exitcode.ProviderTemporary,
		"provider_timeout",
		provider.Label()+" timed out before completing the translation.",
		fmt.Sprintf("The configured provider timeout is %d seconds. Nothing was changed or executed.", seconds),
		true,
		cause,
		fixes...,
	)
}

func Quota(provider string, cause error) error {
	fixes := []usererr.Fix{{Description: "Inspect provider status with", Command: "humansh provider list"}}
	switch provider {
	case "Codex":
		fixes = append(fixes, usererr.Fix{Description: "Or explicitly switch subscription provider with", Command: "humansh provider use claude"}, usererr.Fix{Description: "Or", Command: "humansh provider use cursor"}, usererr.Fix{Description: "Or explicitly configure metered OpenRouter with", Command: "humansh provider configure openrouter --model provider/model"})
	case "Claude Code":
		fixes = append(fixes, usererr.Fix{Description: "Or explicitly switch subscription provider with", Command: "humansh provider use codex"}, usererr.Fix{Description: "Or", Command: "humansh provider use cursor"}, usererr.Fix{Description: "Or explicitly configure metered OpenRouter with", Command: "humansh provider configure openrouter --model provider/model"})
	case "Cursor CLI":
		fixes = append(fixes, usererr.Fix{Description: "Or explicitly switch subscription provider with", Command: "humansh provider use codex"}, usererr.Fix{Description: "Or", Command: "humansh provider use claude"}, usererr.Fix{Description: "Or explicitly configure metered OpenRouter with", Command: "humansh provider configure openrouter --model provider/model"})
	case "OpenRouter":
		fixes = append(fixes, usererr.Fix{Description: "Or explicitly switch subscription provider with", Command: "humansh provider use codex"}, usererr.Fix{Description: "Or", Command: "humansh provider use claude"}, usererr.Fix{Description: "Or", Command: "humansh provider use cursor"})
	}
	return usererr.WithExit(exitcode.ProviderQuota, "provider_quota", provider+" quota, credits, or rate limit is unavailable.", "Nothing was changed or executed; no paid fallback was attempted.", true, cause,
		fixes...)
}

func Malformed(detail string, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("%s", detail)
	}
	return usererr.WithExit(exitcode.ProviderMalformed, "provider_malformed", "Provider did not finish with a usable command.", "Nothing was changed or executed.", true, fmt.Errorf("%s: %w", detail, cause),
		usererr.Fix{Description: "Retry, or diagnose with", Command: "humansh provider test"})
}

func MapCLIError(provider llm.ProviderID, timeout time.Duration, stderr []byte, cause error) error {
	if errors.Is(cause, context.DeadlineExceeded) {
		return Timeout(provider, timeout, cause)
	}
	label := provider.Label()
	text := strings.ToLower(string(stderr))
	switch {
	case strings.Contains(text, "rate limit"), strings.Contains(text, "usage limit"), strings.Contains(text, "quota"), strings.Contains(text, "billing"):
		return Quota(label, cause)
	case strings.Contains(text, "login"), strings.Contains(text, "auth"), strings.Contains(text, "unauthorized"):
		return Auth(label, "humansh provider test", "humansh doctor", cause)
	case strings.Contains(text, "model not found"), strings.Contains(text, "unknown model"), strings.Contains(text, "invalid model"):
		return usererr.WithExit(exitcode.ProviderUnavailable, "provider_model", label+" rejected the configured model.", "Nothing was changed or executed.", false, cause, usererr.Fix{Description: "Choose a supported model, then run", Command: "humansh provider test"})
	case strings.Contains(text, "organization disallowed"), strings.Contains(text, "workspace denied"), strings.Contains(text, "permission denied"), strings.Contains(text, "forbidden"):
		return usererr.WithExit(exitcode.ProviderUnavailable, "provider_access_denied", label+" account, organization, or workspace denied access.", "Nothing was changed or executed.", false, cause, usererr.Fix{Description: "Check account access, then run", Command: "humansh provider test"})
	case strings.Contains(text, "unknown option"), strings.Contains(text, "unexpected argument"), strings.Contains(text, "unknown field"), strings.Contains(text, "unknown key"):
		return usererr.WithExit(exitcode.ProviderUnavailable, "provider_too_old", label+" is too old for safe structured translation.", "Nothing was changed or executed.", false, cause, usererr.Fix{Description: "Update the provider CLI, then run", Command: "humansh doctor"})
	default:
		return Temporary(label, cause)
	}
}

// versionPattern captures the first dotted numeric version in a CLI's version
// output, for example "codex-cli-exec 0.149.0" or "2.1.238 (Claude Code)".
var versionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)(?:\.(\d+))?\b`)

// VersionFloor reports whether reported is a recognisable version that is at
// least minimum. Both results matter to callers:
//
//   - meets is true when the version parsed and is greater than or equal to the
//     floor, so "update the CLI" is never suggested to somebody already newer.
//   - parsed is false when no version could be read at all. Callers then fall
//     back to capability probes alone, because build_with_ai.md Section 10.1
//     requires version gating only where no direct probe exists, and Section 28
//     requires reporting a version mismatch through doctor rather than silently
//     refusing to run.
//
// minimum is given as {major, minor, patch}.
func VersionFloor(reported string, minimum [3]int) (meets bool, parsed bool) {
	match := versionPattern.FindStringSubmatch(reported)
	if match == nil {
		return false, false
	}
	var found [3]int
	for index := range found {
		if value := match[index+1]; value != "" {
			number, err := strconv.Atoi(value)
			if err != nil {
				return false, false
			}
			found[index] = number
		}
	}
	for index := range found {
		if found[index] != minimum[index] {
			return found[index] > minimum[index], true
		}
	}
	return true, true
}
