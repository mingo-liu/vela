# Agent guide

These instructions apply to the entire repository.

## Project overview

Vela is a macOS proxy client for Apple Silicon, built with Go, Wails v3,
React, TypeScript, Vite, and the mihomo core. It supports system proxy and TUN
connections, subscription management, proxy selection, custom routing rules,
diagnostics, and a bilingual interface.

Read [README.md](README.md) for setup and user-facing behavior,
[docs/architecture.md](docs/architecture.md) before changing component or Runner
boundaries, and [docs/security.md](docs/security.md) before changing profile
compilation, controller access, file handling, or the privileged TUN service.

## Repository map

| Path | Responsibility |
| --- | --- |
| `main.go` | Application entry point and embedded frontend assets |
| `internal/app/` | Wails application, window, tray, and lifecycle wiring |
| `internal/desktop/` | `RuntimeService`, the public Wails API boundary |
| `internal/mihomo/` | Runner lifecycle, controller requests, connection modes, and runtime transactions |
| `internal/profile/` | Profile compilation, settings, subscriptions, saved selections, and custom rules |
| `internal/platform/macos/` | System proxy, Keychain, login items, traffic counters, and TUN integration |
| `cmd/vela-tun-service/` | Standalone privileged TUN service entry point |
| `frontend/src/app/` | Shared runtime state, navigation, sidebar, and traffic monitoring |
| `frontend/src/features/` | Feature hooks, pages, and dialogs |
| `frontend/src/lib/` | Polling, visibility, snapshot ordering, and error helpers |
| `frontend/src/i18n.ts` | Chinese and English UI translations |
| `frontend/tests/` | Node tests and diagnostics browser regressions |
| `build/` and `Taskfile.yml` | Wails development, build, and packaging tasks |
| `scripts/` | Pinned core build and geodata preparation |
| `.github/workflows/test.yml` | CI checks and their execution order |

## Setup and common commands

Use macOS on Apple Silicon with Go 1.26.9+, Node.js 24, npm, and Xcode Command
Line Tools. Match the Wails CLI to the version pinned in `go.mod` and
`frontend/package.json` (currently `v3.0.0-beta.24`). Run commands from the
repository root unless stated otherwise.

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24
npm --prefix frontend ci
wails3 generate bindings -ts -i
npm --prefix frontend run build
```

Generate bindings before frontend type checking or building. Build the frontend
before running root-level Go checks: `main.go` embeds `frontend/dist`.

```sh
wails3 dev                 # Desktop development with the Vite server
wails3 package             # Build and package bin/Vela.app
open bin/Vela.app          # Launch the packaged application
```

The Wails tasks prepare the core, geodata, TUN service, bindings, and frontend.
The first core build downloads and verifies pinned source; later builds reuse a
verified cache. The Vite server defaults to `127.0.0.1:9245`; override its port
with `WAILS_VITE_PORT` when needed.

## Implementation guidelines

- Keep changes focused on the requested behavior and follow nearby code style.
  Format Go changes with `gofmt`; follow `.editorconfig` for whitespace.
- Keep `RuntimeService` as the frontend/backend boundary. Preserve public Wails
  method signatures and model JSON fields during refactors. Regenerate bindings
  when changing the exposed API or models.
- Keep backend calls and feature state in feature hooks; page components render
  state and invoke commands. Use `useRuntime` and the shared action accounting
  for concurrent operations.
- Preserve feature state across navigation. Gate polling by page and window
  visibility, refresh when the window becomes visible, and retain cancellation
  and stale-snapshot protection. Keep dialogs outside the main content element.
- Preserve Runner locking and transaction rules. `operationMu` serializes
  connection changes, profile transactions, and process-exit cleanup; `mu`
  protects state and session handles. Code holding `operationMu` uses internal
  lifecycle helpers instead of calling public `Start` or `Stop`. Follow existing
  helpers when releasing `mu` during waits; observer callbacks must not call
  back into Runner.
- Preserve startup cancellation, child-process cleanup, configuration rollback,
  and rejection of stale controller responses. System proxy and TUN connections
  remain mutually exclusive.
- Keep custom rules scoped to their subscription or local profile. Editing an
  inactive subscription must not switch or reload the active profile. Preserve
  saved rules and valid node selections across subscription updates.
- Maintain both `zh-CN` and `en-US` UI behavior using the existing translation
  helpers and `frontend/src/i18n.ts`. Keep the English and Chinese READMEs aligned
  when changing documented user-facing behavior.

## Security and generated files

- Keep controller secrets and raw controller URLs out of the public Wails API,
  frontend state, and logs. Keep subscription credentials and private profile
  data out of the repository and test fixtures.
- Preserve the privileged service's peer validation, restricted controller
  gateway, configuration validation, private file permissions, and confined file
  reads. Do not bypass profile compilation for live reloads or weaken provider
  path and symlink checks. See `docs/security.md` for the full boundaries.
- Treat the bundled core as an independent build: its dependency locks live in
  `scripts/mihomo/go.mod` and `scripts/mihomo/go.sum`. Preserve source checksums,
  pinned toolchains, and cache verification in `scripts/prepare-mihomo.sh`.
- Do not hand-edit or commit `frontend/bindings/`, `frontend/dist/`,
  `frontend/node_modules/`, `build/resources/`, `bin/`, or other ignored build
  outputs. Change their source inputs and regenerate them through the existing
  tasks. Keep dependency manifests and their lockfiles consistent.

## Verification

Run checks appropriate to the change. For application code, the standard checks
are:

```sh
go test ./...
go vet ./...
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
```

Add or update regression tests for changed behavior. Backend tests live beside
the implementation as `*_test.go`; frontend tests use Node's test runner in
`frontend/tests/*.test.mjs`. For documentation-only changes, check the diff,
links, and command accuracy; application builds are unnecessary.

For core lifecycle, profile compilation, controller, or connection changes, run
the real-core suite used in CI:

```sh
sh scripts/prepare-mihomo.sh
sh scripts/prepare-geodata.sh
VELA_TEST_MIHOMO="$PWD/build/resources/mihomo" go test -race ./...
```

Without `VELA_TEST_MIHOMO`, real-core tests are skipped. For diagnostics list or
virtualization changes, also run the browser regressions in WebKit and Chromium:

```sh
npm --prefix frontend exec -- playwright install chromium webkit
npm --prefix frontend run test:diagnostics
```

Tests gated by `VELA_TEST_KEYCHAIN`, `VELA_TEST_SYSTEM_PROXY`, or
`VELA_TEST_APP_BINARY` exercise the host Keychain or network settings. Use them
when the task calls for host integration checks, with the existing restoration
and cleanup paths intact. The private-controller integration test disables TUN
routing; it does not replace a manual TUN connection check on the target Mac.

For security-sensitive changes, follow the vulnerability scans in
`docs/security.md` and CI. For packaging changes, run `wails3 package` and
`codesign --verify --deep --strict bin/Vela.app` on macOS.

Before handing off, run `git diff --check`, review the final diff, and report
the checks performed and any skipped or blocked verification.

## Git commit messages

Use Conventional Commits:

```text
<type>[optional scope][!]: <short summary>

<optional body>

<optional footer>
```

- `type` is required; include `(scope)` when useful. The body and footer are
  optional. Omit `!` unless the change breaks compatibility.
- Keep the subject short and specific about what changed.
- For complex changes, add a blank line followed by a body explaining the reason
  and key effects.
- Mark breaking changes with `!` or a `BREAKING CHANGE: <description>` footer.

Common types:

| Type | Purpose |
| --- | --- |
| `feat` | New functionality |
| `fix` | Bug fixes |
| `docs` | Documentation-only changes |
| `refactor` | Code restructuring without behavior changes |
| `test` | New or updated tests |
| `build` | Build process or dependency changes |
| `ci` | Continuous integration configuration |
| `chore` | Other maintenance |

Examples:

```text
build: track Wails build inputs and ignore generated assets
fix(session): restore system proxy after startup failure
feat(profile)!: change profile import format
```
