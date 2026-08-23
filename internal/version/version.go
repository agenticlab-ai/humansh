package version

import "runtime"

var (
	Version   = "0.1.0-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string   `json:"version"`
	Commit    string   `json:"commit"`
	BuildDate string   `json:"build_date"`
	GoVersion string   `json:"go_version"`
	Protocol  string   `json:"protocol"`
	Protocols []string `json:"protocols"`
}

func Current(primaryProtocol string, additionalProtocols ...string) Info {
	protocols := append([]string{primaryProtocol}, additionalProtocols...)
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate, GoVersion: runtime.Version(), Protocol: primaryProtocol, Protocols: protocols}
}
