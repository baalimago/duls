// Package itest is the black-box toolkit of the duls scenarios: it builds
// the released binary once and runs it as a real process, so a scenario
// sees only events in and events out.
package itest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var binary string

// cacheHome is the store root every scenario gets unless it names its own, so a
// run never writes the real user's cache.
var cacheHome string

// Main builds the binary and runs the package's scenarios.
func Main(m *testing.M) {
	dir, err := os.MkdirTemp("", "duls-itest-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "itest: create the build directory: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	if cacheHome, err = os.MkdirTemp("", "duls-itest-cache-"); err != nil {
		fmt.Fprintf(os.Stderr, "itest: create the cache directory: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(cacheHome)
	binary = filepath.Join(dir, "duls")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, ".")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "itest: build duls: %v\n%s", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// repoRoot is the module root above this package.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// Result is one finished process: its exit code and both streams.
type Result struct {
	Code   int
	Stdout string
	Stderr string
}

// Run runs duls in dir with the sanitized environment.
func Run(t *testing.T, dir string, args ...string) Result {
	t.Helper()
	return RunEnv(t, dir, nil, args...)
}

// RunEnv runs duls in dir with env over the sanitized environment.
func RunEnv(t *testing.T, dir string, env []string, args ...string) Result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = Env(env...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run duls %v: %v", args, err)
	}
	return Result{Code: code, Stdout: out.String(), Stderr: errOut.String()}
}

// Env is a deterministic environment: no inherited colour setting, and
// true colour announced so a terminal scenario proves the 24-bit palette.
func Env(extra ...string) []string {
	base := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"NO_COLOR=",
		"COLORTERM=truecolor",
	}
	if cacheHome != "" {
		base = append(base, "XDG_CACHE_HOME="+cacheHome)
	}
	return override(base, extra)
}

// override drops a base entry whose key extra names, so a scenario can replace
// any variable the harness sets.
func override(base, extra []string) []string {
	named := make(map[string]bool, len(extra))
	for _, kv := range extra {
		named[keyOf(kv)] = true
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		if !named[keyOf(kv)] {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
}

func keyOf(kv string) string {
	if before, _, ok := strings.Cut(kv, "="); ok {
		return before
	}
	return kv
}

// ANSIEscapeRE matches one ANSI SGR escape sequence.
var ANSIEscapeRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// StripANSI removes every ANSI SGR escape sequence from s.
func StripANSI(s string) string { return ANSIEscapeRE.ReplaceAllString(s, "") }

// Layout turns a visual answer into the layout text a scenario asserts: the
// frame's last erase ends one run's frame, the strip and the blank line after
// it are dropped, every escape sequence is removed, and every row box becomes
// the two-column margin it occupies. The result carries one trailing newline
// when it holds a line.
func Layout(styled string) string {
	if cut := strings.LastIndex(styled, "\x1b[2K"); cut >= 0 {
		styled = styled[cut+len("\x1b[2K"):]
	}
	text := strings.TrimSuffix(StripANSI(styled), "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 1 && strings.HasPrefix(lines[0], "█") && lines[1] == "" {
		lines = lines[2:]
	}
	for i, line := range lines {
		lines[i] = strings.Replace(line, "  ■ ", "  ", 1)
	}
	return strings.Join(lines, "\n") + "\n"
}
