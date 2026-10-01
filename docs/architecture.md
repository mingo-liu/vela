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

Splitting files does not change lock ownership. Helpers called while holding a
Runner lock and observer callbacks still follow their documented restrictions.
Subscription updates retain their saved-profile and running-config rollback paths.
