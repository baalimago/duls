// Package app is the process body of duls: it resolves the requested
// directory, scans it, and renders its size view.
package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/baalimago/duls/internal/tui"
)

// Options are the streams and the environment a command runs over. A nil
// stream takes the process stream; a nil Env reads no variable.
type Options struct {
	Stdout io.Writer
	Stderr io.Writer
	Env    []string
	Cwd    string
	// Style is the test seam for the colour depth of a visual answer; nil
	// reads COLORTERM from Env.
	Style func() tui.Style
	// Form is the test seam for the answer's encoding; nil detects on the
	// stream the answer goes to.
	Form func(io.Writer) tui.Form
	// Width is the test seam for the view's width; nil asks the stream.
	Width func(io.Writer) int
}

// Out is the view's stream: Stdout, or the process stdout when unset.
func (o Options) Out() io.Writer {
	if o.Stdout != nil {
		return o.Stdout
	}
	return os.Stdout
}

// ErrOut is the diagnostic stream: Stderr, or the process stderr when unset.
func (o Options) ErrOut() io.Writer {
	if o.Stderr != nil {
		return o.Stderr
	}
	return os.Stderr
}

func (o Options) styleOf() tui.Style {
	if o.Style != nil {
		return o.Style()
	}
	return tui.DepthOf(EnvLookup(o.Env))
}

// formOf chooses the answer's encoding. The flag wins over the environment, so
// a program that asked for the document always gets it (CLI.9).
func (o Options) formOf(w io.Writer, json bool) tui.Form {
	if json {
		return tui.Agent
	}
	if o.Form != nil {
		return o.Form(w)
	}
	return tui.DetectForm(w, EnvLookup(o.Env))
}

func (o Options) widthOf(w io.Writer) int {
	if o.Width != nil {
		return o.Width(w)
	}
	return tui.Width(w)
}

// EnvLookup returns a getenv over env, the process environment as KEY=value
// pairs, with the last value winning.
func EnvLookup(env []string) func(string) string {
	values := environ(env)
	return func(name string) string { return values[name] }
}

// cacheDir is the record store's root: $XDG_CACHE_HOME/duls/v1, or
// $HOME/.cache/duls/v1 when XDG_CACHE_HOME is unset, as the XDG base directory
// spec says. It is empty, which means the store is off, only when both are
// unset (CLI.7).
func (o Options) cacheDir() string {
	lookup := EnvLookup(o.Env)
	base := lookup("XDG_CACHE_HOME")
	if base == "" {
		home := lookup("HOME")
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "duls", "v1")
}

func environ(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			values[kv[:i]] = kv[i+1:]
		}
	}
	return values
}
