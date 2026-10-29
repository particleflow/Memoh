# Retired computer operation history: isolated verification

Current verification ran on 2026-10-29 against fixed source `7808bcd0722655844cc0c62fd61686b1c627da17`. The Go server at `http://127.0.0.1:58080` was rebuilt from that worktree and restarted. The unchanged actual Vue/Vite frontend runs at `http://127.0.0.1:58083`, using its checked source mirror, frozen lockfile and pinned UI submodule. Dedicated synthetic PostgreSQL/pgvector containers were reused. `mise` was unavailable, so equivalent Go and Vite processes ran directly.

**Current 7808 results:** the existing revoked runtime and deleted binding still permit all three owner history-detail reads (200), reject three foreign-user reads (403), and reject the wrong session (404). The list returns all three retired receipts. The deleted-binding observation GET returns **404 `session.not_found`** before any remote call. No resume POST or other task execution was attempted. Six operation rows, the original and fixture history sessions, and three message rows remain identical, including IDs, owners, state, revision, timestamps and details. Unknown remains unknown; two completed foreground invocations project as `process_exited`, with business completion still unknown.

The earlier three retirement phases and 21 assertions belong to source `6391829c`; its results remain under `prior_639_verification`. The subsequent delivery-denial patch's seven assertions are preserved separately under `prior_delivery_denial_patch_verification`. Those phases were **not** rerun or relabeled as current 7808 evidence. The parent owns separate PostgreSQL and live-callback tests.

![Current Tasks directory on fixed 7808 source](retired-tasks-current.png)

The actual browser opened Contact → Tasks, closed the dialog and reopened it. Three retired entries remained visible alongside the earlier three history entries. The fresh screenshot was inspected. Page errors, task write requests, OAuth requests and actual synthetic Agenvo requests were all **zero**; the OAuth token table remains empty.

The current UI mounts the Bot Tasks directory; the former `session-tasks.vue` detail component has no production importer. These hand-seeded receipts have no execution run/turn destination, so their navigation buttons are intentionally disabled. **The screenshot verifies directory visibility; individual details and scope restrictions were verified through the real HTTP API.** No fake destination or substitute component was added. Message rows are preserved byte-for-byte, but were not seeded as visible execution turns; their rendered content is not claimed verified.

Earlier setup resolved an environment-only Contact ambiguity (409 from multiple unbound legacy candidates) by adding a separate empty synthetic Contact session and route. No original history was rewritten. This final 7808 verification adds no fixture and reuses that Contact. No UI or product code was changed for verification.

See [verification-summary.json](verification-summary.json) for source/binary hashes, the scoped row digest, current and prior HTTP evidence, and limitations. The isolated processes and scripts remain under `/workspace/native-herdr-verification/retired-history/`. No real credentials, OAuth flow, Windows enrollment/call, model request, business execution or production deployment was used. This is agent verification, not human QA.
