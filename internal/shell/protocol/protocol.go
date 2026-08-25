package protocol

import "github.com/agenticlab-ai/humansh/internal/exitcode"

const (
	Version         = "zle-v1"
	ReadlineVersion = "readline-v1"
)

const (
	ExitLiteral             = exitcode.Literal
	ExitGeneratedLow        = exitcode.GeneratedLow
	ExitAmbiguous           = exitcode.Ambiguous
	ExitClarify             = exitcode.Clarify
	ExitGeneratedMedium     = exitcode.GeneratedMedium
	ExitGeneratedHigh       = exitcode.GeneratedHigh
	ExitUnsupported         = exitcode.Unsupported
	ExitConfig              = exitcode.Config
	ExitProviderUnavailable = exitcode.ProviderUnavailable
	ExitProviderAuth        = exitcode.ProviderAuth
	ExitProviderQuota       = exitcode.ProviderQuota
	ExitProviderTemporary   = exitcode.ProviderTemporary
	ExitProviderMalformed   = exitcode.ProviderMalformed
	ExitPolicyRejected      = exitcode.PolicyRejected
	ExitInternal            = exitcode.Internal
)

func Known(code int) bool { return exitcode.Known(code) }
