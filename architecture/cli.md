# CLI

How a command line becomes one exit code: the flags, the positional path, the
streams, the environment, and the exit codes.

Read this when: adding a flag, changing help, changing what goes to stdout or
stderr, or changing an exit code.

## Key files

| File | Purpose |
|------|---------|
| `main.go` | `os.Exit(cli.Run(...))` over `os.Args`, `os.Stdout` and `os.Stderr`. |
| `internal/cli/cli.go` | `Run`, `withProcessEnv`, the `command` interface, `router.run`, `router.help`, `router.fail`. |
| `cmd/duls/duls.go` | `Command`, `Parse`, `parseCommandLine`, `parseFlagsAndPositionals`, `validate`, `excludeFlag`, `compileExcludes`, `readExcludeFile`, `Request`, `Run`, `helpText`. |
| `internal/scan/exclude.go` | `NewExclude`, `Exclude`, `PatternError`: the one compiler and validator of exclude patterns. |
| `internal/app/options.go` | `Options`, `Abs`, `Out`, `ErrOut`, `EnvLookup`, `cacheDir`. |
| `internal/app/run.go` | `Request`, `Run`, `openStore`: the policies the flags select. |

## Flow

```text
duls [flags] [path]
  |
  cli.Run:
    opts.Env == nil ? os.Environ() : keep
    router.run
      |
      command.Parse(rest)         the flags, then interleaved positionals
        parseCommandLine          split at "--", parse each flag run
        validate                  mode 0..3, refresh >= 0, timeout >= 0
        at most one positional    path, or the empty string
        compileExcludes           read each -X file (Options.Abs), then
                                  scan.NewExclude over every pattern in
                                  command-line order
      |
      command.Run(ctx)
        --version ?  stdout: "duls <version>\n"
        else         app.Run(ctx, opts, Request)
```

## Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-a`, `--all` | off | include hidden entries |
| `--all-types` | off | measure every regular file; disable the extension deny-list |
| `--bytes` | off | exact byte integers instead of SI units; visual form only |
| `--exclude p` | none | leave out every directory and regular file the glob `p` matches; repeatable |
| `-X`, `--exclude-from f` | none | read exclude patterns from file `f`, one per line; repeatable |
| `-json` | off | print the agent document as JSON, whatever the stream |
| `-m`, `--mode` | `1` | 0, 1, 2 or 3: workers and read buffer |
| `-r`, `--refresh` | `15m` | record trust window; `0` = no read and no write |
| `-t`, `--timeout` | `2s` | the run's deadline; `0` = none |
| `-h`, `--help` | - | print the help text on stdout, exit 0 |
| `-v`, `--version` | - | print `duls <version>`, exit 0 |

The modes are constants, not heuristics. `0` is 1 worker and a 64 KiB buffer;
`1` is 4 workers and 256 KiB; `2` is `GOMAXPROCS` workers and 1 MiB; `3` is
`4*GOMAXPROCS` workers and 4 MiB. The worker count changes the speed, never
the answer.

## Behavior

**One command, one optional path.** `duls` with no path inspects the working
directory. `duls <path>` inspects that directory. duls has no subcommands: a
positional is always a path, so `duls help` looks for a directory named
`help`.

**Path resolution.** A relative path resolves against the working directory
(`Options.Cwd`, or the process's when unset). An empty path means `.`. The
resolved directory is cleaned and its symlinks are resolved before the walk
starts, so a root named through a link is scanned as its target.

**Flags after the path.** The Go flag package stops at the first positional.
`parseFlagsAndPositionals` therefore parses the remaining arguments again
after each positional, so `duls notes -a` and `duls -a notes` mean the same
thing.

**The `--` terminator.** Everything after `--` is a positional, so
`duls -- -a` inspects a directory named `-a`.

**Exclude patterns.** `--exclude` and `-X` follow ncdu 1.x, whose
`exclude_match` calls glibc `fnmatch(pattern, path, 0)`. A pattern is tested
against the entry's absolute path (slash form, under the resolved root) and
against every suffix of it that begins after a `/`; any match leaves the entry
out. `*` and `?` match `/` too, as does a bracket. A bracket negates with `!` or
`^`, takes a leading `]` and a leading or trailing `-` literally, compares
ranges by rune, and knows the POSIX classes (`[:alpha:]`, `[:digit:]`, and the
rest). It is read the way glibc reads it: members up to the first hit, then
glibc's skip to the closing `]`, which steps over an escape or a `[:class:]`
whole; after a `-`, a `[` is a literal range end. `\x` matches `x`. A `[` with
no closing `]` matches a literal `[`. Matching is per rune, and per byte when
the pattern or the path is not valid UTF-8, as glibc falls back. So
`node_modules` leaves out every entry of that name, `*.log` every path that
ends in `.log`, `src/gen` every path that ends in `src/gen`, and
`/home/me/proj/build` that one path. Patterns apply in both phases, after the
hidden rule; an excluded directory is never listed, an excluded file is never
stat'ed or counted, and the run counts each excluded entry once.

`-X FILE` reads one pattern per line, as ncdu does: trailing CRs are dropped,
empty lines are skipped, and whitespace is part of the pattern. A relative
FILE resolves against the working directory, like the path. Patterns from the
flags and the files keep command-line order, and the document lists them in
that order.

`Parse` refuses a pattern no cleaned path can match: an empty pattern, one that
ends in `/`, one with `//`, one with a `.` or `..` segment, one that ends in an
unescaped `\`, and one whose bracket runs off the end after a `-` or a `\`
(`[a-`). A bracket that holds only `/` or `.` counts as that literal, so `x[/]`
and `a/[.]/b` are refused too. Beyond those, duls refuses two things glibc
accepts: an unknown class name, which glibc silently fails on unless an
earlier member already matched, and a `[.` or `[=` inside a bracket, because
duls does not implement collating symbols or equivalence classes.

**Streams.** The answer goes to stdout: the visual view on a colour terminal,
otherwise the agent document. Help and the version line go to stdout. Every
failure goes to stderr as exactly one line, `error: <message>`; a newline
inside the message becomes a space. Nothing else writes to either stream.

**The form.** `-json` selects the agent document before anything else. Without
it, a pipe, a file, `/dev/null`, and a non-empty `NO_COLOR` select it too; only
a colour terminal gets the visual view. The form never changes the exit code.

**The environment.** duls reads `NO_COLOR` (any non-empty value selects the
agent document) and `COLORTERM` (`truecolor` or `24bit` selects the 24-bit
palette of the visual form). `XDG_CACHE_HOME` selects the store root, with
`$HOME/.cache` as the fallback; with neither set, or with an unusable path,
the run has no store and still succeeds. `Options.Env` is the process
environment in the released binary; tests inject their own.

**Exit codes.** 0 for a printed view, including a declared partial view, and
for help and version. 1 for a flag error, more than one path, a missing or
non-directory path, a scan failure, a cancelled context, and a write failure.
duls installs no signal handler: a signal ends the process with the shell's
usual status.

## Gotchas

- The flag set writes to `io.Discard`; the router formats diagnostics, not the
  flag package.
- `flag.ErrHelp` is the help signal, not a failure. Both `Parse` and the
  router check it, and both print the help text and return 0.
- `Parse` adds the `run 'duls -h' for its flags` hint to a flag-package
  failure and to an exclude failure (a refused pattern, an unreadable `-X`
  file). A `validate` range error and a path-count error carry none.
- A refused pattern from an `-X` file names its origin: `flag: FILE:LINE: scan:
  exclude pattern "src/" can never match a path; drop the trailing '/'`.
- The full path includes the root's own name, so `duls ~/build --exclude
  'build/*'` leaves out everything below the root. That is ncdu 1.x behaviour.
- Because `*` crosses `/`, `a*z` leaves out `ab/cz` although `ab` does not
  match.
- An absolute pattern is matched against the resolved root: `duls ~/link
  --exclude /home/me/link/build` matches nothing when `link` is a symlink.
- Matching is per rune for valid UTF-8. glibc also matches some valid
  multibyte names per byte (it matches `é` with both `?` and `??`); duls does
  not reproduce that. Invalid UTF-8 is matched per byte in both.
- Classes use Go's `unicode` tables above U+007F, not glibc's locale tables:
  `[[:space:]]` matches U+00A0 and `[[:alpha:]]` misses U+0663, the reverse of
  glibc. ASCII agrees for every class. In byte mode a class matches ASCII only.
- A pattern that starts with `?`, `*` or a bracket can match the whole path,
  which starts with `/`: `[!.]*` leaves out everything, because `/` is not `.`
  and `*` crosses `/`. Anchor it to a name with a leading literal, as in
  `.[!g]*`.
- The document carries the patterns as JSON strings, so a pattern with invalid
  UTF-8 prints its bad bytes as U+FFFD in `accounting.exclude`.
- An exclusion is the user's choice, not a gap: it never lowers a total to a
  bound and never adds the device line.
- A negative `-r` and a negative `-t` are flag errors. `-r 0` is valid and
  means no cache. `-t 0` is valid and lifts the large-file cap.
- `-json` and `-bytes` do not interact: a document carries exact integers
  already, so `-bytes` changes only the visual form.
- A custom flag set with its default output would print usage to stderr behind
  the router's back. Keep the output discarded.

## Related

- [errors.md](./errors.md): wrapping and the one error line.
- [view.md](./view.md) and [agent.md](./agent.md): what stdout carries.
- [tui.md](./tui.md): how the stream decides the form and the width.
- [cache.md](./cache.md): `-r` and `XDG_CACHE_HOME`.
