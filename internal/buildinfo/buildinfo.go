// Package buildinfo carries the version stamped in at link time.
package buildinfo

import "runtime"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

func Get() Info {
	return Info{Name: "mmcp", Version: Version, Commit: Commit, BuildTime: BuildTime, GoVersion: runtime.Version()}
}
