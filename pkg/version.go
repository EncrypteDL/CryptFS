package pkg

import (
	"fmt"
	"runtime/debug"
	"strings"
)

var (
	// Version release version
	Version = "0.0.0"

	// Commit will be overwritten automatically by the build system
	Commit = "HEAD"
)

// FullVersion display the full version and build
func FullVersion() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s@%s", Version, Commit))

	if info, ok := debug.ReadBuildInfo(); ok {
		sb.WriteString(fmt.Sprintf(" %s built with %s", info.Main.Version, info.GoVersion))
		if info.Main.Sum != "" {
			sb.WriteString(fmt.Sprintf(" (checksum: %s)", info.Main.Sum))
		}
	}

	return sb.String()
}
