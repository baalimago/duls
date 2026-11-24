// Package duls is the duls command: the one direct invocation that prints
// a directory's size view. It owns the flag surface and the positional
// path; the process body walks, counts and renders.
package duls

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/baalimago/duls/internal/app"
	"github.com/baalimago/duls/internal/scan"
)

type command struct {
	opts     app.Options
	flagset  *flag.FlagSet
	all      bool
	types    bool
	bytes    bool
	json     bool
	version  bool
	mode     int
	refresh  time.Duration
	timeout  time.Duration
	path     string
	excludes []excludeSource
	exclude  scan.Exclude
	defaults app.Request
}

// excludeSource is one --exclude pattern or one -X file, in command-line order.
type excludeSource struct {
	value    string
	fromFile bool
}

// excludeFlag records --exclude patterns or -X files; Parse compiles them (architecture/cli.md).
type excludeFlag struct {
	sources  *[]excludeSource
	fromFile bool
}

func (f excludeFlag) String() string { return "" }

func (f excludeFlag) Set(value string) error {
	*f.sources = append(*f.sources, excludeSource{value: value, fromFile: f.fromFile})
	return nil
}

// patternLine is one pattern of an -X file and its line number.
type patternLine struct {
	pattern string
	number  int
}

// Command returns the duls command over the given streams and environment.
func Command(opts app.Options) *command {
	fs := flag.NewFlagSet("duls", flag.ContinueOnError)
	// Diagnostics are formatted by this process, not by the flag package.
	fs.SetOutput(io.Discard)
	c := &command{opts: opts, flagset: fs, defaults: app.Request{
		Mode:    1,
		Refresh: 15 * time.Minute,
		Timeout: 2 * time.Second,
	}}
	fs.BoolVar(&c.all, "all", false, "include hidden entries")
	fs.BoolVar(&c.all, "a", false, "include hidden entries (shorthand)")
	fs.BoolVar(&c.types, "all-types", false, "measure every regular file")
	fs.BoolVar(&c.bytes, "bytes", false, "print exact byte counts")
	fs.BoolVar(&c.json, "json", false, "print the agent document as JSON")
	fs.Var(excludeFlag{sources: &c.excludes}, "exclude", "leave out the entries a glob matches")
	fs.Var(excludeFlag{sources: &c.excludes, fromFile: true}, "exclude-from", "read exclude patterns from a file")
	fs.Var(excludeFlag{sources: &c.excludes, fromFile: true}, "X", "read exclude patterns from a file (shorthand)")
	fs.IntVar(&c.mode, "mode", c.defaults.Mode, "workers and buffer preset, 0 to 3")
	fs.IntVar(&c.mode, "m", c.defaults.Mode, "workers and buffer preset (shorthand)")
	fs.DurationVar(&c.refresh, "refresh", c.defaults.Refresh, "record trust window; 0 disables the cache")
	fs.DurationVar(&c.refresh, "r", c.defaults.Refresh, "record trust window (shorthand)")
	fs.DurationVar(&c.timeout, "timeout", c.defaults.Timeout, "run deadline; 0 means none")
	fs.DurationVar(&c.timeout, "t", c.defaults.Timeout, "run deadline (shorthand)")
	fs.BoolVar(&c.version, "version", false, "print the version")
	fs.BoolVar(&c.version, "v", false, "print the version (shorthand)")
	return c
}

// Help is the command's help text, printed by -h and on a flag error.
func (c *command) Help() string { return helpText }

// Parse resolves the flags and the one optional positional path. Flags may
// follow the path, and "--" ends flag parsing.
func (c *command) Parse(args []string) error {
	positionals, err := parseCommandLine(c.flagset, args)
	if err != nil {
		return fmt.Errorf("flag: %w; run 'duls -h' for its flags", err)
	}
	switch len(positionals) {
	case 0:
	case 1:
		c.path = positionals[0]
	default:
		return fmt.Errorf("at most one path argument is accepted, got %d", len(positionals))
	}
	if err := c.validate(); err != nil {
		return err
	}
	exclude, err := c.compileExcludes()
	if err != nil {
		return err
	}
	c.exclude = exclude
	return nil
}

// compileExcludes reads the -X files and compiles every pattern in order (architecture/cli.md).
func (c *command) compileExcludes() (scan.Exclude, error) {
	var patterns, origins []string
	for _, source := range c.excludes {
		if !source.fromFile {
			patterns, origins = append(patterns, source.value), append(origins, "")
			continue
		}
		lines, err := c.readExcludeFile(source.value)
		if err != nil {
			return scan.Exclude{}, fmt.Errorf("flag: read the exclude file %s: %w; run 'duls -h' for its flags", source.value, err)
		}
		for _, line := range lines {
			patterns = append(patterns, line.pattern)
			origins = append(origins, fmt.Sprintf("%s:%d", source.value, line.number))
		}
	}
	exclude, err := scan.NewExclude(patterns)
	if err == nil {
		return exclude, nil
	}
	var refused *scan.PatternError
	if errors.As(err, &refused) && origins[refused.Index] != "" {
		return scan.Exclude{}, fmt.Errorf("flag: %s: %w; run 'duls -h' for its flags", origins[refused.Index], err)
	}
	return scan.Exclude{}, fmt.Errorf("flag: %w; run 'duls -h' for its flags", err)
}

// readExcludeFile reads one pattern per line, as ncdu does.
func (c *command) readExcludeFile(name string) ([]patternLine, error) {
	path, err := c.opts.Abs(name)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []patternLine
	for i, line := range strings.Split(string(content), "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			lines = append(lines, patternLine{pattern: line, number: i + 1})
		}
	}
	return lines, nil
}

// validate rejects the flag values the presets and the deadline cannot use.
func (c *command) validate() error {
	if c.mode < 0 || int(c.mode) > 3 {
		return fmt.Errorf("flag: mode must be 0, 1, 2 or 3, got %d", c.mode)
	}
	if c.refresh < 0 {
		return fmt.Errorf("flag: refresh must not be negative, got %s", c.refresh)
	}
	if c.timeout < 0 {
		return fmt.Errorf("flag: timeout must not be negative, got %s", c.timeout)
	}
	return nil
}

// Request is the run this command line names.
func (c *command) Request() app.Request {
	return app.Request{
		Path:    c.path,
		All:     c.all,
		Types:   c.types,
		Bytes:   c.bytes,
		JSON:    c.json,
		Mode:    c.mode,
		Refresh: c.refresh,
		Timeout: c.timeout,
		Exclude: c.exclude,
	}
}

// Run prints the version, or the size view of the resolved directory.
func (c *command) Run(ctx context.Context) error {
	if c.version {
		if _, err := fmt.Fprintln(c.opts.Out(), app.VersionLine()); err != nil {
			return fmt.Errorf("duls: write the version: %w", err)
		}
		return nil
	}
	return app.Run(ctx, c.opts, c.Request())
}

// parseCommandLine parses flags interleaved with positionals: the flag
// package stops at the first positional, so the remaining arguments are
// parsed again. "--" ends flag parsing; everything after it is positional.
func parseCommandLine(fs *flag.FlagSet, args []string) ([]string, error) {
	if i := slices.Index(args, "--"); i >= 0 {
		positionals, err := parseFlagsAndPositionals(fs, args[:i])
		if err != nil {
			return nil, err
		}
		return append(positionals, args[i+1:]...), nil
	}
	return parseFlagsAndPositionals(fs, args)
}

func parseFlagsAndPositionals(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		left := fs.Args()
		if len(left) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, left[0])
		rest = left[1:]
	}
}

const helpText = `duls - the size view of a directory

Usage:
  duls [flags] [path]

[path] is the directory to inspect and defaults to the working directory.
duls walks the directory and prints the view slivingdoc prints when an
initial pull materializes a notebook into an empty directory: a strip of the
largest entries, the file, size and line totals, and one row per entry with
its files, size, share, and largest children. Rows are ordered and shared by
bytes. Entries below the top level are promoted into rows while rows remain,
and the smallest top-level entries fold into one row. A file's lines follow
the LF rule: one per LF, plus a final run without an LF; empty content has
zero lines.

--exclude leaves out every directory and regular file whose path matches a
glob, as ncdu does: the pattern is tested against the absolute path and every
suffix that begins after a '/', and '*', '?' and a bracket match '/' too. So
node_modules leaves out every entry of that name, '*.log' every log file, and
src/gen every path that ends in src/gen. -X reads patterns from a file, one per
line. An excluded directory is never entered, the totals stay exact, and the
footer counts the entries left out. A pattern that can never match a path, such
as one that ends in '/', is an error.

Hidden entries, names that begin with '.', are skipped unless --all is set.
Symbolic links and special files are skipped. duls stays on the filesystem of
[path]: a directory on another device, such as /proc, /sys or a mounted disk,
is not entered, the totals above it are lower bounds, and the footer counts it.
Sizes are apparent bytes, the size the filesystem reports for each file. When
it cannot reach the whole tree it also states the device's used bytes, the
figure df prints, so the difference is declared. A terminal gets colour, a
layout that adapts to its width between 40 and 150 columns, and a live frame of
the outline while the scan runs. A pipe, a file, /dev/null, a non-empty
NO_COLOR, or -json gets the agent document instead: the same facts as JSON, no
frame, and no escape byte.

The walk reads no file content. Line counts come from a second pass over the
files the view can show, bounded by the deadline. A spent deadline prints a
partial view and exits 0.

Flags:
  -a, --all              include hidden entries
      --all-types        measure every regular file; disable the deny-list
      -bytes             exact byte counts instead of SI units, visual view only
      --exclude p        leave out the entries the glob p matches; repeatable
  -X, --exclude-from f   read exclude patterns from file f, one per line
      -json              print the agent document as JSON
  -m, --mode n           0, 1, 2 or 3: workers and read buffer; default 1
  -r, --refresh d        record trust window; default 15m; 0 = no cache
  -t, --timeout d        run deadline; default 2s; 0 = none
  -h, --help             print this help
  -v, --version          print the version
`
