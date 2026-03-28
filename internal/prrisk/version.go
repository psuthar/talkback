package prrisk

import "fmt"

// Version and VersionMinor identify the PR risk report schema (emitted in JSON, markdown, PR comments).
const (
	Version      = 2
	VersionMinor = 5
)

// ReportVersionString returns the canonical label, e.g. "v2.5".
func ReportVersionString() string {
	return fmt.Sprintf("v%d.%d", Version, VersionMinor)
}
