// Package app holds build-time application metadata injected via ldflags.
package app

// Build metadata, overridden at build time via -ldflags "-X ...".
var (
	// Name is the application name.
	Name = "Alchemist"
	// Version is the semantic version or git describe output.
	Version = "dev"
	// BuildDate is the UTC build timestamp.
	BuildDate = "unknown"
)
