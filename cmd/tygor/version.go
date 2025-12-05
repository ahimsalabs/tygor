package main

import (
	_ "embed"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var embeddedVersion string

// Version returns the version string.
//
// When installed via `go install ...@version`, returns the module version (e.g., "v0.7.4").
// For development builds, returns "devel-0.7.4+abc1234" with VCS revision if available.
func Version() string {
	base := strings.TrimSpace(embeddedVersion)

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return base
	}

	// If installed via go install, use the module version
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// For development builds, show devel-{version}+{revision}
	var vcsRev string
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			vcsRev = s.Value[:7]
			break
		}
	}

	if vcsRev != "" {
		return "devel-" + base + "+" + vcsRev
	}

	return "devel-" + base
}
