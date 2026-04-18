package assets

import (
	"embed"
	"strings"
)

//go:embed VERSION
var rawVersion string

//go:embed banner.txt
var rawBanner string

//go:embed simulation-data/* simulation-data/**/*
var SimulationData embed.FS

// Version returns the parsed version string
func Version() string {
	return strings.TrimSpace(rawVersion)
}

// Banner returns the banner string with ANSI escapes evaluated
func Banner() string {
	b := strings.ReplaceAll(rawBanner, "\\033", "\x1b")
	// Add Daemon version suffix
	b += " \x1b[38;2;200;200;200mFS INGEST DAEMON | v" + Version() + "\x1b[0m\n"
	return b
}
