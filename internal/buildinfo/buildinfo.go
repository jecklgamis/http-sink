// Package buildinfo exposes build metadata (git branch, commit, build time).
package buildinfo

// Info is the JSON shape served at /build-info.
type Info struct {
	Branch    string `json:"branch"`
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
}
