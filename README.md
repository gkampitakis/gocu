# gocu — Go Check Updates

`gocu` reports and applies upgrades to your Go module dependencies. Think
[`npm-check-updates`](https://github.com/raineorshine/npm-check-updates), but
for `go.mod`.

```
 github.com/spf13/cobra    v1.6.0   → v1.10.2  minor
 github.com/urfave/cli/v3  v3.3.0   → v3.9.0   minor
 golang.org/x/mod          v0.20.0  → v0.36.0  minor

3 upgrade(s) available. Run with -u to apply.
```

## Why not just `go list -m -u all`?

`go list -m -u all` works, but it's serial, dense, and has no way to filter,
group, preview, or guard against just-published versions. `gocu` adds:

- Parallel proxy lookups (default 8 in-flight) with an in-memory cache
- Target modes: `latest`, `minor`, `patch`, `newest` (by publish date), `greatest`
- Filter / reject by glob, `/regex/`, or comma-separated list
- Interactive TUI to pick exactly which upgrades to apply
- Supply-chain `--cooldown` to hide too-fresh releases
- Colored, sorted output with clickable module links (OSC 8)
- JSON output for scripts and CI
- A `--dry-run` that prints the `go get` commands without running them

## Install

```bash
go install github.com/gkampitakis/gocu/cmd/gocu@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

## Usage

```bash
gocu                                # report upgrades for ./go.mod
gocu -u                             # apply upgrades (runs `go get`, then `go mod tidy`)
gocu -i                             # interactive picker
gocu -t patch                       # only patch upgrades
gocu -t newest                      # most-recently-published, regardless of semver
gocu -f 'github.com/aws/**'         # only modules under github.com/aws
gocu -x '/internal/'                # exclude any module matching this regex
gocu --dep all                      # include indirect deps
gocu --cooldown 7d                  # skip versions published in the last 7 days
gocu --pre                          # include prereleases
gocu --allow-incompatible           # allow upgrades into v2+ +incompatible versions
gocu --json | jq .                  # machine-readable output
gocu --cwd /path/to/project         # operate elsewhere
gocu -u --dry-run                   # preview what `go get` would run
```

## Interactive mode

`gocu -i` opens a checkbox picker. All rows start **unchecked** — toggle the
ones you want, then press **enter** to apply.

| Key | Action |
|---|---|
| `↑` / `k`, `↓` / `j` | Move cursor |
| `g` / `G` | Jump to top / bottom |
| `space` | Toggle selected row |
| `a` / `n` | Select all / none |
| `M` / `m` / `p` | Toggle all **major** / **minor** / **patch** as a group |
| `enter` | Apply selected upgrades |
| `q` / `esc` / `Ctrl-C` | Quit without applying |

## Target modes (`-t`)

| Mode | Picks |
|---|---|
| `latest` (default) | Highest stable semver |
| `greatest` | Highest semver including prereleases (with `--pre`) |
| `newest` | Most-recently-published version, regardless of semver |
| `minor` | Highest minor/patch within the current major |
| `patch` | Highest patch within the current minor |

## Filtering

`-f` / `--filter` includes; `-x` / `--reject` excludes. Patterns can be:

- **Exact**: `github.com/foo/bar`
- **Glob**: `github.com/aws/*` (single segment), `github.com/aws/**` (any depth)
- **Regex**: `/aws/`, `/^golang\.org/`
- **Comma list**: `a/b,c/d` (also works across multiple flag uses)

Same rules apply to `--filter-version` / `--reject-version`, which match
against the resolved target version string instead of the module name.

## Cooldown (supply-chain guard)

```bash
gocu --cooldown 7d
```

Hides any candidate version published less than 7 days ago. If the highest
version is too fresh, `gocu` falls back to the highest *outside* the cooldown
window. Supported units: `h`, `m`, `s`, `d`, `w`. Examples: `48h`, `14d`,
`2w`.

The publish time comes from the module proxy's `.info` endpoint
(`{module}/@v/{version}.info`).

## How upgrades are applied

`gocu -u` doesn't rewrite `go.mod` itself — it shells out to
`go get module@version` for each chosen upgrade, then runs `go mod tidy`
(disable with `--no-tidy`). This lets the Go toolchain handle MVS, indirect
dependencies, and `go.sum` correctly.

If a single upgrade fails, the rest still run and `gocu` exits non-zero with a
summary so you can re-run on just the failures.

## Output formats

### Default (terminal)

Aligned color table with module paths as clickable links (OSC 8) in
terminals that support them: iTerm2, WezTerm, Kitty, Alacritty, VS Code's
terminal, recent gnome-terminal.

### JSON

```bash
gocu --json
```

```json
[
  {
    "module": "github.com/spf13/cobra",
    "current": "v1.6.0",
    "target": "v1.10.2",
    "bump": "minor",
    "published_at": "2025-12-03T23:51:15Z"
  }
]
```

Modules with no available upgrade still appear in the array with empty
`target` and `bump` fields.

## Environment

`gocu` honors the standard Go module environment:

- `GOPROXY` — comma/pipe-separated proxy chain (default `https://proxy.golang.org,direct`)
- `GOPRIVATE` / `GONOPROXY` — modules matching these patterns are skipped (not sent to the proxy)

## Flag reference

| Flag | Default | Description |
|---|---|---|
| `-t, --target` | `latest` | `latest`, `greatest`, `newest`, `minor`, `patch` |
| `-f, --filter` | — | Include only modules matching pattern |
| `-x, --reject` | — | Exclude modules matching pattern |
| `--filter-version` | — | Include only resolved targets matching pattern |
| `--reject-version` | — | Exclude resolved targets matching pattern |
| `--dep` | `direct` | `direct`, `indirect`, `all` |
| `-u, --upgrade` | `false` | Apply upgrades via `go get` |
| `-i, --interactive` | `false` | Interactive picker |
| `--cooldown` | — | Skip versions published within this window (`7d`, `12h`, `2w`) |
| `--pre` | `false` | Include prereleases |
| `--allow-incompatible` | `false` | Allow `+incompatible` upgrades |
| `--concurrency` | `8` | Max parallel proxy requests |
| `--tidy` | `true` | Run `go mod tidy` after upgrades |
| `--dry-run` | `false` | With `-u`: print commands instead of executing |
| `--json` | `false` | JSON output |
| `--no-color` | `false` | Disable color and hyperlinks |
| `--cwd` | _cwd_ | Operate in this directory |

## Status

v1 covers the common ncu workflow. Not yet implemented (tracked separately):

- `--deep` for nested `go.mod` discovery in monorepos
- Doctor mode (upgrade → test → bisect breakers)
- Major-version path bumps (`foo/v2` → `foo/v3` requires Go-source rewrites)
- Deprecated-version annotation in output
- Config file (`.gocurc.yaml`)

## License

MIT
