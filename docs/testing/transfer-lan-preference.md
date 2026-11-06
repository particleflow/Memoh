# Configured LAN preference without changing NAS identity

This increment follows `8b83ca85fd81dc24ff5678ef890c09d324db1b77` on PR77.
It implements an optional TCP route preference in the existing transfer host.
It does not add a NAS endpoint, protocol, service, task identity or file budget.
The earlier large-byte and default dual-stack evidence remains unchanged.

## Configuration ownership and compatibility

`HostConfig` reads one optional `transfer-network.json` from the **existing private
`HERDR_PLUGIN_CONFIG_DIR`**, using the same root/file ownership and link checks as
`transfer.json` and `device.credential`. This follows the existing optional host
policy loader pattern; it does not introduce another configuration root.

The existing `transfer.json` still supplies the only HTTPS origin/API prefix and
source root. Its exact bytes still determine HostBinding. Routing policy is
separate from identity specifically so adding/changing it does not invalidate
old queues or require a user migration. The code never rewrites either file.

Example shape below uses documentation-only addresses, not deployment settings:

```json
{"version":1,"lan_addresses":["192.0.2.10","2001:db8::10"]}
```

- List entries are explicit IP literals, in preference order. IPv6 zones are
  supported for scoped addresses. A hostname, URL, port, unspecified/multicast
  address, normalized duplicate, unknown field or malformed document fails.
- The owner designates these addresses as LAN candidates. There is no network
  scan, private-range guess, IPv4=LAN rule or IPv6=WAN rule. A global IPv6 address
  can be a valid explicitly configured LAN destination.
- The absent file preserves the previous resolver/transport behavior exactly.
  An invalid present file fails; it does not silently fall back to defaults.
- Configuration uses the existing bounded private-document reader (8 KiB).
  This bounds configuration parsing, not the number or bytes of transfer jobs.
- Older binaries ignore this optional file and do not implement the preference.
  The bundle's source revision must identify this implementation before its
  managed setup advertises the feature. Removing the policy returns to legacy
  routing without changing journals; no queue or schema migration is needed.

## Connection behavior

Both configured upload/download constructors now use the same policy before
any network action. On each **new TCP connection**:

1. Try the explicitly configured LAN IPs in order, at the original origin's
   port. The LAN phase shares the existing default TCP time bound of 30 seconds;
   remaining time is shared between candidates rather than multiplied per IP.
2. If TCP cannot connect, dial the **original authority** using `tcp6`, resolving
   its IPv6 addresses through the existing resolver. No alternative URL, Host,
   API path, credential, task/attempt or file reference is generated.
3. Once TCP succeeds, `net/http` performs its normal TLS verification against
   the original origin hostname, then HTTP authentication. TLS/HTTP failures
   do not cause another address to be selected. Redirects remain disabled.

The original DefaultTransport dialer bounds the fallback connection too; the
existing finite host invocation/request deadlines remain in force. This is a
connection-time failure bound, not a file-size limit or measured recovery SLA.
See Go's [Dialer](https://pkg.go.dev/net@go1.25.7#Dialer) and
[Transport](https://pkg.go.dev/net/http@go1.25.7#Transport) contracts.

A configured HTTP proxy owns routing. If the existing proxy function selects a
proxy for this origin while explicit LAN policy is enabled, construction returns
the existing configuration error `ErrBinding`. The implementation does not
silently disable the proxy or pretend that it can enforce LAN preference through
it. No environment proxy or NO_PROXY settings are changed by this code.

Healthy pooled connections are not interrupted. The next new connection uses
the preference again; existing finite host actions close idle connections on
exit. No additional scheduler, per-chunk probe or file hash pass is introduced.
If an explicit LAN address accepts TCP but presents the wrong certificate, the
request fails even when IPv6 would work. Failure is not permission to bypass
verification. If the original hostname has no usable AAAA address, the fallback
fails and the existing queue records its normal bounded connection retry.

## Executed evidence and consumer handoff

Product files: `internal/transferclient/host_config.go`, `host_action.go`, and
the new `host_network.go`. No Agent, server, database, Windows installer, phone
consumer, workflow or production configuration is changed.

The producer of the optional configuration is the existing endpoint setup owner;
the consumer is `OpenHostConfig` and the same constructors used by the finite
CLI upload/download actions. There is no HTTP API or result-field change for NAS,
Bot, Windows task feedback or the phone. This does not claim the installer/phone
already writes or consumes this local policy. Their setup owners can adopt it
without changing NAS identity or requiring approval for each file.

Targeted command:

```sh
go test -v -race -count=1 -timeout=90s -run 'Test(HostNetwork|ConfiguredLAN)' ./internal/transferclient
```

The raw result is in [the evidence file](evidence/transfer-lan-policy.txt).
Six top-level tests and two failure subcases passed in **1.247s** with zero skips. Tests cover
old queue binding across policy changes, strict validation, proxy conflict,
explicit IPv4-first and IPv6-first order, LAN refusal to IPv6, both unavailable,
and LAN recovery on a later invocation. A 16-byte transfer preserves its
task/attempt/receipt, full hash and reference with one Create, four PATCHes and
one Complete. Trusted wrong-host TLS and HTTP 403 do not reach the alternative.

TCP/TLS connections are actual Linux kernel loopback sockets; the hostname is
unchanged and only fixture RootCAs are supplied inside the process. HTTP storage
is the existing **in-memory protocol fixture**, checkpoint recovery uses the
existing test-file adapter, and separate binding tests use real DiskQueue
metadata. These are not a new real Server/PG end-to-end test, physical LAN/WAN,
Windows/phone acceptance or a power-loss claim. No system trust is modified.

The first test build had a Cleanup call typo; the second run exposed a new test
job whose SnapshotID did not satisfy the existing DiskQueue admission contract.
Only the fixtures were corrected. Both outputs remain in
[the development failures](evidence/transfer-lan-policy-development.txt).
There was no previously deployed product defect inferred from those failures.

Routed/public IPv6 remains unavailable in this cloud environment, as recorded in
the prior environment evidence. It was not retried. Real device route choice,
DNS cache behavior and network handover still require the corresponding device
acceptance; this code does not change production DNS, routes or trust settings.

Affected package regression command: `go test -race -count=1 -timeout=240s ./internal/transferclient ./cmd/transferqueue` passed (49.264s / 26.993s). [Raw output](evidence/transfer-lan-policy-regression.txt). Incremental lint and diff checks passed. This does not re-enable the opt-in large-byte scenario.

Evidence SHA-256:

- `transfer-lan-policy.txt`: `a3b0b89387e56cd6d202503af8da451d0bcd508cb999faad9f5665bcbc9a1ca7`
- `transfer-lan-policy-development.txt`: `1a986192360b6bf2fb030c907fcd1be2ac74fdd286c1ebd768e7b27c001c67c0`
- `transfer-lan-policy-regression.txt`: `ea5aea2c37880aad38352ca43f6fddc68354dd0301c46e750dfa16d47397450f`
