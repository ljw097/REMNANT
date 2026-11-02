// Package buildinfo carries the version stamped into release builds with
//
//	-ldflags "-X github.com/ljw097/remnant/internal/buildinfo.Version=v1.2.3
//	          -X github.com/ljw097/remnant/internal/buildinfo.Commit=<sha>"
//
// Builds without it report "dev" and never update themselves.
package buildinfo

var (
	Version = "dev"
	Commit  = ""
)

// IsRelease reports whether this binary was built by the release pipeline.
func IsRelease() bool { return Version != "dev" }
