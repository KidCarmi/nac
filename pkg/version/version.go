// Package version exposes build metadata for the binary.
//
// Values are injected at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/kidcarmi/nac/pkg/version.Version=1.2.3"
//
// When built without ldflags (e.g. `go run`), the zero values below apply,
// which is acceptable for development.
package version

import "fmt"

var (
	// Version is the semantic version of the build (e.g. "1.2.3").
	Version = "dev"
	// Commit is the git commit SHA the binary was built from.
	Commit = "unknown"
	// BuildDate is the RFC3339 timestamp of the build.
	BuildDate = "unknown"
)

// Info is a structured representation of build metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

// Get returns the current build metadata.
func Get() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate}
}

// String renders a human-readable one-line version string.
func (i Info) String() string {
	return fmt.Sprintf("nac %s (commit %s, built %s)", i.Version, i.Commit, i.BuildDate)
}
