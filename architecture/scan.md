# Scan

How duls enumerates a directory tree without reading file content: the
parallel breadth-first walk, the fact set it returns, the frontier that
declares a cut, and the progress it publishes.

Read this when: changing what the walk counts, how a walk is cut, what the
view's peek column can show, or how progress reaches the frame.

## Key files

| File | Purpose |
|------|---------|
| `internal/scan/walk.go` | `Mode`, `Preset`, `WalkOptions`, `Walk`, `Snapshot`, `Dir`, `Child`, `Witness`, `Stop`, `Sink`, `Progress`, `Phase`. |
| `internal/scan/witness_linux.go` | `WitnessOf`: `dev`, `ino`, `size`, `mtime` and `ctime` from a `Stat_t`. |
| `internal/scan/witness_other.go` | The portable fallback witness. |
| `internal/scan/filesystem.go` | `Filesystem`: the device's total, used and available bytes. |
| `internal/scan/filesystem_linux.go` | `FilesystemOf`: `statfs` on the root. |
| `internal/scan/filesystem_other.go` | The fallback that reports no device usage. |
| `internal/app/run.go` | The one caller: `Preset`, `Walk`, `FilesystemOf`, and the sink it passes. |

## Flow

```text
Walk(ctx, root, opts, sink)
  resolveRoot              a root named through a symlink is resolved
  os.Stat                  the root must be a directory
  fan-out pool             opts.Workers goroutines, one listing each
    readdir                entries in name order
    lstat / Entry.Info     bytes and witness per regular file
    fold                   totals into the directory and its ancestors
    publish                every 4096 files per worker, through the sink
  |
  Snapshot{Root, Dirs, Frontier, Types, Files, Bytes, Unreadable, UnreadableDirs, OtherFilesystems, Filesystem, Stopped}
```

## The fact set

`Snapshot.Dirs` holds every directory the walk read, in DirSeq order: a
depth-first flatten, root first (`Path == ""`), siblings by subtree bytes
descending then name. The order is total, so two walks of one tree return
equal snapshots at any worker count.

`Dir.Files` and `Dir.Bytes` are subtree totals. A total is a lower bound when
its subtree intersects `Snapshot.Frontier`. `(*walk).record` folds each counted
batch into its directory and every ancestor as the walk runs, so a node's
`Files` and `Bytes` are the subtree total the walk holds right now, including
the directories it has not finished. A cut therefore keeps what it counted, and
`Snapshot.Files` and `Snapshot.Bytes` never fall below the work the walk
reported through its sink.

`Dir.Kids` holds at most the six largest children by bytes, ties by name. It
is the only per-child value that survives the walk, because it is what the
view's peek column and the count's dispatch need.

`Snapshot.Unreadable` counts regular files whose size the walk could not read.
They count in `Files` and in the type census and hold no byte fact, so they
never enter `Kids`: a child states its own bytes. `Snapshot.UnreadableDirs`
counts the directories whose listing was refused.

A refused directory is not read, so it holds no fact and never appears in
`Dirs` or `Kids`; its path is a frontier path, so every ancestor's totals are
lower bounds. A directory that vanished before its listing, and an entry whose
`lstat` reports that it is gone, is abandoned without a fact and without a
count: the tree no longer holds it.

A directory on another device than the root is not read either: a filesystem
is mounted there, so its entries belong to that filesystem and not to the tree
the root reports. It stays open in `Frontier`, is counted in
`Snapshot.OtherFilesystems`, and contributes no fact, so its ancestors' totals
are lower bounds. The rule is relative to the root: `duls /proc` walks procfs
because that is the root's own device, while `duls /` leaves the mounted `/proc`
unentered. A platform whose witness reports no device has no device to compare
and crosses.

`Snapshot.Filesystem` is the usage of the device that holds the root: total,
used and available bytes from `FilesystemOf`. The walk leaves it nil; the
composition fills it after the walk. It counts every file on the device,
including the subtrees the walk could not reach, so it is the quantity `df`
reports and the run's totals are a part of it. `FilesystemOf` asks the kernel
about the path itself, so it needs no read permission on the directory and
reports a device whose tree is refused. A platform that cannot report it leaves
the fact out: a run never fails because the device is unknown.

`Snapshot.Types` counts the proved files by their final extension: the last dot
of the base name and the characters after it, lowercased with the dot, and `""`
for a name without one. A leading dot alone is not an extension, and a trailing
dot names none. Directories and hidden entries never enter the census, so the
counts sum to `Snapshot.Files`. The walk keeps one census for the whole run
(`walk.census`), fed by the same batch as the totals, so the counts are the
files the walk counted and never name a file it did not reach, whether the walk
finished or a deadline cut it.

## Bounds and ordering

The walk is breadth-first by depth. Unfinished work at any instant is one
depth band plus the directories its workers hold. A directory is open from the
moment its listing starts until `finish` closes it. `Frontier` names an unlisted
directory, and a listed directory that has no child directory outstanding and is
still reading its own entries; once a listed directory holds an unfinished child
directory, that child's own path stands for it. A path is a lower bound when
`Frontier` holds it or a path below it, so an ancestor of an open subtree is a
lower bound without being named.

The queue holds directory paths only. A worker holds one listing and one
buffer, and a file's facts fold into its directory as it is read.

## The two stops

Only two events end the walk before it finishes.

`context.DeadlineExceeded` returns the facts held, `Stopped: StopDeadline`,
and a nil error. `Snapshot.Frontier` then names every open subtree, and the
view declares a partial view. The files the walk counted inside an open
subtree are held too: they fold into their ancestors as lower bounds, so the
answer states the work the run did and not less.

`context.Canceled` returns a wrapped error and no snapshot.

A refusal does not stop the walk: the refused subtree is abandoned, the walk
continues, and `Stopped` stays `StopNone` while its path stays in `Frontier`.
A run can therefore declare a lower bound without naming a stopped phase.

Every goroutine the walk started has exited before it returns, on success, on
error, on deadline, and on cancel. A syscall blocked in the kernel is outside
the tool's control.

## Policies

Only `readdir` and `lstat` are used. The walk never opens a regular file, so
phase 1 costs metadata only.

Hidden entries are skipped unless `WalkOptions.All` is set. A symlink below
the root is never followed. A directory on another device than the root's is
not entered, so a mounted filesystem never leaks its entries, its apparent
sizes or its network latency into the view. A special file is never counted.
Only regular files count.

A root that is not a directory, a root that cannot be read, a directory whose
`lstat` fails, or a child that cannot be stat'ed returns a wrapped error and no
snapshot. The first failure cancels the remaining work. Two cases are not
failures: a directory below the root whose listing is refused for permission is
abandoned and counted in `UnreadableDirs`, and an entry that is gone by the time
the walk reaches it is skipped. No other error is classified as permission.

`WalkOptions.Skip` holds absolute paths the walk leaves out. A skipped
directory is not listed and its subtree never counts; a skipped file counts for
nothing. The composition names the store's directory when it lies inside the
root, so a run never reports the files it writes (I10).

## Progress

`Sink.Publish` receives one `Progress` per 4096 files per worker and after each
listing, and never blocks: a sink that cannot keep up drops the update. Counters
are cumulative within a phase. `PhaseWalk` progress carries files, bytes and
completed directories, a zero line count, and `Tree`: a function that returns
the fact set the walk holds at that moment, declared `StopDeadline` because a
walk in progress proves nothing final. `Tree` takes the aggregate lock and
builds the snapshot, so only a sink that renders an observation pays for it.
`PhaseCount` progress comes from `internal/measure` and carries no tree, because
its facts are the walk's snapshot.

## Gotchas

- A snapshot is built under the aggregate lock and copies the census, so a
  sink renders the facts of one instant and never a map it shares with the walk.
- `Dir.Kids` is capped at six. A directory with more than six children cannot
  be fully named by the view, and the smallest children are the ones dropped.
  The count measures them all; only the peek is bounded.
- A witness is a fact about an inode. The walk records it, but the count takes
  its own witness at dispatch, because a file can change between the phases.
- The `Nice` value of a mode is carried, not applied: Go has no per-goroutine
  priority, so no call sets the process nice level. The field is the preset's
  declaration, and `-m` changes the worker count and the read buffer only.
- A frontier path of `""` means the root itself is unfinished.
- A frontier path does not imply a deadline: a refused directory is open while
  `Stopped` is `StopNone`.
- A refused directory is absent from `Dirs`, so the count never lists it again
  and its files never enter the line count.
- A directory on another filesystem is absent from `Dirs` too. Its name reaches
  no row and no peek column; only `Snapshot.OtherFilesystems` and the path in
  `Frontier` name it. That is what keeps a mount's apparent size out of the
  totals: procfs reports a file of 128 TiB that stores nothing.
- The device usage is allocated bytes, the quantity `df` prints, and the run's
  bytes are apparent size, so the two never add up exactly: block rounding,
  inode tables, journals and unlinked files that are still open all count for
  the device and for no row.
- `Skip` holds absolute paths and is matched against the cleaned absolute
  path of each entry. A skipped directory is absent from `Dirs`, from `Kids`,
  and from every total, so the skipped subtree reads as if it did not exist.

## Related

- [measure.md](./measure.md): phase 2, which reads content.
- [plan.md](./plan.md): how the fact set becomes rows.
- [frame.md](./frame.md): the sink a visual run passes.
