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
| `cmd/duls/duls.go` | `Command`, `Parse`, `parseCommandLine`, `parseFlagsAndPositionals`, `validate`, `Request`, `Run`, `helpText`. |
| `internal/app/options.go` | `Options`, `Out`, `ErrOut`, `EnvLookup`, `cacheDir`. |
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
- `Parse` adds the `run 'duls -h' for its flags` hint to a flag failure and
  only to a flag failure; a path-count error carries no hint.
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
