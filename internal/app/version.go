package app

// Version is the release version. The linker injects it through -X
// github.com/baalimago/duls/internal/app.Version.
var Version = "0.1.0-dev"

// VersionLine is the one line 'duls --version' prints.
func VersionLine() string { return "duls " + Version }
