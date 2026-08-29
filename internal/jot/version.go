package jot

import "runtime/debug"

// version is overridden at build time with -ldflags "-X github.com/paws-in-the-machine/jot/internal/jot.version=v1.2.3".
var version = ""

// Version reports the build version, falling back to VCS data stamped in by
// the Go toolchain and finally to "dev".
func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		return "dev+" + revision + "-dirty"
	}
	return "dev+" + revision
}
