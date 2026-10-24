// Package app is the process body of duls: it resolves the requested
// directory, walks it, counts the lines of what the view can show, and renders
// the size view, animating it on a colour terminal.
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/duls/internal/agent"
	"github.com/baalimago/duls/internal/cache"
	"github.com/baalimago/duls/internal/frame"
	"github.com/baalimago/duls/internal/measure"
	"github.com/baalimago/duls/internal/plan"
	"github.com/baalimago/duls/internal/scan"
	"github.com/baalimago/duls/internal/tui"
	"github.com/baalimago/duls/internal/view"
)

// Request is one size view: where to look and which policies apply.
type Request struct {
	// Path is the directory to inspect; empty means the working directory.
	Path string
	// All includes hidden entries.
	All bool
	// Types measures every regular file, not only the admitted ones.
	Types bool
	// Bytes exact integers instead of SI units in the visual view.
	Bytes bool
	// JSON forces the agent document, even on a terminal.
	JSON bool
	// Mode selects the workers and the read buffer, 0 to 3.
	Mode int
	// Refresh is the record trust window; 0 disables the cache.
	Refresh time.Duration
	// Timeout is the run's deadline; 0 means none.
	Timeout time.Duration
}

// The composition's constants are the README's parameters.
const (
	frameInterval = 100 * time.Millisecond
	storeCap      = 32 << 20
	flushBudget   = 200 * time.Millisecond
	largeCap      = 64 << 20
	minChunk      = 16 << 10
	maxChunk      = 64 << 20
)

// Run scans the requested directory and writes its size view to the
// options' stdout.
func Run(ctx context.Context, opts Options, req Request) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("duls: %w", err)
	}
	root, err := resolveRoot(opts.Cwd, req.Path)
	if err != nil {
		return err
	}
	preset, ok := scan.Preset(scan.Mode(req.Mode))
	if !ok {
		return fmt.Errorf("duls: mode %d is not a mode", req.Mode)
	}
	out := opts.Out()
	style := opts.styleOf()
	form := opts.formOf(out, req.JSON)
	viewOpts := view.Options{Style: style, Width: opts.widthOf(out), Bytes: req.Bytes, Limit: req.Timeout}
	planner := plan.New()
	renderer := view.New()
	liveOpts := viewOpts
	liveOpts.Provisional = true
	live := &live{planner: planner, renderer: renderer, opts: liveOpts}
	raster, sink := frame.New(frame.Options{
		Out:      out,
		Enabled:  form == tui.Visual,
		Trigger:  0,
		Interval: frameInterval,
		Lines:    live.lines,
	}), scan.Sink(nil)
	// The frame never outlives the run: a failure leaves no painted progress
	// behind the one error line (architecture/frame.md).
	defer raster.Erase()
	if form == tui.Visual {
		sink = raster
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	store := openStore(opts, root, req)
	defer store.Close()
	snapshot, err := scan.New().Walk(ctx, root, scan.WalkOptions{
		All:     req.All,
		Skip:    storeSkip(opts, root, req),
		Workers: preset.Workers,
		Buffer:  preset.Buffer,
		Nice:    preset.Nice,
	}, sink)
	if err != nil {
		return err
	}
	// The device is a fact of this run: what it holds is what the run could not
	// reach. A platform that cannot report it states no device usage, never a
	// failure (architecture/scan.md).
	if fs, err := scan.FilesystemOf(root); err == nil {
		snapshot.Filesystem = &fs
	}
	live.set(snapshot)
	large := int64(0)
	if req.Timeout > 0 {
		large = largeCap
	}
	result, err := measure.New().Count(ctx, measure.Plan{Root: root, Dirs: snapshot.Dirs}, measure.Options{
		Workers: preset.Workers,
		Chunk:   min(max(preset.Buffer, minChunk), maxChunk/preset.Workers),
		Large:   large,
		All:     req.All,
		Types:   req.Types,
		Store:   store,
		Sink:    countSink(sink, snapshot),
	})
	if err != nil {
		return err
	}
	measured := plan.Measured{
		Lines:      result.Named,
		Total:      result.Lines,
		Measured:   result.Measured,
		Skipped:    result.Skipped,
		Unknown:    result.Unknown,
		Unreadable: result.Unreadable,
		Reused:     result.Reused,
		Stopped:    result.Stopped,
	}
	planned := planner.Plan(snapshot, measured)
	if form == tui.Agent {
		return writeAgent(out, store, root, req.Timeout, planned)
	}
	lines := renderer.Lines(planned, viewOpts)
	if sink != nil {
		raster.Erase()
	}
	if _, err := io.WriteString(out, strings.Join(lines, "\n")+"\n"); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	_ = store.Flush(flushBudget)
	return nil
}

// writeAgent writes the agent document of the run and flushes the store. It
// reports an encode failure rather than falling back to the visual view, so a
// program never reads a layout it did not ask for.
func writeAgent(out io.Writer, store cache.Store, root string, limit time.Duration, v plan.View) error {
	doc, err := agent.Encode(agent.Request{Root: root, Limit: limit, View: v})
	if err != nil {
		return fmt.Errorf("duls: %w", err)
	}
	if _, err := out.Write(append(doc, '\n')); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	_ = store.Flush(flushBudget)
	return nil
}

// openStore opens the record store, or a disabled one when the store is off or
// unusable: a cache problem is never a run failure (I14).
func openStore(opts Options, root string, req Request) cache.Store {
	config := cache.Config{
		Dir:     opts.cacheDir(),
		Root:    root,
		Cap:     storeCap,
		Refresh: req.Refresh,
	}
	store, err := cache.Open(config)
	if err == nil {
		return store
	}
	config.Dir = ""
	store, _ = cache.Open(config)
	return store
}

// storeSkip is the store's directory when it lies inside the scanned root and
// the store is on: the walk leaves it out, so a run never reports the files it
// writes (I10). A run with the store off skips nothing.
func storeSkip(opts Options, root string, req Request) []string {
	if req.Refresh <= 0 {
		return nil
	}
	dir := opts.cacheDir()
	if dir == "" {
		return nil
	}
	resolved, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	if resolved == root || !cache.Inside(resolved, root) {
		return nil
	}
	return []string{resolved}
}

// countSink joins phase-2 counters to the walk's totals, so the frame receives
// one monotone stream across both phases (README D29). Bytes and Lines are
// already cumulative across the phases.
func countSink(sink scan.Sink, snapshot scan.Snapshot) scan.Sink {
	if sink == nil {
		return nil
	}
	return offsetSink{inner: sink, files: int64(snapshot.Files), dirs: int64(len(snapshot.Dirs))}
}

type offsetSink struct {
	inner scan.Sink
	files int64
	dirs  int64
}

func (s offsetSink) Publish(p scan.Progress) {
	p.Files += s.files
	p.Dirs += s.dirs
	s.inner.Publish(p)
}

// live renders the frame's lines from the walk's fact set and the count's
// progress, using the planner and the renderer of the answer (I15).
type live struct {
	mu       sync.Mutex
	snapshot *scan.Snapshot
	planner  plan.Planner
	renderer view.Renderer
	opts     view.Options
}

func (l *live) set(snapshot scan.Snapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.snapshot = &snapshot
}

// lines draws the frame from the facts held right now: the walk's running
// totals before the snapshot exists, then that snapshot with the count's
// progress. Every value it has not proved stays marked (README I3).
func (l *live) lines(p scan.Progress) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := scan.Snapshot{Files: int(p.Files), Bytes: p.Bytes, Stopped: scan.StopDeadline}
	if l.snapshot != nil {
		snapshot = *l.snapshot
	}
	measured := plan.Measured{Total: p.Lines, Measured: int(p.Files)}
	return l.renderer.Lines(l.planner.Plan(snapshot, measured), l.opts)
}

// resolveRoot makes the requested directory absolute and resolves it, so a root
// named through a symbolic link is walked by its target (I8).
func resolveRoot(cwd, path string) (string, error) {
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		if cwd == "" {
			var err error
			if cwd, err = os.Getwd(); err != nil {
				return "", fmt.Errorf("duls: resolve the working directory: %w", err)
			}
		}
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("duls: inspect %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("duls: %s is not a directory", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("duls: resolve %s: %w", path, err)
	}
	return resolved, nil
}
