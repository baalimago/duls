// Package cache stores measured line counts in a sharded, path-keyed store. A
// cache problem is a miss: no call returns an error to the run, and a damaged
// shard costs a re-read and nothing else.
package cache

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/baalimago/duls/internal/measure"
	"github.com/baalimago/duls/internal/scan"
)

// Config is one store's parameters.
type Config struct {
	Dir     string
	Root    string
	Cap     int64
	Refresh time.Duration
	Now     func() time.Time
}

// Store is the on-disk measured-fact store.
type Store interface {
	measure.Measured

	// Flush writes what the mailbox still holds.
	//
	// - It returns once the mailbox is drained or the budget has passed.
	//   (Flush.1)
	// - The caller drops its error: a cache failure is a miss. (Flush.2, I14)
	// - Each shard is replaced atomically and a write merges with the facts
	//   already in the same shard; a writer killed mid-write leaves the
	//   previous shard or none. (Flush.3, D11)
	Flush(budget time.Duration) error

	// Close releases the store's writer.
	//
	// - It is safe after a failed Flush and safe to call twice. (Close.1)
	Close()
}

// Open opens the store.
//
//   - An empty Dir, or a Root inside Dir, returns a disabled Store and a nil
//     error: every call misses and writes nothing. A Dir inside Root keeps the
//     store open, and the caller skips that directory in its walk, so the run
//     never reports the files it writes. (Open.1, I10)
//   - Failure to create or use the store directory returns an error in the form
//     `cache: open <Dir>: <err>`; the caller treats it as cache-off. (Open.2, I14)
//   - No call on a Store reads or writes a path under Root unless Dir lies
//     inside Root, and the caller then leaves Dir out of its walk. (Open.3, I10)
//   - A damaged shard is removed and all its keys read as misses. (Open.4, I14)
//   - The cap is enforced by whole-shard eviction until the configured reserve
//     is reached. (Open.5, D11)
//   - Keys use the resolved absolute path and --all value. Content facts remain
//     reusable only within Refresh, retain their LF-rule line count, and persist
//     only the proven facts supplied by the counter. (Open.6, I1, I7, I8, I9, I25)
func Open(c Config) (Store, error) {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Cap <= 0 {
		c.Cap = defaultCap
	}
	s := &store{cfg: c, pending: make(map[string][]byte, mailboxLimit), rename: os.Rename}
	if c.Dir == "" || c.Refresh <= 0 {
		s.off = true
		return s, nil
	}
	dir, err := filepath.Abs(c.Dir)
	if err != nil {
		return nil, fmt.Errorf("cache: open %s: %w", c.Dir, err)
	}
	if c.Root != "" {
		root, err := filepath.Abs(c.Root)
		if err != nil {
			return nil, fmt.Errorf("cache: open %s: %w", c.Dir, err)
		}
		if within(root, dir) {
			s.off = true
			return s, nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cache: open %s: %w", c.Dir, err)
	}
	s.dir = dir
	return s, nil
}

const (
	shardCount   = 256
	mailboxLimit = 256
	settleWindow = 2 * time.Second
	reservePct   = 90
	defaultCap   = 32 << 20
	shardMagic   = "DULSHARD"
	shardVersion = 2
	headerSize   = 16
	indexRow     = 20
	shardPrefix  = "shard-"
	shardSuffix  = ".bin"
	kindFact     = 2
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

type store struct {
	cfg    Config
	dir    string
	off    bool
	rename func(oldpath, newpath string) error

	mu      sync.Mutex
	pending map[string][]byte
	closed  bool

	wmu sync.Mutex
}

// FileLines returns the line count stored for a file with this witness.
func (s *store) FileLines(path string, w scan.Witness) (int64, bool) {
	if s.off {
		return 0, false
	}
	payload, ok := s.lookup(factKey(resolve(path)))
	if !ok {
		return 0, false
	}
	fact, ok := decodeFact(payload)
	if !ok || !sameWitness(fact.witness, w) {
		return 0, false
	}
	if s.cfg.Now().Sub(fact.written) >= s.cfg.Refresh {
		return 0, false
	}
	return fact.lines, true
}

// StoreFact offers one proven fact to the store.
func (s *store) StoreFact(f measure.Fact) {
	if s.off || !settled(s.cfg.Now(), f.W) {
		return
	}
	f.Path = resolve(f.Path)
	s.offer(factKey(f.Path), encodeFact(f, s.cfg.Now()))
}

// Flush writes what the mailbox still holds.
func (s *store) Flush(budget time.Duration) error {
	if s.off {
		return nil
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	batches := s.drain()
	deadline := s.cfg.Now().Add(budget)
	for _, idx := range shardOrder(batches) {
		if !s.cfg.Now().Before(deadline) {
			return fmt.Errorf("cache: flush: %w", context.DeadlineExceeded)
		}
		if err := s.writeShard(idx, batches[idx]); err != nil {
			return err
		}
	}
	s.enforce()
	return nil
}

// Close releases the store's writer.
func (s *store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.pending = nil
}

func (s *store) offer(key, payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.pending) >= mailboxLimit {
		return
	}
	s.pending[string(key)] = payload
}

func (s *store) drain() map[uint32]map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	batches := make(map[uint32]map[string][]byte)
	for key, payload := range s.pending {
		idx := shardIndex([]byte(key))
		if batches[idx] == nil {
			batches[idx] = make(map[string][]byte)
		}
		batches[idx][key] = payload
	}
	s.pending = make(map[string][]byte, mailboxLimit)
	return batches
}

func (s *store) lookup(key []byte) ([]byte, bool) {
	s.mu.Lock()
	payload, ok := s.pending[string(key)]
	s.mu.Unlock()
	if ok {
		return payload, true
	}
	return s.readKey(key)
}

func (s *store) readKey(key []byte) ([]byte, bool) {
	idx := shardIndex(key)
	file, err := os.Open(s.shardPath(idx))
	if err != nil {
		return nil, false
	}
	defer file.Close()
	payload, found, damaged := searchShard(file, key)
	if damaged {
		s.drop(idx)
		return nil, false
	}
	return payload, found
}

func (s *store) shardPath(idx uint32) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s%03d%s", shardPrefix, idx, shardSuffix))
}

func (s *store) drop(idx uint32) {
	_ = os.Remove(s.shardPath(idx))
}

func (s *store) writeShard(idx uint32, add map[string][]byte) error {
	path := s.shardPath(idx)
	merged := make(map[string][]byte, len(add))
	existing, ok := loadShard(path)
	if !ok {
		return fmt.Errorf("cache: shard %s: damaged", path)
	}
	for _, item := range existing {
		merged[string(item.key)] = item.val
	}
	maps.Copy(merged, add)
	entries := make([]entry, 0, len(merged))
	for key, payload := range merged {
		entries = append(entries, entry{hash: keyHash([]byte(key)), key: []byte(key), val: payload})
	}
	temp, err := os.CreateTemp(s.dir, "tmp-")
	if err != nil {
		return fmt.Errorf("cache: write %s: %w", path, err)
	}
	name := temp.Name()
	if _, err := temp.Write(encodeShard(entries)); err != nil {
		_ = temp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("cache: write %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cache: write %s: %w", path, err)
	}
	if err := s.rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cache: write %s: %w", path, err)
	}
	return nil
}

func (s *store) enforce() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	type shardFile struct {
		name string
		size int64
		mod  time.Time
	}
	var files []shardFile
	var total int64
	for _, item := range entries {
		if item.IsDir() || !strings.HasPrefix(item.Name(), shardPrefix) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		files = append(files, shardFile{name: item.Name(), size: info.Size(), mod: info.ModTime()})
		total += info.Size()
	}
	if total <= s.cfg.Cap {
		return
	}
	target := s.cfg.Cap * reservePct / 100
	slices.SortFunc(files, func(a, b shardFile) int {
		if !a.mod.Equal(b.mod) {
			return a.mod.Compare(b.mod)
		}
		return strings.Compare(a.name, b.name)
	})
	for _, file := range files {
		if total <= target {
			return
		}
		if os.Remove(filepath.Join(s.dir, file.name)) == nil {
			total -= file.size
		}
	}
}

func searchShard(file *os.File, key []byte) (payload []byte, found, damaged bool) {
	head := make([]byte, headerSize)
	if _, err := file.ReadAt(head, 0); err != nil {
		return nil, false, true
	}
	if string(head[:len(shardMagic)]) != shardMagic || head[8] != shardVersion {
		return nil, false, true
	}
	count := int(binary.LittleEndian.Uint32(head[12:]))
	info, err := file.Stat()
	if err != nil {
		return nil, false, true
	}
	size := info.Size()
	if count < 0 || int64(headerSize)+int64(count)*indexRow > size {
		return nil, false, true
	}
	rows := make([]byte, count*indexRow)
	if count > 0 {
		if _, err := file.ReadAt(rows, headerSize); err != nil {
			return nil, false, true
		}
	}
	hash := keyHash(key)
	lo, hi := 0, count
	for lo < hi {
		mid := (lo + hi) / 2
		if binary.LittleEndian.Uint32(rows[mid*indexRow:]) < hash {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	for i := lo; i < count; i++ {
		row := rows[i*indexRow:]
		if binary.LittleEndian.Uint32(row) != hash {
			break
		}
		offset := int64(binary.LittleEndian.Uint32(row[8:]))
		length := int64(binary.LittleEndian.Uint32(row[12:]))
		if offset < headerSize || length < 0 || offset+length > size {
			return nil, false, true
		}
		body := make([]byte, length)
		if _, err := file.ReadAt(body, offset); err != nil {
			return nil, false, true
		}
		if crc32.Checksum(body, crcTable) != binary.LittleEndian.Uint32(row[16:]) {
			return nil, false, true
		}
		if sameKey(body, key) {
			return body, true, false
		}
	}
	return nil, false, false
}

type entry struct {
	hash uint32
	key  []byte
	val  []byte
}

func loadShard(path string) ([]entry, bool) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	return decodeShard(data)
}

func decodeShard(data []byte) ([]entry, bool) {
	if len(data) < headerSize || string(data[:len(shardMagic)]) != shardMagic || data[8] != shardVersion {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint32(data[12:]))
	if count < 0 || headerSize+count*indexRow > len(data) {
		return nil, false
	}
	entries := make([]entry, 0, count)
	for i := range count {
		row := data[headerSize+i*indexRow:]
		offset := int64(binary.LittleEndian.Uint32(row[8:]))
		length := int64(binary.LittleEndian.Uint32(row[12:]))
		if offset < headerSize || length < 0 || offset+length > int64(len(data)) {
			return nil, false
		}
		body := data[offset : offset+length]
		if crc32.Checksum(body, crcTable) != binary.LittleEndian.Uint32(row[16:]) {
			return nil, false
		}
		key, ok := keyOf(body)
		if !ok {
			return nil, false
		}
		entries = append(entries, entry{hash: binary.LittleEndian.Uint32(row), key: key, val: body})
	}
	return entries, true
}

func encodeShard(entries []entry) []byte {
	slices.SortFunc(entries, func(a, b entry) int {
		if a.hash != b.hash {
			if a.hash < b.hash {
				return -1
			}
			return 1
		}
		return bytes.Compare(a.key, b.key)
	})
	rows := make([]byte, len(entries)*indexRow)
	var body bytes.Buffer
	start := headerSize + len(rows)
	for i, item := range entries {
		row := rows[i*indexRow:]
		binary.LittleEndian.PutUint64(row, uint64(item.hash))
		binary.LittleEndian.PutUint32(row[8:], uint32(start+body.Len()))
		binary.LittleEndian.PutUint32(row[12:], uint32(len(item.val)))
		binary.LittleEndian.PutUint32(row[16:], crc32.Checksum(item.val, crcTable))
		body.Write(item.val)
	}
	head := make([]byte, headerSize)
	copy(head, shardMagic)
	head[8] = shardVersion
	binary.LittleEndian.PutUint32(head[12:], uint32(len(entries)))
	out := make([]byte, 0, start+body.Len())
	out = append(out, head...)
	out = append(out, rows...)
	return append(out, body.Bytes()...)
}

func encodeFact(f measure.Fact, written time.Time) []byte {
	var w writer
	w.byte(kindFact)
	w.string(f.Path)
	w.uvarint(uint64(f.Lines))
	w.witness(f.W)
	w.uvarint(uint64(written.UnixNano()))
	return w.buf
}

type fact struct {
	lines   int64
	witness scan.Witness
	written time.Time
}

func decodeFact(payload []byte) (fact, bool) {
	r := reader{buf: payload}
	if r.byte() != kindFact {
		return fact{}, false
	}
	r.string()
	out := fact{lines: int64(r.uvarint())}
	out.witness = r.witness()
	out.written = nanos(r.uvarint())
	return out, r.err == nil
}

// keyOf rebuilds the key an encoded payload is stored under.
func keyOf(payload []byte) ([]byte, bool) {
	r := reader{buf: payload}
	if r.byte() != kindFact {
		return nil, false
	}
	path := r.string()
	if r.err != nil {
		return nil, false
	}
	return factKey(path), true
}

func sameKey(payload, key []byte) bool {
	derived, ok := keyOf(payload)
	return ok && bytes.Equal(derived, key)
}

func factKey(path string) []byte {
	key := make([]byte, 0, len(path)+1)
	key = append(key, kindFact)
	return append(key, path...)
}

func keyHash(key []byte) uint32 {
	return crc32.Checksum(key, crcTable)
}

func shardIndex(key []byte) uint32 {
	return keyHash(key) % shardCount
}

func shardOrder(batches map[uint32]map[string][]byte) []uint32 {
	out := make([]uint32, 0, len(batches))
	for idx := range batches {
		out = append(out, idx)
	}
	slices.Sort(out)
	return out
}

func resolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return filepath.Clean(abs)
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// Inside reports whether child lies inside parent, or is parent itself.
func Inside(child, parent string) bool {
	return within(child, parent)
}

func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func settled(now time.Time, w scan.Witness) bool {
	newest := w.Mod
	if w.Change.After(newest) {
		newest = w.Change
	}
	return !newest.IsZero() && now.Sub(newest) >= settleWindow
}

func sameWitness(a, b scan.Witness) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Size == b.Size &&
		a.Mod.Equal(b.Mod) && a.Change.Equal(b.Change)
}

func nanos(value uint64) time.Time {
	return time.Unix(0, int64(value))
}

type writer struct{ buf []byte }

func (w *writer) byte(value byte) { w.buf = append(w.buf, value) }

func (w *writer) uvarint(value uint64) { w.buf = binary.AppendUvarint(w.buf, value) }

func (w *writer) string(value string) {
	w.buf = binary.AppendUvarint(w.buf, uint64(len(value)))
	w.buf = append(w.buf, value...)
}

func (w *writer) witness(wi scan.Witness) {
	w.uvarint(wi.Dev)
	w.uvarint(wi.Ino)
	w.uvarint(uint64(wi.Size))
	w.uvarint(uint64(wi.Mod.UnixNano()))
	w.uvarint(uint64(wi.Change.UnixNano()))
}

var errShortData = errors.New("cache: short data")

type reader struct {
	buf []byte
	err error
}

func (r *reader) byte() byte {
	if r.err != nil {
		return 0
	}
	if len(r.buf) < 1 {
		r.err = errShortData
		return 0
	}
	value := r.buf[0]
	r.buf = r.buf[1:]
	return value
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	value, n := binary.Uvarint(r.buf)
	if n <= 0 {
		r.err = errShortData
		return 0
	}
	r.buf = r.buf[n:]
	return value
}

func (r *reader) string() string {
	if r.err != nil {
		return ""
	}
	length := r.uvarint()
	if r.err != nil || length > uint64(len(r.buf)) {
		r.err = errShortData
		return ""
	}
	value := string(r.buf[:length])
	r.buf = r.buf[length:]
	return value
}

func (r *reader) witness() scan.Witness {
	var w scan.Witness
	w.Dev = r.uvarint()
	w.Ino = r.uvarint()
	w.Size = int64(r.uvarint())
	w.Mod = nanos(r.uvarint())
	w.Change = nanos(r.uvarint())
	return w
}
