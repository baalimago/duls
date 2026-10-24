package app

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/baalimago/duls/internal/tui"
)

func TestEnvLookupKeepsTheLastValue(t *testing.T) {
	t.Parallel()
	getenv := EnvLookup([]string{"A=1", "A=2", "B=x=y", "=bad", "NO_EQUALS", "EMPTY="})
	for _, tt := range []struct{ name, want string }{
		{"A", "2"}, {"B", "x=y"}, {"", ""}, {"NO_EQUALS", ""}, {"EMPTY", ""},
	} {
		if got := getenv(tt.name); got != tt.want {
			t.Fatalf("getenv(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestOutAndErrOutFallBackToTheProcessStreams(t *testing.T) {
	t.Parallel()
	if (Options{}).Out() != os.Stdout {
		t.Fatal("an unset Stdout must be os.Stdout")
	}
	if (Options{}).ErrOut() != os.Stderr {
		t.Fatal("an unset Stderr must be os.Stderr")
	}
	var out, errOut strings.Builder
	opts := Options{Stdout: &out, Stderr: &errOut}
	if opts.Out() != &out || opts.ErrOut() != &errOut {
		t.Fatal("the set streams must be used")
	}
}

func TestStyleWidthAndFormSeams(t *testing.T) {
	t.Parallel()
	opts := Options{
		Style: func() tui.Style { return tui.New(tui.TrueColor) },
		Width: func(io.Writer) int { return 42 },
		Form:  func(io.Writer) tui.Form { return tui.Visual },
	}
	var out strings.Builder
	if got := opts.styleOf(); got != tui.New(tui.TrueColor) {
		t.Fatalf("styleOf = %v, want the seam's depth", got)
	}
	if got := opts.widthOf(&out); got != 42 {
		t.Fatalf("widthOf = %d, want the seam's width", got)
	}
	if got := opts.formOf(&out, false); got != tui.Visual {
		t.Fatalf("formOf = %v, want the seam's form", got)
	}
	if got := opts.formOf(&out, true); got != tui.Agent {
		t.Fatalf("formOf with -json = %v, want Agent to win over the seam", got)
	}
}

func TestFormWithoutATerminalIsAgent(t *testing.T) {
	t.Parallel()
	if got := (Options{}).formOf(&strings.Builder{}, false); got != tui.Agent {
		t.Fatalf("formOf = %v, want Agent for a stream that is not a terminal", got)
	}
}

func TestCacheDirFollowsTheXDGSpec(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		env  []string
		want string
	}{
		{"XDG set", []string{"XDG_CACHE_HOME=/x"}, "/x/duls/v1"},
		{"XDG unset falls back to HOME", []string{"HOME=/h"}, "/h/.cache/duls/v1"},
		{"XDG empty falls back to HOME", []string{"XDG_CACHE_HOME=", "HOME=/h"}, "/h/.cache/duls/v1"},
		{"neither set", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Options{Env: tt.env}).cacheDir(); got != tt.want {
				t.Fatalf("cacheDir = %q, want %q", got, tt.want)
			}
		})
	}
}
