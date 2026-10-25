# Disabled Agenvo connection retirement — synthetic UI evidence

The current Go binary and real Vite UI ran at `http://127.0.0.1:58080` / `http://127.0.0.1:58083`, with isolated PostgreSQL. No real credential, enrollment, OAuth authorization/refresh, business operation, or production deployment was used.

The old binary reproduced the defect: disabling the synthetic MCP connection left its computer in all three runtime/target/grant lists. With the changed SQL, the existing **MCP → Enabled** switch restores those same entries when enabled and hides them when disabled. Native Runtime stays visible. The bot Computers page removes the old grant option.

- [Before: disabled legacy computer still listed](old-inactive-computers.png)
- [Existing MCP switch disabled successfully](new-mcp-disabled.png)
- [After: only native computer remains](new-inactive-computers.png)
- [Bot permissions: native retained, legacy option absent](new-bot-computers.png)

[Summary](verification-summary.json) records source/binary hashes, 9 successful HTTP list observations, and 9 database assertions. Exact row comparisons preserve both runtimes, both mounts, historical workdir/session/completed operation, and their timestamps. The MCP toggle is the only intended configuration update. No OAuth token row or additional operation was created.

Browser page errors, OAuth mutation requests, authorization redirects, and requests to the loopback synthetic MCP endpoint were all zero. The MCP detail page made its two existing read-only OAuth status requests. Screenshots were captured after the switch animation settled and inspected. Native and Cloud workspace are intentionally offline fixtures. This is agent verification, not human QA; earlier producer evidence remains unchanged.

Separate parent-reported validation passed: PostgreSQL/race cutover tests (9.282 s overall, including 16 UUID formats) and integration-tag golangci-lint. Detailed local browser records and production source manifest remain under `/workspace/native-herdr-verification/retirement/` and are not copied into this review bundle.
