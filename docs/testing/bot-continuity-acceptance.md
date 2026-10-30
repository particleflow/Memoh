# Fixed Bot conversation and real tasks: evidence ledger

This ledger separates field evidence, isolated evidence and remaining coverage. It is an update to this original Bot owner objective, not a new test run or a claim that every client/runtime has passed. All field results below are parent-reported; the cloud owner has not read the writer-local F: files. Earlier failed/inconclusive samples and their history remain valid records, not replaced by the latest success.

## Latest completed field sample

Parent reports the b965 collector sample passed and all business work is terminal. Environment label: production `cef` / HAP36. The full production SHA was not supplied with this result; do not invent one or equate collector commit b965 with the server version. This was a **Web** observation, not proof of HAP36 device behavior.

| Identity | Original value |
| --- | --- |
| Fixed source contact C | `1c686a5a-b181-472f-9fc1-e354f66c79a7` |
| Primary A run | `b57e784c-befb-4189-bea3-c1339e8a11bc` |
| Primary B run | `75a46ede-b2f4-425d-bb8f-0d78394914bd` |
| Delegation | `df60a59d-979e-404e-b707-ca50217b10a8` |
| Expert session | `20942e37-d160-4645-8a4f-c210749ac733` |
| Expert run | `d8227e4f-d8fe-4c33-b3bc-01cb82bc83ee` |
| Formal result reply | `43387ddc-1d4d-4ee5-8a6e-1b5fcbb217e9` |

Strict observer order **2805 → 2808 → 2810** establishes B completed → B's substantive answer persisted → the same original expert still running. This is not a queue202/progress acknowledgement or a comparison of different clocks. The expert later completed, `summary.source=assistant_final`; the natural result returned to C and refreshing did not add a reply ID. A and B were each sent once, with no business retry. Transport was WS; queue POST202/item_id was not exercised. Internal publication receipt and billing were not inspected. Parent reports owned space64 cleaned and no work left in flight.

Evidence location (writer-local, not a cloud-access promise): `F:/NAS-Assistant/tmp/parallel-b965-20261030/OUTCOME.json`. Structured transcription is in `parallel-capture-3f361/ego/field-parallel-result.json`. No request to repeat this sample follows from this ledger.

## Independent acceptance boundaries

| Item | Evidence / version | What is established | Remaining boundary |
| --- | --- | --- | --- |
| Web fixed C during expert work | Latest b965 sample; production label cef | B answers naturally in the same C while the original expert remains running | Does not establish every reopen/restart/model change or HAP behavior |
| Real primary/expert overlap | Sequences2805/2808/2810 and exact run identities | Live independent specialist work with a completed, substantive persisted B answer | Same primary session A/B turns still serialize; no claim of simultaneous turns within C |
| Natural result return and persistence | Same delegation/expert; assistant_final; formal reply ID; refresh | Original task result visibly returns to C without an extra reply on this refresh | Not a crash/restart/receipt-transaction proof for this new event |
| Current collector host pacing | Latest successful b965 field sample, plus a766 host pattern and7 seam checks | The repaired collector completed this real sample using Node-host pacing | Not buffer-capacity, arbitrary navigation, long retention or cross-round lifetime guarantees |
| Exact business WS identity | Latest sample used WS with original A/B/task identities | This sample's positive WS path was captured successfully | POST request/202/item_id mapping remains unexercised in this collector |
| Existing-result recovery and duplicate protection | Earlier production3f361: original event88be… recovered to attempt4; approved repeatPOST returned409, receipt1/reply1 and run set unchanged | The earlier original event was recovered without rerunning its expert; duplicate request was rejected | That receipt evidence is specific to the older event, not the latest one |
| Directory set/page completeness for existing samples | Earlier046 writer turn `01a189a0-48a8-732b-a855-7b6f9aa3827d`: E=12/X=0/U=0; limit2 six pages versus limit100 one page | Same12-item set, no duplicate/missing/extra item for that authorized sample | Not all possible task kinds/accounts/current production sets; do not rerun the same12-item exercise |
| Historical run destinations | Same046 field acceptance: same-session old→new→old→refresh, cross-history unknown, cross-Bot delegation | Original run/messages correctly selected for those actual samples | Does not cover absent execution links, every legacy record or HAP back navigation |
| Historical preservation during directory reads | Same046:17 business-table digests unchanged, zero business submission | Those reads preserved the observed stored history | Not permission to repeat DB reads or extend SQL scope |
| Incoming delegation directory | Previously explicitly untested | New completed df60… is now an available real sample | **One proposed next step below**, not yet passed |
| Failed/schedule/deleted-plan history, admission coexistence dedup | Source/isolated fixes and tests from original task; schedule history retention b11774d integrated via b360/schema180, later181 deployed | These are implemented/isolated evidence, not an outstanding request to rebuild the feature | Matching field samples for these directory branches still unverified; latest sample supplies none |
| Restricted-account access / revocation | Original isolated permission/transaction evidence | Source checks and isolated cases exist | No restricted-account field pass; do not change permissions or fabricate an account for this update |
| UI >20-row paging / empty page with cursor | Existing source/isolated evidence; old field sample12 items | API small-limit pagination passed for the old sample | Actual >20 UI page and filtered empty-page-with-cursor field cases remain unverified |
| Main-chat raw tool visibility / task transcript completeness | Original isolated projection evidence; task destination samples separately passed | Shared Web projection and preserved task transcript tested in isolation | Latest parent summary does not explicitly attest every live tool/log projection; no new claim added |
| HAP / other channels or runtimes | HAP36 installed-version label only in latest report | No new device acceptance from this Web sample | HAP cold/reopen, directory navigation/return, parallel conversation and permission behavior require their own evidence |

The046 directory evidence was read from the already-received parent handoff before selecting the next step; the cloud cannot claim to have opened its local `task-directory-046/OUTCOME.json`. Cold/hot fast-response work belongs to owner01a18ca3; read-aloud work belongs to owner01a18352-ad88. This ledger neither assigns them new work nor modifies their source. New fees, multimedia calls, product deployment and UI expansion are outside this update.

## One next step: incoming directory membership for the completed real delegation

Objective: close only the previously untested **receiving Bot directory** branch using df60… already completed above. This does not repeat global12-item reconciliation or paid parallel work. Parent schedules the existing unique writer when available. Use the same originally authorized user, the existing application login and supported read APIs; no SQL, permission/account changes, new conversation, task execution, recovery POST, browser framework or UI mutation is requested.

1. Reuse the original tool/delegation evidence to obtain **verified source Bot and target Bot IDs**, since the parent summary supplied neither. Never guess a name or ID. If the target ID is unavailable in the retained record, make one supported GET `/bots/{verified_source}/sessions/{C}/delegations/df60a59d-979e-404e-b707-ca50217b10a8`; verify delegation ID, C, target session20942… and run d822… before using its target Bot ID. If original source identity is unavailable, report that precise missing identifier instead of guessing or looking up a latest run.
2. Read GET `/bots/{verified_target}/tasks?limit=100`. Search by **task_type=bot_delegation and record_id=task_id=df60a59d-979e-404e-b707-ca50217b10a8**, not title/session/latest run. Follow each opaque next_cursor exactly until this record is found or pagination ends, including an empty page with a nonempty cursor. Repeated cursor or transport/authorization error ends the check as unresolved; do not silently restart/alter scope. This is target-side membership lookup, not a claim of current full-catalog completeness.
3. Read GET `/bots/{verified_target}/tasks/bot_delegation/df60a59d-979e-404e-b707-ca50217b10a8` once to exercise the directory's same current-authorization detail path. List/detail must agree on real IDs/state and destination: target Bot ID, session `20942e37-d160-4645-8a4f-c210749ac733`, run `d8227e4f-d8fe-4c33-b3bc-01cb82bc83ee`, and a valid original turn_id if returned. Expected execution state is completed from the settled field sample. Do not fall back to a different/latest run if a destination is absent or mismatched.
4. Return a sanitized table of source evidence identity, current version label, target/list/detail HTTP outcomes, page/cursor termination, matched typed IDs, state and destination equality. No token, private host, raw messages or content bodies. Pass means this one genuine incoming task is present and points to its original execution under the original user. Missing row after full traversal, identity mismatch or inconsistent state is a concrete failure to send back to this owner.401/403/404 or a changed permission boundary is a recorded access limitation, not permission to switch users or broaden access.

Source basis (owner's existing code, not an assertion of having read a full cef production SHA): `db/postgres/queries/bot_task_directory.sql:50–58` includes either original source or target Bot in the delegation candidate set; `internal/handlers/bot_task_directory.go:19–51` checks current account and Bot/session permissions; `directory.go:107–135` permits the queried Bot to be source or target, calls source-scoped DirectoryMetadata, verifies both session paths, and resolves the saved original run; `directory.go:56–77` verifies Bot/session/run/invocation and never chooses latest. GETs do not submit work. The proposed runtime responses are the authority for what this deployment actually serves.

This step cannot pass restricted-account denial, all historical kinds, HAP, queue POST mapping or internal receipt accounting. Those remain separately listed; completing one incoming record must not turn the entire matrix green.
