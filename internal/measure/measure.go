// Package measure counts the lines of admitted files in a fixed order, with a
// bounded pool, under the run's deadline, publishing a fact only when the
// file's witness survived the read.
package measure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/duls/internal/scan"
)

// Plan is the dispatch input: the walk's directories, in DirSeq order.
type Plan struct {
	Root string
	Dirs []scan.Dir
}

// Measured is the store seam the count consumes. Phase 6 implements it.
type Measured interface {
	// FileLines returns the line count stored for a file with this witness.
	//
	//   - It reports false on any witness difference and never returns an error.
	//     (FileLines.1)
	//   - It reports false for a fact outside the configured trust window; a
	//     damaged store reads as a miss. (FileLines.2)
	//   - A returned count follows the LF rule. (FileLines.3, I7)
	FileLines(path string, w scan.Witness) (int64, bool)

	// StoreFact offers one proven fact to the store.
	//
	//   - It never blocks the count and never fails: a full mailbox drops the
	//     fact, which costs a future re-read and nothing else. (StoreFact.1)
	//   - It does not persist a fact until its witness is older than the settle
	//     window. (StoreFact.2, I25)
	StoreFact(f Fact)
}

// Fact is one proven line count.
type Fact struct {
	Path  string
	Lines int64
	W     scan.Witness
}

// Options are the count's parameters.
type Options struct {
	Workers int
	Chunk   int
	Large   int64
	All     bool
	Types   bool
	Now     func() time.Time
	Open    func(string) (*os.File, error)
	Store   Measured
	Sink    scan.Sink
}

// Result is what one count proved.
type Result struct {
	Lines      int64
	Measured   int
	Skipped    int
	Unknown    int
	Unreadable int
	Reused     int
	Named      map[string]int64
	Stopped    scan.Stop
}

// Counter counts lines under a deadline.
type Counter interface {
	// Count walks the plan's directories and counts the admitted files.
	//
	//   - Directories are visited in plan.Dirs order; inside one directory the
	//     admitted files are dispatched by size descending then name, one file
	//     per free worker, and no file is dispatched twice. At dispatch, Count
	//     takes its own path witness; it does not use a per-file witness from the
	//     walk. Worker interleaving does not change the completed measured set.
	//     (Count.1, I17, I22)
	//   - An admitted file is a regular file whose final extension is not on the
	//     deny-list; an extensionless file is admitted. Options.Types admits
	//     every regular file. Hidden entries follow Options.All. A denied file is
	//     Skipped, never opened. (Count.2, I8)
	//   - A file whose size is at least Options.Large, when Large is above zero,
	//     is Skipped without being opened. (Count.3)
	//   - A file is read in Chunk-sized pieces over one buffer per worker, and
	//     lines follow the LF rule. Count is the content-reading phase; the walk
	//     does not open regular files. (Count.4, I7, I16)
	//   - A fact exists only when the dispatch witness, pre-read and post-read
	//     descriptor witnesses, and the path witness after close agree, the read
	//     reached EOF, and the count does not exceed the file's size; anything else
	//     is Unknown and is not stored. (Count.5, I3, I25)
	//   - A store hit with a matching witness is Measured without opening the
	//     file; the coordinator is the only caller of StoreFact. (Count.6)
	//   - An open refused for permission is Unknown and Unreadable, not an
	//     error. A read or close failure returns a wrapped error and no Result;
	//     a failed witness is Unknown and is not stored. (Count.7, I24)
	//   - context.DeadlineExceeded returns the whole facts held with Stopped set
	//     and a nil error. context.Canceled returns a wrapped error and no
	//     Result, and it outranks the deadline when both are observable.
	//     (Count.8, I11)
	//   - Measured + Skipped + Unknown equals the number of files dispatched, and
	//     Unreadable is counted inside Unknown. (Count.9, I23)
	//   - Every goroutine it started has exited before it returns. (Count.10, I19)
	//   - Named holds the line count of every measured file that is one of its
	//     directory's six largest children, keyed by root-relative path. (Count.11)
	//   - Cancellation wins over a simultaneous deadline; the first non-context
	//     failure cancels remaining workers. (Count.12, I20, D26)
	//   - Count publishes PhaseCount progress through opts.Sink with cumulative
	//     completed Files, measured Lines and completed Dirs counters, and Bytes
	//     held at the walk's byte total. (Count.13, D29)
	Count(ctx context.Context, plan Plan, opts Options) (Result, error)
}

// New returns the counter.
func New() Counter {
	return counter{}
}

const (
	defaultWorkers = 1
	defaultChunk   = 64 << 10
	publishEvery   = 100 * time.Millisecond
)

type counter struct{}

type job struct {
	path    string
	rel     string
	size    int64
	witness scan.Witness
	peer    bool
}

type outcome struct {
	job
	lines      int64
	measured   bool
	unknown    bool
	unreadable bool
	aborted    bool
	err        error
}

type candidate struct {
	job
	skipped    bool
	unknown    bool
	unreadable bool
}

func (counter) Count(ctx context.Context, plan Plan, opts Options) (Result, error) {
	opts = withDefaults(opts)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := &state{opts: opts, named: map[string]int64{}, sink: opts.Sink, now: opts.Now}
	if len(plan.Dirs) > 0 {
		state.bytes = plan.Dirs[0].Bytes
	}
	var cut bool
	for _, dir := range plan.Dirs {
		if ctx.Err() != nil {
			cut = true
			break
		}
		candidates, err := counter{}.list(plan.Root, dir, opts)
		if err != nil {
			return Result{}, err
		}
		var jobs []job
		for _, cand := range candidates {
			switch {
			case cand.unknown:
				state.unknown++
			case cand.skipped:
				state.skipped++
			default:
				state.lookup(cand.job)
				if !state.hit {
					jobs = append(jobs, cand.job)
				}
			}
			state.tick(false)
		}
		outcomes, err := counter{}.run(ctx, jobs, opts, cancel)
		if err != nil {
			return Result{}, err
		}
		for _, out := range outcomes {
			if out.err != nil {
				return Result{}, out.err
			}
			state.apply(out)
		}
		state.dirs++
		state.tick(true)
		if ctx.Err() != nil {
			cut = true
			break
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return Result{}, fmt.Errorf("measure: count: %w", context.Canceled)
	}
	stopped := scan.StopNone
	if cut && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		stopped = scan.StopDeadline
	}
	return Result{
		Lines:      state.lines,
		Measured:   state.measured,
		Skipped:    state.skipped,
		Unknown:    state.unknown,
		Unreadable: state.unreadable,
		Reused:     state.reused,
		Named:      state.named,
		Stopped:    stopped,
	}, nil
}

type state struct {
	opts       Options
	sink       scan.Sink
	now        func() time.Time
	named      map[string]int64
	bytes      int64
	lines      int64
	measured   int
	skipped    int
	unknown    int
	unreadable int
	reused     int
	dirs       int64
	hit        bool
	last       time.Time
}

func (s *state) lookup(j job) {
	s.hit = false
	if s.opts.Store == nil {
		return
	}
	lines, ok := s.opts.Store.FileLines(j.path, j.witness)
	if !ok {
		return
	}
	s.hit = true
	s.reused++
	s.measured++
	s.lines += lines
	s.name(j, lines)
}

func (s *state) apply(out outcome) {
	switch {
	case out.unknown:
		s.unknown++
		if out.unreadable {
			s.unreadable++
		}
		if out.aborted {
			// The run was cut: the fact is not whole and not stored.
		}
	case out.measured:
		s.measured++
		s.lines += out.lines
		s.name(out.job, out.lines)
		if s.opts.Store != nil {
			s.opts.Store.StoreFact(Fact{Path: out.path, Lines: out.lines, W: out.witness})
		}
	}
	s.tick(false)
}

func (s *state) name(j job, lines int64) {
	if j.peer {
		s.named[j.rel] = lines
	}
}

func (s *state) tick(force bool) {
	if s.sink == nil {
		return
	}
	now := s.now()
	if !force && now.Sub(s.last) < publishEvery {
		return
	}
	s.last = now
	s.sink.Publish(scan.Progress{
		Phase: scan.PhaseCount,
		Files: int64(s.measured + s.skipped + s.unknown),
		Bytes: s.bytes,
		Lines: s.lines,
		Dirs:  s.dirs,
	})
}

func (c counter) list(root string, dir scan.Dir, opts Options) ([]candidate, error) {
	path := filepath.Join(root, filepath.FromSlash(dir.Path))
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("measure: read %s: %w", path, err)
	}
	peek := make(map[string]bool, len(dir.Kids))
	for _, kid := range dir.Kids {
		if !kid.Dir {
			peek[kid.Name] = true
		}
	}
	var out []candidate
	for _, entry := range entries {
		name := entry.Name()
		if !opts.All && strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		rel := join(dir.Path, name)
		abs := filepath.Join(path, name)
		cand := candidate{job: job{path: abs, rel: rel, peer: peek[name]}}
		if !opts.Types && denied(name) {
			cand.skipped = true
			out = append(out, cand)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				cand.unknown, cand.unreadable = true, true
				out = append(out, cand)
				continue
			}
			return nil, fmt.Errorf("measure: stat %s: %w", abs, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		cand.size = info.Size()
		cand.witness = scan.WitnessOf(info)
		if opts.Large > 0 && cand.size >= opts.Large {
			cand.skipped = true
		}
		out = append(out, cand)
	}
	slices.SortFunc(out, func(a, b candidate) int {
		if a.size != b.size {
			return compareDesc(a.size, b.size)
		}
		return strings.Compare(a.rel, b.rel)
	})
	return out, nil
}

func (c counter) run(ctx context.Context, jobs []job, opts Options, cancel context.CancelFunc) ([]outcome, error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	jobc := make(chan job)
	outc := make(chan outcome, len(jobs))
	var wg sync.WaitGroup
	workers := min(opts.Workers, len(jobs))
	for range workers {
		wg.Go(func() {
			for j := range jobc {
				out := c.measure(ctx, j, opts)
				if out.err != nil {
					cancel()
				}
				outc <- out
			}
		})
	}
	go func() {
		defer close(jobc)
		for _, j := range jobs {
			select {
			case jobc <- j:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(outc)
	var out []outcome
	for o := range outc {
		out = append(out, o)
	}
	return out, nil
}

func (c counter) measure(ctx context.Context, j job, opts Options) outcome {
	out := outcome{job: j}
	file, err := opts.Open(j.path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			out.unknown, out.unreadable = true, true
			return out
		}
		out.err = fmt.Errorf("measure: open %s: %w", j.path, err)
		return out
	}
	pre, err := file.Stat()
	if err != nil {
		_ = file.Close()
		out.err = fmt.Errorf("measure: stat %s: %w", j.path, err)
		return out
	}
	if scan.WitnessOf(pre) != j.witness {
		_ = file.Close()
		out.unknown = true
		return out
	}
	lines, err := countLines(ctx, file, opts.Chunk)
	if errors.Is(err, errAborted) {
		_ = file.Close()
		out.unknown, out.aborted = true, true
		return out
	}
	if err != nil {
		_ = file.Close()
		out.err = fmt.Errorf("measure: read %s: %w", j.path, err)
		return out
	}
	post, err := file.Stat()
	if err != nil {
		_ = file.Close()
		out.err = fmt.Errorf("measure: stat %s: %w", j.path, err)
		return out
	}
	if scan.WitnessOf(post) != j.witness {
		_ = file.Close()
		out.unknown = true
		return out
	}
	if err := file.Close(); err != nil {
		out.err = fmt.Errorf("measure: close %s: %w", j.path, err)
		return out
	}
	after, err := os.Lstat(j.path)
	if err != nil || scan.WitnessOf(after) != j.witness {
		out.unknown = true
		return out
	}
	if lines > j.size {
		out.unknown = true
		return out
	}
	out.measured = true
	out.lines = lines
	return out
}

var errAborted = errors.New("count aborted")

type lineCounter struct {
	newlines int64
	last     byte
	size     int64
}

func (c *lineCounter) add(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	c.size += int64(len(chunk))
	for _, b := range chunk {
		if b == '\n' {
			c.newlines++
		}
	}
	c.last = chunk[len(chunk)-1]
}

func (c *lineCounter) lines() int64 {
	if c.size == 0 {
		return 0
	}
	if c.last == '\n' {
		return c.newlines
	}
	return c.newlines + 1
}

func countLines(ctx context.Context, file *os.File, chunk int) (int64, error) {
	buf := make([]byte, chunk)
	var counter lineCounter
	for {
		if ctx.Err() != nil {
			return 0, errAborted
		}
		n, err := file.Read(buf)
		if n > 0 {
			counter.add(buf[:n])
		}
		switch {
		case errors.Is(err, io.EOF):
			return counter.lines(), nil
		case err != nil:
			return 0, err
		}
	}
}

var deniedExtensions = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "bmp": true, "tif": true, "tiff": true,
	"webp": true, "ico": true, "heic": true, "heif": true, "avif": true, "psd": true,
	"zip": true, "tar": true, "gz": true, "tgz": true, "bz2": true, "tbz2": true, "xz": true,
	"txz": true, "zst": true, "7z": true, "rar": true, "lz4": true, "cab": true, "iso": true,
	"mp3": true, "mp4": true, "m4a": true, "m4v": true, "wav": true, "flac": true, "ogg": true,
	"ogv": true, "webm": true, "avi": true, "mkv": true, "mov": true, "wmv": true, "aac": true,
	"opus": true, "wma": true, "mpg": true, "mpeg": true, "flv": true,
	"o": true, "a": true, "so": true, "dylib": true, "obj": true, "pyc": true, "pyo": true,
	"class": true, "wasm": true, "lib": true, "pdb": true,
	"exe": true, "dll": true, "com": true, "msi": true, "app": true, "elf": true,
}

func denied(name string) bool {
	ext := filepath.Ext(name)
	if ext == "" {
		return false
	}
	return deniedExtensions[strings.ToLower(ext[1:])]
}

func withDefaults(opts Options) Options {
	if opts.Workers < 1 {
		opts.Workers = defaultWorkers
	}
	if opts.Chunk < 1 {
		opts.Chunk = defaultChunk
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Open == nil {
		opts.Open = os.Open
	}
	return opts
}

func compareDesc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func join(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}
