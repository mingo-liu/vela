# Code organization

Vela has a Go desktop service boundary and a React frontend. Refactors should keep
public Wails methods and model JSON fields stable so generated bindings continue
to work.

## Frontend

`frontend/src/App.tsx` composes navigation, the shared runtime controller, feature
hooks and page views.

- `app/`: shared runtime state, action accounting, navigation and sidebar traffic.
- `features/overview/`: connection controls and exit IP information.
- `features/proxies/`: groups, node selection, delay tests and their cache.
- `features/profiles/`: subscription management, imports and dialogs.
- `features/diagnostics/`: connection and rule views.
- `features/rules/`: subscription-scoped routing rule dialogs, editing drafts and original rule previews.
- `features/logs/`: log polling, filtering and scroll following.
- `features/settings/`: application settings, port editing and core information.
- `lib/`: polling lifecycle, snapshot ordering, visibility and error helpers.

The settings, proxies, profiles and logs hooks are mounted by `App`, even when
another page is visible. This preserves drafts, expansion and sort choices, delay
caches and log filters across navigation. Polling remains gated by page and window
visibility. Dialogs remain outside the main content element.

Feature hooks own backend calls and state changes; page components render that
state and invoke feature commands. `useRuntime` coordinates command responses,
runtime events, fallback reads, notices and the shared busy state. Features use
the same action accounting so concurrent operations cannot clear busy state early.

The diagnostics rule list uses TanStack Virtual to render visible rows plus a
small buffer. Rows retain their original rule numbers and wrap at their actual
height; refreshing data or changing the list width invalidates measured heights.
Search filters the full rule array and resets the scroll position. The list can
be focused and scrolled with arrow keys, Page Up/Down and Home/End.

Browser regressions build the production diagnostics component with an in-memory
runtime fixture, then check search, keyboard navigation, bounded DOM size, refresh
and resizing in WebKit and Chromium:

```sh
npm --prefix frontend exec -- playwright install chromium webkit
npm --prefix frontend run test:diagnostics
```

## Backend

`internal/desktop/RuntimeService` remains the Wails boundary. The `internal/mihomo`
package keeps one `Runner` with its existing synchronization and transaction rules:

- `runner.go`: shared state, initialization, observation and shutdown.
- `process.go`: process startup, readiness and stopping.
- `connection.go`: system proxy and TUN connection modes and proxy monitoring.
- `subscriptions.go`: imports, updates, selection and reload rollback.
- `settings.go`: routing, logging and proxy port changes.
- `controller.go`: core information and HTTP controller requests.
- `proxies.go`: group queries and persistent node selection.
- `delay.go` and `operation.go`: delay probes, progress and cancellation.
- `diagnostics.go` and `ipinfo.go`: diagnostics and exit IP lookup.
- `resources.go`: configuration compilation, geodata and private runtime writes.
- `custom_rules.go`: scoped rule editor snapshots and transactional live reloads. Inactive subscription edits never reload or switch the active configuration.
- `logs.go`: bounded core output capture.

`operationMu` serializes connection changes, profile transactions and process-exit
cleanup. Public `Start` and `Stop` acquire it; code already holding it calls
`startWithContext` and `stop`. `mu` protects state and session handles. Startup
checks, TUN authorization, readiness probes, controller writes and node-selection
restoration release `mu` during waits and reacquire it before committing state.
Helpers such as `requestWhileLocked`, `controllerGroupsContext` and
`waitReadyContext` enter and return with `mu` held. Observer callbacks must still
return without calling Runner.

Connection changes register their cancellation context under `taskMu` before
waiting for `operationMu`. A newer connection request cancels the previous one;
disconnect and shutdown can therefore interrupt both queued and active startup.
Cancelled starts reap their child process and skip restoring the old connection.
Ordinary startup failures retain the existing rollback behavior. TUN sessions use
a normal child process after startup so releasing the startup context does not
terminate a successful connection.

Subscription and rule reloads keep their saved-profile and running-config rollback
paths. They mark the controller as changing while state reads and logs remain
available. Controller reads reject intermediate results and results begun before
a reload, including failed reloads that roll back without changing `configVersion`.
Transactional apply remains non-cancellable.
