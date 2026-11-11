# Local race fix for sing-anytls v0.0.11

Source: https://github.com/anytls/sing-anytls/tree/v0.0.11
Module checksum: `h1:w8e9Uj1oP3m4zxkyZDewPk0EcQbvVxb7Nn+rapEx4fc=`
Module go.mod checksum: `h1:7rjN6IukwysmdusYsrV51Fgu1uW6vsrdd6ctjnEAln8=`

This directory contains the complete Go module downloaded through the Go module
proxy and verified with the module checksum database. The original LICENSE is
retained without changes. Upstream is GPL-3.0-or-later. This is modified source,
not an unmodified upstream release.

Only `session/stream.go` and `session/client.go` production files are modified;
`session/client_lifecycle_test.go` is a new regression test. A mutex synchronizes
terminal error reads/writes and close-hook registration/extraction. Hooks execute
outside the mutex, including registration after an already closed stream, so a
remote closure cannot race a late hook assignment. Blocking reads and writes do
not hold the mutex. The cleanup worker uses a cancellable ticker instead of sleeping for up to 30
seconds after Close. Wire encoding, authentication and TLS are unchanged.

The exact diff is `../patches/sing-anytls-v0.0.11-stream-state.patch`.
Bridge tests reproduced upstream data races under `go test -race -count=20` on
wrong-password and concurrent-close paths; the corresponding before/after logs
are in `../../test-evidence/`. These are local loopback tests, not Android tests.

Reevaluate/remove the patch after an upstream version fixes the stream-state
race and passes the same regression cases. Do not update this vendored source
without updating provenance, checking the diff and rerunning the tests.
