package exitcode

const (
	Literal             = 0
	GeneratedLow        = 10
	Ambiguous           = 11
	Clarify             = 12
	GeneratedMedium     = 13
	GeneratedHigh       = 14
	Unsupported         = 15
	Config              = 20
	ProviderUnavailable = 21
	ProviderAuth        = 22
	ProviderQuota       = 23
	ProviderTemporary   = 24
	ProviderMalformed   = 25
	PolicyRejected      = 26
	Internal            = 70
)

func Known(code int) bool {
	switch code {
	case Literal, GeneratedLow, Ambiguous, Clarify, GeneratedMedium, GeneratedHigh, Unsupported,
		Config, ProviderUnavailable, ProviderAuth, ProviderQuota, ProviderTemporary, ProviderMalformed,
		PolicyRejected, Internal:
		return true
	default:
		return false
	}
}
