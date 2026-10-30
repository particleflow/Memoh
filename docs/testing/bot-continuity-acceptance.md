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
| Incoming delegation directory | New parent-reported writer01a18cd9 GET-only acceptance | Receiver791af2c4… list200:4 items/no next cursor, df60… exactly once; detail200; list/detail match original expert session/run/turn | This incoming sample passed; no repeat requested. Does not prove restricted-account or phone behavior |
| Failed/schedule/deleted-plan history, admission coexistence dedup | Source/isolated fixes and tests from original task; schedule history retention b11774d integrated via b360/schema180, later181 deployed | These are implemented/isolated evidence, not an outstanding request to rebuild the feature | Matching field samples for these directory branches still unverified; latest sample supplies none |
| Restricted-account access / revocation | Original isolated permission/transaction evidence | Source checks and isolated cases exist | No restricted-account field pass; do not change permissions or fabricate an account for this update |
| UI >20-row paging / empty page with cursor | Existing source/isolated evidence; old field sample12 items | API small-limit pagination passed for the old sample | Actual >20 UI page and filtered empty-page-with-cursor field cases remain unverified |
| Main-chat raw tool visibility / task transcript completeness | Original isolated projection evidence; task destination samples separately passed | Shared Web projection and preserved task transcript tested in isolation | Latest parent summary does not explicitly attest every live tool/log projection; no new claim added |
| HAP / other channels or runtimes | HAP36 installed-version label only in latest report | No new device acceptance from this Web sample | HAP cold/reopen, directory navigation/return, parallel conversation and permission behavior require their own evidence |

The046 directory evidence was read from the already-received parent handoff before selecting the next step; the cloud cannot claim to have opened its local `task-directory-046/OUTCOME.json`. Cold/hot fast-response work belongs to owner01a18ca3; read-aloud work belongs to owner01a18352-ad88. This ledger neither assigns them new work nor modifies their source. New fees, multimedia calls, product deployment and UI expansion are outside this update.

## Completed incoming-directory field check

Parent reports original writer01a18cd9 completed the proposed read-only step:

| Item | Observed result |
| --- | --- |
| Receiving Bot | `791af2c4-2d89-4c57-b863-5d5ff53ff9f7` |
| List | GET `/bots/791af2c4-2d89-4c57-b863-5d5ff53ff9f7/tasks?limit=100`, HTTP200;4 entries, no next_cursor |
| Matching real record | `bot_delegation` / `df60a59d-979e-404e-b707-ca50217b10a8`, exactly once; name `Delegated task`, reported state completed |
| Detail | Corresponding `/tasks/bot_delegation/df60a59d-979e-404e-b707-ca50217b10a8`, HTTP200 |
| List/detail destination | Receiving Bot; session `20942e37-d160-4645-8a4f-c210749ac733`; run `d8227e4f-d8fe-4c33-b3bc-01cb82bc83ee`; turn `02c69f3c-2b0a-43ce-b690-1ae1dd782c1a` |
| Side effects / resources | Pure GET; zero model/DB/write; space67 cleaned, none in flight |

Evidence: writer-local `F:/NAS-Assistant/tmp/incoming-df60-directory-20261030/OUTCOME.json`, parent transcription only. Structured transcription: `parallel-capture-3f361/ego/incoming-directory-result.json`. The full server/client version for this separate read was not supplied; do not manufacture one. It is not phone acceptance. The receiver's4-item catalog is a different Bot/scope from the earlier046 source's12-item reconciliation, so those counts are neither contradictory nor a single current global inventory.

The candidate/query source allows source or receiver (`db/postgres/queries/bot_task_directory.sql:50–58`); current account and session permissions are checked (`internal/handlers/bot_task_directory.go:19–51`), the delegation source/target relation and both paths are rechecked (`internal/sessiontask/directory.go:107–135`), and destination resolution verifies the original Bot/session/run/invocation (`directory.go:56–77`). The actual list/detail result now validates this one incoming branch. Do not repeat the same row merely to collect another pass.

## Remaining gaps and the exact evidence needed

No newly demonstrated product defect follows from this PASS. Keep the distinctions below; missing field coverage is not evidence that an implemented feature is absent.

| Remaining item | What is already available | Smallest missing evidence / limitation |
| --- | --- | --- |
| True failed task and schedule/deleted-plan history | Original isolated fixes/tests; old046 unknown-history field pass | An already existing **typed real task record_id/task_id**, authorized owning Bot and saved state/destination; for deleted-plan retention, an existing deletion/history linkage record. Do not create/delete a plan or mislabel a failed ordinary primary-chat run as a task. |
| Native admission and execution coexistence dedup | Original durable-admission source/tests | One existing admission ID linked to its actual run and one catalog occurrence. No matching field identity was supplied in the current handoffs; ordinary A/B runs and df60 delegation are not substitutes. |
| Real record with no execution association | Original isolated pre-admission/legacy unknown cases | An existing typed record with confirmed absent destination, and a directory row retaining its real state. Do not remove a link or reuse df60 (which has a valid run). |
| Restricted-account visibility / revoked permission | Original isolated checks; positive same-user GETs passed | An existing permitted test account/principal plus its already-established current scope and one expected inaccessible real record. No such principal is identified in these handoffs. Do not create an account, change grants, log in as another person or convert positive-access evidence into a denial test. |
| UI beyond20 rows / filtered empty page with cursor | Earlier API limit2/100 reconciliation passed | A naturally existing authorized catalog with >20 eligible rows for UI behavior, or an actual returned empty page/nonempty cursor. Source12 and receiver4 do not meet those sample requirements. Do not generate tasks or change permissions to create the shape. |
| Live main-chat projection versus original tool transcript | Isolated projection suite; b965 A actually delegated and task context identity is known | Already-retained DOM/screenshot evidence for exact A/C showing no raw tool rows, paired with the exact primary A execution-history tool data (A is not thereby a directory task). Parent b965 summary did not include this rendering evidence. If not retained, do not infer it from the natural text reply. New navigation observation belongs after the existing navigation owner's reviewed candidate; no parallel navigation fix here. |
| HAP continuity, task route/back, projection and concurrency | Shared source/isolated tests and Web field passes; HAP36 label only | Actual device/app build identity, account/Bot/contact identity, and observed same-contact reopen plus original task route/back behavior. A device parallel claim additionally requires a real correlated device observation; Web seq2805/2808/2810 cannot be reused as phone evidence. No phone participation/build evidence is supplied to this owner for these checks. |
| Current collector queue POST202/item mapping | Old queue implementation/fixtures; current WS real pass | A retained original POST invocation/requestId and matching completed202/item_id plus admitted invocation, if such a genuine prior record exists. The older dedda sample explicitly lost this response; the b965 sample used WS. Do not replay a message or create busy work solely to produce coverage. |
| Current-event internal receipt and billing | Older88be recovery/receipt/repeat409 passed; latest reply persisted once | Current event receipt was not read and is not exposed by the existing public projection. No SQL scope expansion is authorized by this update. Billing is not a prerequisite invented for the user's natural-chat goal; do not add calls to prove it. |

The full cef deployment SHA is still not supplied with the latest Web pass, and the incoming read's exact version was not reported. Existing outcomes remain valid observations with those version limits; collector SHA is never substituted for deployed product SHA. Restart/crash C1–C3 remain isolated evidence plus the earlier specific real recovery result, not a new all-events crash guarantee. Cold/hot response, automatic read-aloud and pending navigation candidates1f021/6e04 retain their existing independent owners; this update creates no work competing with them.

## One minimal next action: inspect already-saved incoming-list metadata

The only new action proposed now is **one local read-only extraction**, not another browser/API sample. The receiving list already returned4 entries. In the original writer's existing `incoming-df60-directory-20261030` evidence, extract the three non-df60 rows **only if their original response/metadata was retained**:

- task_type, task_id, record_id, reported state;
- whether destination exists, and its original bot_id/session_id/run_id/turn_id when already recorded;
- whether the saved list proves all4 rows and no next_cursor, and its recorded product version if present.

Return only those identifiers/metadata and a provenance pointer; no name/body content, token, private address, new GET, DB read or permission check. If OUTCOME saved only df60 or counts, report `other_row_metadata_not_retained` once. Do not rediscover the list or rebuild the sample to fill the artifact gap.

This determines whether a genuine failed/schedule/native-admission/no-destination candidate already exists in the captured evidence. A matching candidate permits this owner to specify a later exact read-only check; the extraction itself does not pass that branch. If all three are ordinary completed linked delegations, record `no_matching_existing_sample_in_saved_list` and retain the exact missing prerequisites above. No new task, paid sample, restricted account, device or UI is to be manufactured, and no inactive owner is cleared merely because the remaining evidence is unavailable.
