# Same-origin dual-stack recovery: tested scope and routing gaps

Base: `723edcf6340c158d0aa74967f04958f0d9da1bfe` on the existing PR77 branch.
This increment changes tests and evidence only. The prior 1 GiB + 512-file
Server/PG/CLI acceptance is retained and was **not rerun**. It does not introduce
endpoint aliases, a resolver, a transfer service or a new shared configuration.

## Existing routing and configuration

| Existing owner | Actual behavior at the fixed base |
| --- | --- |
| `internal/transferclient/host_config.go:29,74` | One `transfer.json` in the existing Herdr config root: `version`, HTTPS `origin` including `/api`, and `source_root`. Unknown fields fail. No LAN/IPv6 candidate list. |
| `host_config.go:90` and `host_action.go:73` | Exact config bytes determine HostBinding; each action compares that binding. Changing the configured origin is not a safe failover mechanism for an existing queue. |
| `internal/transferclient/client.go:50` | Clone `http.DefaultTransport`; preserve its resolver/dial/proxy policy; enforce TLS hostname/chain validation, disable redirects. No application-level LAN classifier or address ordering. |
| `host_action.go:76` and `cmd/transferqueue/main.go:25` | Production passes the default client policy. A finite action constructs a client, then closes idle connections. No extra network scheduler or discovery component. |

Go 1.25.7's [Dialer](https://pkg.go.dev/net@go1.25.7#Dialer) supplies TCP
address-family fallback (default fallback delay 300 ms); its
[address selection](https://github.com/golang/go/blob/go1.25.7/src/net/addrselect.go)
uses RFC 6724/source-route information, not a Memoh LAN label. Its
[DefaultTransport](https://github.com/golang/go/blob/go1.25.7/src/net/http/transport.go)
uses environment proxy policy, a 30-second dial timeout and a 10-second TLS
handshake timeout. The installed command's existing work context is 115 seconds.
These are failure bounds, not a measured recovery latency guarantee.

A pooled connection can remain on its existing address; a new TCP connection
consults name resolution again, which can still be subject to OS/resolver caches.
This test changes listener availability, **not DNS answers or cache TTL**.
`localhost` uses Go's existing proxy exclusion; production NAS proxy/bypass
configuration is not established by this run.

LAN is not synonymous with IPv4. The same IPv6 destination can use an on-link
route while on LAN and a routed path elsewhere. Thus absence of an application
candidate selector does not by itself prove a deployment routing bug. With only
one resolved address there is no second address-family target to try.

## Actual cloud capability

The current isolated Linux environment has IPv6 enabled and can send bytes over
an actual `::1` TCP socket. It has **zero global IPv6 addresses, zero IPv6 default
routes**, and its main IPv6 routes are all link-local. A route query and bounded
TCP connect to the reserved documentation prefix `2001:db8::1` fail locally with
`ENETUNREACH` (errno 101). No remote/private endpoint was contacted and no interface,
route, DNS, firewall, proxy or trust configuration was changed.

[Sanitized environment output](evidence/transfer-ipv6-environment.txt), SHA-256
`f1441a63785cf2a0e2c036fef28d95e5f131580a8e829fdc73d13140ea0ab455`. It contains capability counts and errors, not actual interface
addresses, private NAS names or credentials. **Public/routed IPv6 validation is
blocked by this environment's missing route; it is not an access denial.**

## New executable evidence

```sh
go test -v -race -count=1 -timeout=45s -run '^TestDualStack' ./internal/transferclient
```

Result: two top-level tests and two named failure subcases PASS, no skips,
package **1.186s**. Incremental package lint passed. The final source was the
source executed by this command; no post-run implementation adjustment follows.
[Original output](evidence/transfer-dual-stack.txt), SHA-256 `44f9a09d0fe088600655de1894f6c93f5aa28ea1c6c7e1698c7a796840e65e3e`.

The new test uses actual IPv4/IPv6 kernel TCP sockets, real HTTPS, a shared
certificate for `localhost`, the same authority/port and `/api`, and the
**production client with DefaultTransport**. Only its RootCAs are supplied by
the isolated fixture. No custom DialContext, resolver or fake connection is
injected. HTTP state uses the **existing explicit in-memory protocol fixture**;
checkpoint save/reload uses the existing small test-file adapter. This is not
another real Server/PG, production DiskQueue, installed CLI/Herdr, or native
Windows/phone acceptance. Those prior independent proofs cannot be combined
into an unperformed end-to-end network-topology claim.

| Scenario | Observed on this cloud host |
| --- | --- |
| Both addresses healthy | Actual IPv6 TCP/HTTPS selected; IPv4 had zero HTTP requests. This is not a LAN preference measurement. |
| Used IPv6 listener/connection closed | Same client reconnects with dial families `[6,4]`; IPv4 reads the same receipt at offset 4. A new client continues to offset 8. |
| Both listeners closed | Both families attempted; original receipt/operation/offset remain. Exactly one retry attempt is recorded with `connection` and a future due time. |
| IPv6 listener restored | Same hostname and credential recover the original receipt at offset 8, then finish. 16 synthetic bytes, one Create, four successful PATCHes, one Complete; full hash and reference match. |
| Preferred endpoint has a trusted wrong-host certificate, alternative healthy | TLS fails; no HTTP request reaches the alternative. Chain trust is independently established; hostname validation remains mandatory. |
| Preferred endpoint returns HTTP 403, alternative healthy | 403 is preserved; no silent rerouting to the alternative. |

The test helper advances its supplied clock to the recorded due time; it does not
claim that an installed background scheduler woke itself. A new client is used
for later finite invocations, but no process is killed in these network tests.
Closed listeners cause connection refusal; silent packet loss, a TLS blackhole,
DNS refresh/expiry and an Internet IPv6 route are not tested.

## Minimal next boundary

Keep the existing NAS hostname/API base, task identity, credentials and storage
configuration. First validate that the actual Windows/phone hosts choose the
expected on-link route and recover after their network changes; include their
real resolver and proxy behavior. That check cannot be performed against private
devices in this cloud-only task. Do not infer WAN traversal merely from a globally
scoped destination address, or infer LAN preference merely from IPv4/IPv6 family.

There is currently **no explicit application LAN-first policy** to claim as
implemented. If actual deployment evidence establishes that the OS/single-host
route is insufficient and a product policy is required, the minimal product
coordination surface is `internal/transferclient/client.go` (transport creation)
and, only if new user-configurable policy is necessary, `host_config.go` plus its
existing binding/consumer contract. No changes to these files are made here;
no alternate URL fields, new configuration root or manual queue migration are
proposed as a test workaround. TLS/authorization failures must not become
permission to bypass verification or send credentials to an unapproved origin.
