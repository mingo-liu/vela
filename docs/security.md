# Security boundaries and verification

## Privileged TUN service

The launchd helper accepts the installing user's Unix peer UID. It validates the
initial managed configuration, then starts Mihomo with a root-owned `0700` data
directory. This directory preserves rule caches and protocol state across
reconnects. The controller socket and initial runtime configuration are kept in a
separate root-owned `0700` session directory and removed when the session ends.
The GeoIP and GeoSite databases are installed with the core and verified before installation.

The GUI's controller token only authorizes the restricted loopback gateway. The
full Mihomo API is accessible through the private Unix socket. The gateway permits
version, traffic, rule and proxy queries, node selection, the fixed delay probe, routing
mode changes, and validated configuration reloads. Every reload passes through
the profile compiler again, fixing listeners, controller, secret, TUN and provider
paths. Restart, upgrade, arbitrary path reloads, unrestricted PATCH fields, and
other endpoints are rejected. The gateway has request timeouts, concurrency and
body limits, and does not forward redirects or internal error details.

User files are read through `os.Root`, with regular-file, size, ownership and
owner-read permission checks. Local rule files and node certificate/key files are
copied into private storage; symlinks escaping the user's data directory are
rejected. The helper never deletes user-provided stop paths. The unprivileged
client watches/removes its stop marker and half-closes the session connection;
client exit also stops the core.

Existing installations are upgraded through the normal macOS administrator
prompt the next time TUN is enabled with the new application.

## Rule resources

HTTP rule-provider paths are replaced with `rules/<SHA-256>.yaml`, derived from
the provider name and URL. A subscription cannot choose application filenames
such as `profile.yaml` or `settings.json`. Local file providers must use paths
under `rules/` or `ruleset/` in Vela's data directory. Move local rule files into
one of those directories and adjust the profile if it previously used another
location. Inline rule providers remain supported.

HTTP proxy-provider paths are similarly replaced with Vela-owned names under
`providers/`. Local file providers must use paths under `providers/` or `proxies/`
inside Vela's data directory. The TUN helper snapshots these files into private
storage after checking ownership, type, size, and directory confinement.
Inline and local provider node credentials are snapshotted into private storage.
HTTP proxy providers are rejected in TUN mode because the privileged core would
otherwise load remote node data without Vela checking local credential paths.

Local certificate/private-key/planet files must be readable files owned by the
user inside Vela's data directory. They are snapshotted for TUN on startup and
reload. Inline PEM and base64 private keys remain supported.

## Toolchain and bundled core

The application requires Go 1.26.8. `scripts/prepare-mihomo.sh` builds the pinned
Mihomo v1.19.31 source revision with the dependency locks in `scripts/mihomo/`.
The source archive is SHA-256 verified. Builds use `-mod=readonly`, a pinned Go
toolchain, and a cache key covering the recipe and both lockfiles. Cached binary
content is verified before reuse. Updating only the application's `go.mod` does
not update the independently built core.

Security updates include `x/crypto` v0.56.0, `x/net` v0.57.0, `x/text` v0.41.0,
`klauspost/compress` v1.18.7 and the patched DHCP revision. The core retains Go
symbol information for accurate binary vulnerability scans. Stripping symbols
can cause conservative module-wide reports for packages absent from the binary.

The September 29, 2026 scan found no reachable vulnerabilities in the application
or rebuilt core. The core scan also reports the unused `x/crypto/openpgp` package
at module level (GO-2026-5932); it is not linked into this core. This is not an
ignored scan failure: the default symbol-level scanner exits successfully.

## Verification

```sh
sh scripts/prepare-mihomo.sh
sh scripts/prepare-geodata.sh
VELA_TEST_MIHOMO="$PWD/build/resources/mihomo" go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -mode=binary build/resources/mihomo
npm --prefix frontend audit
npm --prefix frontend run build
wails3 package
codesign --verify --deep --strict bin/Vela.app
```

CI builds and scans the actual core and runs the real-core regression tests.
Tests cover subscription changes, routing modes, proxy port changes, rollback,
provider filename isolation, private Unix control, rejected privileged API
operations, symlink confinement, credential snapshots, and client stop signaling.
The private-controller integration test disables TUN routing to avoid changing
host network settings. These tests do not substitute for an administrator-enabled
TUN connection check on the target Mac.
