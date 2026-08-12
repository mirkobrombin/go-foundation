// Package version reports which build of the tools this binary is.
//
// It reads the answer rather than storing it. A constant bumped by hand is
// right until the release someone forgets, and a stale constant is worse than
// no version at all: it reports a build that does not exist.
//
// Two sources cover the two ways this binary comes into being. Installed with
// go install module@version, the module version is already recorded in the
// build info. Cross compiled from a checkout, as the release pipeline does,
// there is no module version to read, so the pipeline stamps the tag it is
// building. Anything else is a working copy, and says so.
package version

import (
	"runtime/debug"
	"strings"
)

// stamped is set at link time by the release build:
//
//	go build -ldflags "-X github.com/mirkobrombin/go-foundation/dev/v2/version.stamped=v2.2.0"
var stamped string

// devel is what an unreleased build reports.
const devel = "(devel)"

// Current returns the version of the development tools module.
func Current() string {
	if version := strings.TrimSpace(stamped); version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return devel
	}
	if version := strings.TrimSpace(info.Main.Version); version != "" && version != devel {
		return version
	}
	if revision := shortRevision(info); revision != "" {
		return devel + " " + revision
	}
	return devel
}

// Released reports whether this build carries a real version. The release
// pipeline uses it to refuse to publish a binary that cannot name itself.
func Released() bool {
	return !strings.HasPrefix(Current(), devel)
}

func shortRevision(info *debug.BuildInfo) string {
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if modified == "true" {
		return revision + "-dirty"
	}
	return revision
}
