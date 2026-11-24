# Role

How duls decides the part a file plays in a tree (source, test, docs, config,
generated, vendored or other) from its path alone, and how the walk, the count,
the planner, the view and the document carry it.

Read this when: changing which files count as tests, docs or any other role,
adding a role, or changing the role line or the `roles` key.

## Key files

| File | Purpose |
|------|---------|
| `internal/role/role.go` | `Role`, `Count`, `Tally`, `String`, `Of`, `Ext`, and the rule tables `extensions`, `filenames`, `lockfiles`, `licenseWords`, `docsWords`. |
| `internal/scan/walk.go` | `gathered.census` and `walk.roles`: the files per role, `Snapshot.Roles`; the type census uses `role.Ext`. |
| `internal/measure/measure.go` | `job.role`, `state.prove`: the measured files, lines and skipped files per role, `Result.Roles`. |
| `internal/plan/plan.go` | `RoleShare`, `roleShares`: `Totals.Roles`. |
| `internal/view/view.go` | `rolesLine`: the `lines` line. |
| `internal/agent/agent.go` | `Role`, `roles`: the `roles` key. |

## Flow

```text
role.Of(rel)               rel is slash-separated and relative to the root
  |
  +-- scan.Walk            every counted file, refused size included:
  |                        Snapshot.Roles[role]++     (sums to Files)
  +-- measure.Count        every proven file, read or reused from the store,
  |                        adds {1, lines}; every skipped file adds Skipped
  v                        (sums to Measured, Lines and Skipped)
plan.Plan                  Totals.Roles: files from the walk or the count,
  |                        lines from the count, share of Totals.Lines
  +-- view.Lines           "lines  40% source · 30% test · …"
  +-- agent.Encode         "roles":[{"role","files","lines","share"}]
```

## The rules

`Of` reads only the path below the root, never content, so phase 1 can apply
it without opening a file, and the root's own name never decides a role. The
first rule that matches decides:

1. **Vendored.** A directory component `vendor`, `node_modules`,
   `third_party`, `third-party` or `bower_components`, or a name ending in
   `.min.js`, `-min.js`, `.min.css` or `-min.css`.
2. **Generated.** A lockfile by exact name (`go.sum`, `package-lock.json`,
   `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`, `poetry.lock`, `uv.lock`,
   `flake.lock`, and the rest of `lockfiles`), a `.lock` or `.map` extension,
   a name starting with `zz_generated`, or a stem ending in `.pb`, `_pb`,
   `_pb2`, `_pb2_grpc`, `_gen`, `.gen`, `_generated`, `.generated`, `.g` or
   `.freezed`.
3. **Test.** A directory component `test`, `tests`, `Test`, `Tests`,
   `__tests__`, `spec`, `specs`, `testdata`, `__mocks__` or `__snapshots__`;
   or a source file (rule 6) whose stem ends in `_test`, `_tests`, `_spec`,
   `_unittest`, `.test`, `.spec`, `Test`, `Tests` or `Spec`; or a `.py` file
   named `test_*` or `conftest.py`. A test directory outranks a docs
   directory wherever each appears in the path.
4. **Docs directory or licence name.** A directory component `doc`, `docs`,
   `Doc`, `Docs`, `documentation` or `Documentation`; or a name that is
   `README`, `LICENSE`, `LICENCE` or `COPYING` in upper case, alone or
   followed by `.`, `-` or `_`, whatever its extension: `README.html` and
   `LICENSE-MIT` are docs, `license.go` is not.
5. **Known filename.** `Makefile`, `Dockerfile`, `Dockerfile.*`,
   `CMakeLists.txt`, `requirements*.txt`, `go.mod`, `Gemfile` and the rest of
   `filenames` are config.
6. **Final extension** (`Ext`: lowercased with its dot; a leading or trailing
   dot alone names none): a programming, query, shader, markup or style
   language is source; `.bats` is test; prose and plain-text document formats
   (`.md`, `.rst`, `.txt`, …) are docs; settings and build files (`.yaml`,
   `.toml`, `.json`, `.xml`, `.mk`, …) are config. Binary document formats
   such as `.pdf` and `.docx` are not in the table: they are other, and the
   count's deny-list skips them ([measure.md](./measure.md)). The table is
   `extensions`.
7. **Docs word.** A name that is a docs word, matched without case, alone or
   followed by `.`, `-` or `_`: `readme`, `CHANGELOG`, `AUTHORS`, `INSTALL`,
   and the rest of `docsWords`. A known extension decides first, so
   `install.sh` is source.
8. **Dotfile.** Any other name starting with `.` is config.
9. Everything else is **other**.

## Behavior

**The census.** The walk counts every file it counts in `Files` by role,
including a file whose size was refused, so `Snapshot.Roles` sums to
`Snapshot.Files` on a finished walk and on a cut one.

**The tally.** The count adds each proven file, whether read or reused from
the store, to its role's `Tally` as one file and its lines, and each file the
policy skipped as one `Skipped`. The tallies sum to `Result.Measured`,
`Result.Lines` and `Result.Skipped`. An unknown file adds nothing, so a role
whose files were all skipped or unknown has no proven lines, not zero lines.
The store keeps no role: the role is a function of the path and is computed
again on every run.

**The plan.** `Totals.Roles` lists every role the census or the count saw.
`Files` is the larger of the census and the count's measured and skipped
files, so a file created between the phases never leaves its role at zero
files; on a tree that does not change during the run the files across the
roles sum to `Totals.Files`. The lines across the roles that have lines sum to
`Totals.Lines`. `Lines` is -1 and `Share` is 0 for a role with no proven file.
`Share` is the rounded percentage of `Totals.Lines`. `Skipped` carries the
policy's count. The order is proven lines descending, then files descending,
then role order.

**The line.** The answer prints `lines` and, in the plan's order, each role
with more than zero proven lines as its share and its name, and `? <role>` for
a role with a file the count neither proved nor skipped: a file it could not
read, or one a deadline left unreached. A role whose every file the policy
skipped is left out, and so is a role whose proven lines are zero. A share
below one percent prints `<1%`. Every share carries `~` when the line total is
not exact: it is then a share of a partial total, which can be high or low, as
a row's share is when its bytes are a lower bound. A provisional view, the
frame's, never prints the line, because a frame states no fact the run has
not proved. A view with no role to print has no role line.

**The document.** `roles` is an array of `{"role","files","lines","share"}` in
the plan's order. `lines` and `share` are null for a role with no proven file,
never zero. An exact `totals.linesExact` makes every `lines` exact too.

## Gotchas

- `Of` is on the walk's per-file path. Keep new rules to string comparisons
  and map lookups, never a regular expression.
- Rule order matters more than any single table. A lockfile is generated even
  under `tests/`, and anything under `vendor/` is vendored even when it is
  named `*_test.go`.
- A test name needs a source extension, so `api_test.yaml` is config and
  `contest.md` is docs. A test directory needs no extension: `testdata/a.md` is
  a test.
- A role can be wrong for a file whose meaning only its content shows: an
  extensionless script, a Rust unit test inside a source file, or a generated
  file without a generated name. duls does not read content to decide a role.

## Related

- [scan.md](./scan.md): the walk that keeps the census.
- [measure.md](./measure.md): the count that proves the lines.
- [plan.md](./plan.md): `Totals.Roles`.
- [view.md](./view.md): the role line.
- [agent.md](./agent.md): the `roles` key.
