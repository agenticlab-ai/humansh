package usererr

import (
	"errors"
	"strings"
	"testing"
)

func TestDebugRenderingRedactsCredentialsAndControls(t *testing.T) {
	cause := errors.New("OPENROUTER_API_KEY=sk-or-v1-supersecret Authorization: Bearer eyJheader.payload.signature\x1b[31m")
	rendered := Render(New("test", "Failed.", "Nothing changed.", false, cause), true)
	for _, secret := range []string{"sk-or-v1-supersecret", "eyJheader.payload.signature", "\x1b"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("debug output leaked %q: %q", secret, rendered)
		}
	}
	if !strings.Contains(rendered, "Debug:") || !strings.Contains(rendered, "[REDACTED]") {
		t.Fatalf("debug details were not retained in redacted form: %q", rendered)
	}
}
