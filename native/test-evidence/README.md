# Native test evidence

Paths in logs are normalized to `<checkout>`, `<module-cache>` and `<toolchain>`; diagnostics and outcomes are unchanged. Fixture passwords and hostnames are synthetic.

Compiler: official Go 1.25.5 linux/amd64.

- `anytls-upstream-v0.0.11-race-before.log`: unmodified upstream module, bridge `go test -race -count=20 -timeout=120s ./...`; failed with upstream stream-state races.
- `anytls-vendored-race-after.log`: final patched source, same command; passed.
- `anytls-vendor-lifecycle-after.log`: vendored module `go test -race -count=20 -timeout=90s ./...`; includes immediate cleanup-worker shutdown regression.
- Final bridge `go vet ./...` and `go mod verify` passed.
- Earlier coverage run: 80.0% of bridge statements.

`FINAL_SOURCE_SHA256SUMS` binds the after-results to the final native Go source and module files (paths relative to repository root). These tests exercise loopback Linux TLS/AnyTLS only. They do not validate Android packaging, phone runtime or external-server connectivity.

Integration checks used official Go 1.27.0 linux/amd64 against the pinned
AndroidLibXrayLite overlay. `core-integration-race.txt` records
`go test -race -count=3 -timeout=120s ./...`; `core-vet.txt` is empty on success;
`core-mod-verify.txt` records module checksum verification. The wrapper tests use
only local synthetic TLS/HTTP fixtures. `INTEGRATION_SOURCE_SHA256SUMS` binds the
checked preparation, build, integration-test and packaging-verification sources.
