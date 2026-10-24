package tui

import (
	"bytes"
	"testing"
)

func TestFormOfABufferIsAgent(t *testing.T) {
	t.Parallel()
	if got := DetectForm(&bytes.Buffer{}, func(string) string { return "" }); got != Agent {
		t.Fatalf("DetectForm = %v, want Agent for a buffer", got)
	}
}

func TestFormWithNoColorIsAgent(t *testing.T) {
	t.Parallel()
	// NO_COLOR is honoured before the terminal gate, so this is provable
	// without a terminal.
	if got := DetectForm(&bytes.Buffer{}, func(string) string { return "1" }); got != Agent {
		t.Fatalf("DetectForm = %v, want Agent with NO_COLOR", got)
	}
}

func TestDepthOfReadsColorterm(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		env  string
		want Depth
	}{
		{name: "truecolor", env: "truecolor", want: TrueColor},
		{name: "24bit", env: "24bit", want: TrueColor},
		{name: "unset", env: "", want: Basic},
		{name: "unknown", env: "16color", want: Basic},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DepthOf(func(string) string { return tt.env })
			if got.depth != tt.want {
				t.Fatalf("DepthOf(%q) = %v, want %v", tt.env, got.depth, tt.want)
			}
		})
	}
}

func TestStyleCodes(t *testing.T) {
	t.Parallel()
	basic := New(Basic)
	if got, want := basic.Dim("x"), "\x1b[2mx\x1b[0m"; got != want {
		t.Fatalf("basic Dim = %q, want %q", got, want)
	}
	if got, want := basic.Bold("x"), "\x1b[1mx\x1b[0m"; got != want {
		t.Fatalf("basic Bold = %q, want %q", got, want)
	}
	if got, want := basic.Segment(0, "x"), "\x1b[34mx\x1b[0m"; got != want {
		t.Fatalf("basic Segment = %q, want %q", got, want)
	}
	if got, want := basic.Segment(len(segmentHues), "x"), "\x1b[34mx\x1b[0m"; got != want {
		t.Fatalf("Segment wraps = %q, want %q", got, want)
	}
	trueColor := New(TrueColor)
	if got, want := trueColor.Segment(1, "x"), "\x1b[38;2;25;184;118mx\x1b[0m"; got != want {
		t.Fatalf("truecolor Segment = %q, want %q", got, want)
	}
	if got := trueColor.Segment(2, ""); got != "" {
		t.Fatalf("Segment of empty = %q, want empty", got)
	}
}
