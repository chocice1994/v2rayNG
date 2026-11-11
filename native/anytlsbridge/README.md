# AnyTLS TCP bridge (experimental)

This Go package is an in-process adapter for the project's existing Xray core.
`Prepare(config, protectedDial)` returns a `Prepared` with `Config()` and
idempotent `Close()`. Link it into the **same** gomobile AAR, avoiding a second
Go runtime/AAR. Each prepared configuration owns its listeners and clients;
the caller must close it on failed starts, reload and stop.

Only strict JSON AnyTLS outbounds are transformed. Ordinary JSON is unchanged;
non-JSON/JSONC is passed through to Xray unchanged. AnyTLS JSONC is not supported
and must fail as an unknown Xray protocol, never be treated as direct traffic.
The wrapper must supply a dialer using Xray's protected system-dial path. There
is deliberately no fallback to an unprotected ordinary socket dialer.

Supported AnyTLS schema:

```json
{
  "tag": "proxy",
  "protocol": "anytls",
  "settings": {"address": "example.invalid", "port": 443, "password": "example-only"},
  "streamSettings": {
    "network": "tcp",
    "security": "tls",
    "tlsSettings": {"serverName": "example.invalid", "allowInsecure": false}
  }
}
```

The transformed SOCKS5 endpoint binds exclusively to IPv4 loopback with fresh
cryptographically random username/password credentials. These credentials are
private to the prepared config. Never log that config, its original input, or
client credentials. TLS certificate verification is enabled by default; setting
`allowInsecure: true` explicitly disables verification and is unsafe on untrusted
networks. Unknown transport/TLS fields, source binding, chaining, enabled Xray
Mux and non-default target-resolution strategies are rejected instead of dropped.

## Preview limitations

- **TCP only.** SOCKS UDP ASSOCIATE is rejected. There is no UDP/UoT support and
  no direct fallback. DNS through this outbound must use a TCP-capable path.
- The pinned AnyTLS stream has no `CloseWrite`; EOF from either side closes the
  complete stream. Applications requiring TCP half-close semantics are not
  supported by this preview.
- AnyTLS stream establishment does not prove remote target acceptance. A later
  authentication/target failure can close a stream after SOCKS CONNECT succeeds.
- Tests are Linux loopback TLS/AnyTLS integration tests, not an Android APK,
  device, VPN-protection, DNS-leak or external-server verification.
- GPL-3.0-or-later dependency is vendored with a narrow documented race fix;
  see `../third_party/sing-anytls/LOCAL_PATCH.md`. Retain source and notices in
  any distribution; packaging compliance must be reviewed before release.

## Local checks

```sh
go test -race -count=20 -timeout=120s ./...
go vet ./...
go mod verify
```

Tests cover real TLS AnyTLS echo, injected protected dial use, bad server
password, untrusted certificates, hostname mismatch, local SOCKS authentication,
UDP refusal, invalid/unsupported options, failed-prepare listener rollback,
ordinary JSON/JSONC preservation, active connection shutdown and repeated close.
