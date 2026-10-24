// Package scan enumerates a directory tree without reading file content: a
// parallel breadth-first walk that yields exact file counts, byte totals and a
// declared completeness, plus the largest children the view's peek needs.
package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Mode names one of the four presets -m selects (README D8).
type Mode uint8

// Modes after what the running computer fan will act like
const (
	ModeOwl        Mode = iota // 1 worker, 64 KiB readdir buffer, nice 10
	ModeDefault                // 4 workers, 256 KiB, nice 5
	ModeDrone                  // GOMAXPROCS workers, 1 MiB, nice 0
	ModeHelicopter             // 4*GOMAXPROCS workers, 4 MiB, no caps
)

// Preset returns the options of a mode.
//
//   - Reports false for a mode above ModeHelicopter and returns zero WalkOptions. (Preset.1)
//   - Fills Workers, Buffer and Nice; leaves All at zero for the caller to set
//     from flags. (Preset.2)
//   - Workers and Buffer are always at least 1. (Preset.3)
func Preset(m Mode) (WalkOptions, bool) {
	procs := runtime.GOMAXPROCS(0)
	switch m {
	case ModeOwl:
		return WalkOptions{Workers: 1, Buffer: 64 << 10, Nice: 10}, true
	case ModeDefault:
		return WalkOptions{Workers: 4, Buffer: 256 << 10, Nice: 5}, true
	case ModeDrone:
		return WalkOptions{Workers: procs, Buffer: 1 << 20, Nice: 0}, true
	case ModeHelicopter:
		return WalkOptions{Workers: 4 * procs, Buffer: 4 << 20, Nice: 0}, true
	}
	return WalkOptions{}, false
}

// WalkOptions are the parameters of one walk. The zero value walks one directory
// with one worker.
// Skip holds absolute paths of entries to leave out: a skipped directory is
// never listed and its subtree never counts, and a skipped file counts for
// nothing. The composition skips the store's directory when it lies inside the
// root, so a run never reports the files it writes (I10).
type WalkOptions struct {
	All     bool
	Skip    []string
	Workers int
	Buffer  int
	Nice    int
}

// Stop names why a walk ended before it finished.
type Stop uint8

const (
	StopNone     Stop = iota // the walk finished
	StopDeadline             // the run's deadline cut the walk
)

// Witness proves a fact about a file or a directory.
type Witness struct {
	Dev, Ino    uint64
	Size        int64
	Mod, Change time.Time
}

// Child is one named child of a directory, kept only for the six largest.
// Lines is -1 when the run has not measured it.
type Child struct {
	Name  string
	Dir   bool
	Bytes int64
	Files int
	Lines int64
}

// Dir is one directory the walk read. Path is slash-separated, relative to the
// root, and "" is the root itself. Files and Bytes are subtree totals; a total
// is a lower bound when its subtree intersects Snapshot.Frontier.
type Dir struct {
	Path    string
	Files   int
	Bytes   int64
	Kids    []Child
	Witness Witness
}

// Snapshot is one walk's fact set. Frontier contains slash-separated relative
// directory paths whose enumeration is unfinished: a subtree a deadline left
// open, one the walk was refused, or one another filesystem holds; "" means the
// root is unfinished. Types counts the proved files by their final extension,
// lowercased with its dot, and "" for a name without one; the counts sum to
// Files. Unreadable counts regular files whose permission was refused,
// UnreadableDirs counts the directories whose listing was refused, and
// OtherFilesystems counts the directories the walk left because another
// filesystem is mounted there. Filesystem is the usage of the device that
// holds the root, which the composition fills when it can read it; the walk
// leaves it nil.
type Snapshot struct {
	Root             string
	Dirs             []Dir
	Frontier         []string
	Types            map[string]int
	Files            int
	Bytes            int64
	Unreadable       int
	UnreadableDirs   int
	OtherFilesystems int
	Filesystem       *Filesystem
	Stopped          Stop
}

// Sink receives monotone progress from a walk or a count (README D29).
type Sink interface {
	// Publish records one observation.
	//
	//   - Files, Bytes, Lines and Dirs are cumulative counters; each is greater
	//     than or equal to its value in the previous observation of the same
	//     phase. (Publish.1)
	//   - A walk publishes after each 4096 files per worker with PhaseWalk, and
	//     cumulative Files, Bytes and Dirs counters; Bytes is the byte total of
	//     the files already counted, and Lines is zero during the walk. Dirs
	//     counts completed directories. (Publish.2, D29)
	//   - Publish never blocks: a sink that cannot keep up drops the update. (Publish.3)
	Publish(p Progress)
}

// Progress is one cumulative observation of work done. Files counts completed
// files, Bytes counts their bytes, Lines counts measured lines, and Dirs counts
// completed directories.
type Progress struct {
	Phase Phase
	Files int64
	Bytes int64
	Lines int64
	Dirs  int64
}

// Phase names the stage that produced a Progress.
type Phase uint8

const (
	PhaseWalk Phase = iota
	PhaseCount
)

// Walker enumerates a tree without reading file content.
type Walker interface {
	// Walk enumerates root and returns its fact set.
	//
	//   - Dirs is in DirSeq order: a depth-first flatten, root first, siblings
	//     by subtree bytes descending then name; the order is total. (Walk.1)
	//   - The result is a fact set with declared completeness; every reported
	//     directory is enumerated, and Frontier identifies each unfinished
	//     subtree after a deadline. (Walk.2, I1, I9, I18)
	//   - Only readdir and lstat are used; no regular file is opened. (Walk.3, I16)
	//   - The fact set is independent of Workers and of interleaving; a tie
	//     inside a directory breaks by name. (Walk.4, I17)
	//   - Kids holds at most the six largest children by bytes, ties by name,
	//     and no per-file value survives the walk. (Walk.5, I21)
	//   - context.DeadlineExceeded returns the facts held, Stopped set, and a
	//     nil error. context.Canceled returns a wrapped error and no snapshot.
	//     (Walk.6, I11)
	//   - A directory that cannot be read, a child that cannot be stat'ed, or a
	//     root that is not a directory returns a wrapped error and no snapshot.
	//     A directory below the root whose listing is refused for permission is
	//     not a failure: it stays open in Frontier, is counted in
	//     UnreadableDirs, and the walk continues. Permission refusal for a
	//     regular file keeps its file and byte facts and increments Unreadable;
	//     no other error is classified as permission. (Walk.7, I24)
	//   - Every goroutine it started has exited before it returns. (Walk.8, I19)
	//   - A root named through a symlink is resolved before the walk starts, and
	//     hidden entries are skipped unless All is true; only regular files
	//     count, and no symlink below the root is followed. A directory whose
	//     device is not the root's is not entered: another filesystem is mounted
	//     there, so it is left open in Frontier, counted in OtherFilesystems, and
	//     the walk continues. (Walk.9, I8)
	//   - On failure, the first observed failure cancels remaining work and no
	//     snapshot is returned. (Walk.10, I20)
	Walk(ctx context.Context, root string, opts WalkOptions, sink Sink) (Snapshot, error)
}

// New returns the walker.
func New() Walker {
	return walker{}
}

type walker struct{}

func (walker) Walk(ctx context.Context, root string, opts WalkOptions, sink Sink) (Snapshot, error) {
	if opts.Workers < 1 {
		opts.Workers = 1
	}
	resolved, err := resolveRoot(root)
	if err != nil {
		return Snapshot{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Snapshot{}, fmt.Errorf("scan: stat root: %w", err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("scan: root is not a directory: %s", root)
	}
	return newWalk(resolved, WitnessOf(info).Dev, opts, sink).run(ctx)
}

// newWalk assembles one walk. dev is the root's device: the walk never enters a
// directory on another device, because another filesystem is mounted there and
// its entries are not part of the tree it reports (architecture/scan.md).
func newWalk(root string, dev uint64, opts WalkOptions, sink Sink) *walk {
	return &walk{
		opts:     opts,
		sink:     sink,
		root:     root,
		dev:      dev,
		skips:    skipSet(opts.Skip),
		rootNode: &dirNode{path: "", name: ""},
	}
}

// run walks the tree and returns its fact set.
func (w *walk) run(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	defer cancel()
	if err := w.walk(ctx); err != nil {
		return Snapshot{}, err
	}
	stopped := StopNone
	if !w.rootNode.done {
		stopped = StopDeadline
	}
	return w.snapshot(stopped), nil
}

func resolveRoot(root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("scan: stat root: %w", err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("scan: stat root: %w", err)
	}
	return abs, nil
}

type dirNode struct {
	path    string
	name    string
	parent  *dirNode
	witness Witness
	listed  bool
	done    bool
	pending int
	files   int
	bytes   int64
	unread  int
	fileK   []Child
	ext     map[string]int
	dirs    []*dirNode
}

type walk struct {
	opts     WalkOptions
	sink     Sink
	root     string
	dev      uint64
	skips    map[string]bool
	rootNode *dirNode
	cancel   context.CancelFunc

	aggMu sync.Mutex

	refMu   sync.Mutex
	refused []string

	fsMu    sync.Mutex
	foreign []string

	errMu sync.Mutex
	err   error

	files atomic.Int64
	bytes atomic.Int64
	dirs  atomic.Int64
	pubMu sync.Mutex
}

func (w *walk) walk(ctx context.Context) error {
	level := []*dirNode{w.rootNode}
	for len(level) > 0 {
		if ctx.Err() != nil || w.failure() != nil {
			break
		}
		level = w.band(ctx, level)
	}
	if err := w.failure(); err != nil {
		return err
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("scan: walk: %w", context.Canceled)
	}
	return nil
}

func (w *walk) failure() error {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.err
}

func (w *walk) fail(err error) {
	w.errMu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.errMu.Unlock()
	w.cancel()
}

func (w *walk) band(ctx context.Context, level []*dirNode) []*dirNode {
	var mu sync.Mutex
	var next []*dirNode
	var index atomic.Int64
	var wg sync.WaitGroup
	workers := min(w.opts.Workers, len(level))
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			var local []*dirNode
			for {
				i := int(index.Add(1)) - 1
				if i >= len(level) || ctx.Err() != nil {
					break
				}
				node := level[i]
				if err := w.list(ctx, node, &local); err != nil {
					w.fail(err)
					break
				}
			}
			if len(local) > 0 {
				mu.Lock()
				next = append(next, local...)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return next
}

func (w *walk) list(ctx context.Context, node *dirNode, next *[]*dirNode) error {
	path := w.abs(node.path)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && node.path != "" {
			w.abandon(node)
			return nil
		}
		return fmt.Errorf("scan: stat %s: %w", path, err)
	}
	node.witness = WitnessOf(info)
	if node.path != "" && w.dev != 0 && node.witness.Dev != w.dev {
		w.elsewhere(node)
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) && node.path != "" {
			w.refuse(node)
			return nil
		}
		return fmt.Errorf("scan: read %s: %w", path, err)
	}
	node.listed = true
	var pendingFiles, pendingBytes int64
	for _, entry := range entries {
		name := entry.Name()
		if !w.opts.All && strings.HasPrefix(name, ".") {
			continue
		}
		if w.skipped(joinPath(node.path, name)) {
			continue
		}
		if entry.IsDir() {
			child := &dirNode{path: joinPath(node.path, name), name: name, parent: node}
			node.dirs = append(node.dirs, child)
			node.pending++
			*next = append(*next, child)
			continue
		}
		childPath := w.abs(joinPath(node.path, name))
		entryInfo, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrPermission) && entry.Type().IsRegular() {
				node.count(name)
				node.unread++
				pendingFiles++
				continue
			}
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("scan: stat %s: %w", childPath, err)
		}
		if !entryInfo.Mode().IsRegular() {
			continue
		}
		node.count(name)
		node.bytes += entryInfo.Size()
		node.fileK = keepLargest(node.fileK, Child{Name: name, Bytes: entryInfo.Size(), Lines: -1})
		pendingFiles++
		pendingBytes += entryInfo.Size()
		if pendingFiles%progressStride == 0 {
			w.add(pendingFiles, pendingBytes)
			pendingFiles, pendingBytes = 0, 0
			w.publish()
		}
	}
	w.add(pendingFiles, pendingBytes)
	w.finish(node)
	w.publish()
	return nil
}

// refuse records a directory whose listing was refused: the subtree stays open
// in the frontier, is counted, and holds no fact (architecture/scan.md).
func (w *walk) refuse(node *dirNode) {
	w.refMu.Lock()
	w.refused = append(w.refused, node.path)
	w.refMu.Unlock()
	w.abandon(node)
}

// elsewhere records a directory the walk did not enter because another
// filesystem is mounted there: its subtree stays open in the frontier, is
// counted, and holds no fact (architecture/scan.md).
func (w *walk) elsewhere(node *dirNode) {
	w.fsMu.Lock()
	w.foreign = append(w.foreign, node.path)
	w.fsMu.Unlock()
	w.abandon(node)
}

// abandon completes a directory the walk proved nothing about, so its ancestors
// fold as if it were empty and the facts the walk did prove still reach the
// root. A directory that vanished before the walk listed it is abandoned, and a
// refused one is abandoned and declared (architecture/scan.md).
func (w *walk) abandon(node *dirNode) {
	w.aggMu.Lock()
	node.done = true
	parent := node.parent
	if parent != nil {
		parent.pending--
	}
	w.aggMu.Unlock()
	w.finish(parent)
}

func (w *walk) add(files, bytes int64) {
	if files != 0 {
		w.files.Add(files)
	}
	if bytes != 0 {
		w.bytes.Add(bytes)
	}
}

// count records one proved file and its type in the node's own totals.
func (node *dirNode) count(name string) {
	node.files++
	if node.ext == nil {
		node.ext = map[string]int{}
	}
	node.ext[extOf(name)]++
}

// merge folds a finished child's type census into its parent's.
func (node *dirNode) merge(child *dirNode) {
	if len(child.ext) == 0 {
		return
	}
	if node.ext == nil {
		node.ext = map[string]int{}
	}
	for ext, files := range child.ext {
		node.ext[ext] += files
	}
}

// extOf returns the final extension of a base name, lowercased with its dot,
// and "" when the name has none: a leading dot alone is not an extension, and
// a trailing dot names nothing.
func extOf(name string) string {
	for i := len(name) - 1; i > 0; i-- {
		if name[i] == '/' {
			return ""
		}
		if name[i] == '.' && i < len(name)-1 {
			return strings.ToLower(name[i:])
		}
	}
	return ""
}

func (w *walk) publish() {
	if w.sink == nil {
		return
	}
	w.pubMu.Lock()
	defer w.pubMu.Unlock()
	w.sink.Publish(Progress{
		Phase: PhaseWalk,
		Files: w.files.Load(),
		Bytes: w.bytes.Load(),
		Dirs:  w.dirs.Load(),
	})
}

func (w *walk) finish(node *dirNode) {
	w.aggMu.Lock()
	defer w.aggMu.Unlock()
	for node != nil && node.listed && !node.done && node.pending == 0 {
		node.done = true
		w.dirs.Add(1)
		parent := node.parent
		if parent != nil {
			parent.files += node.files
			parent.bytes += node.bytes
			parent.unread += node.unread
			parent.merge(node)
			parent.pending--
		}
		node = parent
	}
}

func (w *walk) abs(rel string) string {
	if rel == "" {
		return w.root
	}
	return filepath.Join(w.root, filepath.FromSlash(rel))
}

// skipped reports whether the entry at rel is one the composition excluded.
func (w *walk) skipped(rel string) bool {
	return len(w.skips) > 0 && w.skips[w.abs(rel)]
}

func skipSet(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path != "" {
			set[filepath.Clean(path)] = true
		}
	}
	return set
}

func (w *walk) snapshot(stopped Stop) Snapshot {
	frontier := w.frontier()
	w.refMu.Lock()
	frontier = append(frontier, w.refused...)
	unreadableDirs := len(w.refused)
	w.refMu.Unlock()
	w.fsMu.Lock()
	frontier = append(frontier, w.foreign...)
	otherFilesystems := len(w.foreign)
	w.fsMu.Unlock()
	slices.Sort(frontier)
	snapshot := Snapshot{
		Root:             w.root,
		Frontier:         frontier,
		UnreadableDirs:   unreadableDirs,
		OtherFilesystems: otherFilesystems,
		Stopped:          stopped,
	}
	if w.rootNode.listed {
		snapshot.Files = w.rootNode.files
		snapshot.Bytes = w.rootNode.bytes
		snapshot.Unreadable = w.rootNode.unread
		snapshot.Types = w.rootNode.ext
	}
	w.flatten(w.rootNode, &snapshot.Dirs)
	return snapshot
}

func (w *walk) frontier() []string {
	var out []string
	var visit func(node *dirNode)
	visit = func(node *dirNode) {
		if node.done {
			return
		}
		if !node.listed {
			out = append(out, node.path)
			return
		}
		if node.pending == 0 {
			return
		}
		for _, child := range node.dirs {
			visit(child)
		}
	}
	visit(w.rootNode)
	slices.Sort(out)
	return out
}

func (w *walk) flatten(node *dirNode, out *[]Dir) {
	if !node.listed {
		return
	}
	dir := Dir{
		Path:    node.path,
		Files:   node.files,
		Bytes:   node.bytes,
		Witness: node.witness,
	}
	kids := slices.Clone(node.fileK)
	for _, child := range node.dirs {
		if !child.listed {
			continue
		}
		kids = append(kids, Child{
			Name:  child.name,
			Dir:   true,
			Bytes: child.bytes,
			Files: child.files,
			Lines: -1,
		})
	}
	dir.Kids = largest(kids, kidLimit)
	*out = append(*out, dir)
	children := slices.Clone(node.dirs)
	slices.SortFunc(children, func(a, b *dirNode) int {
		if a.bytes != b.bytes {
			return cmpDesc(a.bytes, b.bytes)
		}
		return strings.Compare(a.name, b.name)
	})
	for _, child := range children {
		w.flatten(child, out)
	}
}

const (
	progressStride = 4096
	kidLimit       = 6
)

func keepLargest(kids []Child, child Child) []Child {
	kids = append(kids, child)
	slices.SortFunc(kids, compareChild)
	if len(kids) > kidLimit {
		kids = kids[:kidLimit]
	}
	return kids
}

func largest(kids []Child, limit int) []Child {
	slices.SortFunc(kids, compareChild)
	if len(kids) > limit {
		kids = kids[:limit]
	}
	return kids
}

func compareChild(a, b Child) int {
	if a.Bytes != b.Bytes {
		return cmpDesc(a.Bytes, b.Bytes)
	}
	return strings.Compare(a.Name, b.Name)
}

func cmpDesc(a, b int64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	default:
		return 0
	}
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}
