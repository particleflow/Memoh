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

## Saved-file extraction completed: other rows were not retained

Parent reports the original writer's local read-only extraction is terminal. `incoming-df60-directory-20261030/directory-page-01.json` contains only itemCount4 and the matching df60 row, not the full items array. The other three task types/IDs/states/destinations are **not retained**. No API/DB/model/write/new sample was performed; none remains in flight. The cloud owner did not read that local file.

Record this as `other_row_metadata_not_retained`, not as proof that the other rows are completed, missing or ineligible. Do not request another extraction from the same file. The previous restriction against an API request applied to that one artifact-extraction step; it was not a permanent prohibition on the original user's authorized directory GET.

## One next action: a single authorized directory metadata GET

This read is useful because the saved artifact cannot distinguish existing failed/schedule/native/no-destination records from completed linked delegations. It retrieves existing state; it does not create a test sample or repeat df60's passed list/detail comparison. The parent may assign it to the original unique writer under the existing account authorization, without a new approval request. No cold/hot, read-aloud, navigation UI implementation or separate navigation acceptance is included. Normal application entry is permitted preparation for this read.

**Correction after the ecf attempt**

Parent reports listTaskSpaces was empty because owned space67 had already been correctly closed. The ecf directory GET was **not sent**; the writer cleaned up with no API/model request or work in flight. The plan's ban on navigation incorrectly assumed a live authenticated Page. This was a self-imposed preparation error, not an authorization denial or product fault. Do not request another same-file extraction or require the user to log in again merely because an owned space was cleaned up.

**Preparation → metadata read → cleanup**

1. The parent assigns the original unique writer when available; this document does not reserve or compete for the browser. Reuse a suitable existing authorized Page if one exists. Otherwise create one owned temporary space/Page through the supported Ego workflow and open the normal authorized application entry using the existing logged-in browser profile. Empty listTaskSpaces is an expected cleanup outcome, not a blocker. No external browser, new endpoint, new account, token copying or alternative login flow.
2. Allow ordinary application initialization, including the normal contact resolver. Parent reports navigation owner544/0954 has already established that this resolver uses existing Chat permission, grants no new permission, and may idempotently establish the first internal contact when none exists. Do not block an operation solely because its HTTP method is POST. Do not manually issue a custom resolver POST, new/reset/fork a conversation, rebind an existing contact or delete history. Reuse the normal application path rather than recreating navigation-owner work.
3. Preserve the actual preparation outcome: confirmed existing contact, confirmed first internal contact initialization, or unresolved/unknown when the response does not establish it. Do not claim the whole preparation was write-free if normal initialization created a contact, and do not label that as a new expert task or new authorization. On an uncertain resolver response, do not blindly resubmit or change identity; follow the existing supported readback semantics if needed. The Bot-level directory GET does not require a contact ID: if the original account and receiver access are independently established, that read may proceed while the resolver outcome remains separately unknown. If authentication/target identity itself is unresolved or explicitly denied, stop that dependent read and report the precise fact; do not bypass it or ask for a redundant login without evidence that the session expired.
4. Once the original authorized application context is usable, perform the one metadata read below. Ordinary startup traffic is preparation, not additional investigator-directed task reads. If startup already produced this exact authorized directory response and its complete metadata was retained through the supported workflow, reuse it as the one read instead of issuing a duplicate. Otherwise issue one explicit GET. Do not keep an idle browser merely to avoid normal future preparation.
5. Save the complete sanitized metadata result before cleanup. In the existing supported finally/cleanup flow, stop only owned observation work, close only the space/Page created for this step, remove temporary scripts, and record cleanup status. Do not close unrelated/user-owned resources or log out of the shared profile. If a tool says execution stopping is uncertain, preserve that scope and use its supported stop/close requirement; do not report confirmed cleanup before the receipt. No persistent listener/service is introduced.

**Metadata request contract**

- Obtain one saved response from GET `/bots/791af2c4-2d89-4c57-b863-5d5ff53ff9f7/tasks?limit=100` through the same supported authenticated API workflow and the same originally authorized user. Retain the original API origin locally; do not print credentials, Authorization or a private address. The normal application entry/initialization described above is allowed; repeated navigation or refresh loops to retry a failed read are not part of this step.
- No detail GET, cursor follow-up, df60 comparison, task click, message submission, result-recovery POST, SQL, permission/account change, explicit task creation or model request in this step. Normal app resolver traffic retains its already-approved semantics; do not install a blanket POST-denial guard.
- On HTTP200 require a real items array. Preserve **every item in this response**, including df60, using only the fields below. Do not repeat the earlier filtering mistake by saving only the already-matched record. Names, prompts, summaries, attachments, headers and raw private URLs are unnecessary and must not be included.

```text
preparation: owned_space_created, existing_login_reused, account_scope_confirmed
resolver_outcome: existing | initialized | unknown | not_observed
cleanup: owned_resources_closed | uncertain | not_owned
observed_at_host_utc
http_status
scope_bot_id = 791af2c4-2d89-4c57-b863-5d5ff53ff9f7
items_array_valid
item_count = actual response items length
next_cursor_present = whether a nonempty next_cursor was returned
items[]:
  task_type
  task_id
  record_id
  state
  destination_field_present
  destination_shape = absent | null | object | invalid
  destination (only if object): bot_id, session_id, run_id, turn_id
```

Keep identifier types and values exactly as returned. An absent field is not an empty string, and an absent destination is not a permission-denied conclusion. If any identity/state is missing or has an unexpected shape, preserve that fact rather than coercing the response into a valid task. The task type/ID fields are evidence; do not infer them from the display name, chat session or latest run. Do not include name/body text in the handoff.

**Outcome and limits**

- If the catalog still has4 rows and no cursor, this one response preserves all4 current metadata rows. The old count4 is not an assertion: concurrent legitimate work may have changed it. Preserve the actual count and timestamp without deleting, freezing or retrying the directory to get the old number.
- If next_cursor is present, record `current_page_only`; do not fetch another page in this one-read step or call the current catalog complete. If items is absent/invalid, record a shape error; do not turn it into an empty catalog. On401/403/404 or other transport/status failure, retain the exact outcome and stop without changing the user/account or automatically retrying.
- Exclude df60 **only when identifying a new candidate after all row metadata has been saved**. A genuine failed record, schedule_fire, native_agent or absent-destination record can supply an exact original identity for a focused later check. The list alone does not prove deleted-plan preservation, admission/run deduplication or the reason for a missing destination. Return the candidate identity and the particular gap it could resolve; do not execute extra checks in the same step.
- If no such candidate appears, report `no_matching_existing_sample_in_current_page`, distinguishing a complete page from one with a next cursor. No new task/account/grant or paid work is to be manufactured. The exact true-device and restricted-principal prerequisites remain in the table above; a normal positive directory read cannot satisfy them.

Source/permission basis: the existing Directory handler revalidates the account's current Bot permission and per-session access, then reads the real-task directory; the known authorized receiver and endpoint are unchanged from the passed incoming check. No broader authorization, schema access or permission mutation follows from this request. The cloud owner has prepared this corrected contract only and has not opened a Page or performed the GET. The ecf no-request attempt does not consume the planned metadata read, and this preparation correction requires no new approval.

## Completed four-entry metadata preservation and queue follow-up

Parent reports writer terminal `01a18cef-197a-738c-b84b-f45e19e149a8` completed the f74 corrected preparation/read/cleanup contract: HTTP200, all4 entries retained, no next cursor; all are `bot_delegation` / `completed` with object destinations. Besides the already-accepted df60, records identified in the parent summary as dedda02f,1d742cc8,c0bf7fd3 retain their original session/run/turn associations. Full IDs are not reconstructed from those prefixes. Evidence is writer-local `F:/NAS-Assistant/tmp/receiver-task-metadata-20261030/OUTCOME-live.json`; the cloud has the parent's transcription, not direct access to that turn/file. Space68 was cleared, with no operation left unknown or in flight.

This closes the saved-metadata omission and supersedes the still-pending wording of the earlier one-GET plan above. Do not request the same directory or same-file extraction again. This complete page supplies no failed/schedule/native sample; absence of those kinds is not a product defect. Existing phone/restricted-account and other-kind coverage conditions remain unchanged, without manufacturing tasks or accounts.

The next narrow queue identity check found and fixed a real encoded invocation-path defect, product commit `c878a31e268119939330d9eed52050539636b306`. [Queue acceptance and field continuation](queue-post-identity/README.md) records actual HTTP202/item identity, reconnect/duplicate protection, real PostgreSQL admitted runs, local-fixture parallel answer/result receipt and the exact pending-memory-queue persistence limitation. This isolated pass does not upgrade b965's WS-only production evidence into a production queue pass. No new field/paid test was performed.

## Pending HTTP queue durability increment

Product `efac3b837b6c5291c09a714dddb4b73251367361` addresses the pending-memory-item loss separately from c878. [Durability evidence and migration contract](queue-durability/README.md) records the real loss counterexample, same-item process restart, single admitted B run, duplicate POST/reconnect identity, original-contact result receipt, and real-PG unknown/history/authority checks. Exact-commit terminal-before-claim recovery passed with one local B model call and zero active runs at cleanup. These are isolated results, not production, UI or HAP acceptance. No prior directory read or paid field sample was repeated.

Candidate migration182 is additive; integration must confirm numbering and existing encryption capability. Older memory queues must finish under their original writer; a list projection cannot reconstruct a lost command. Retain new queue/history data on rollback and use a reader-compatible binary when pending work remains. The evidence explicitly records an erroneous global CLI down on a disposable test clone, quarantine, and restoration into a separate database from a pre-test snapshot; later deleted experimental ledger metadata was not restored. Production and the original source database were untouched. The exact182 SQL preservation test does not make the global down command safe.

## Single-connection recovery review correction

Independent review found a nested pool acquisition in efac recovery; the previous in-memory recovery ledger test did not cover it. Product `f13f0ea09374788ea028c02168f7d99874c43570` moves ledger reads outside the queue transaction and conditionally applies only unchanged item/claim observations. [Narrow real-PostgreSQL red/green evidence](queue-durability/single-connection-recovery.md) records three new subcases plus the affected original unknown/lost regression, all green with one shared connection. No model, migration, field, or UI operation was repeated. Parent reports efac is not in4425; queue migration182 conflicts with another unpublished stream and must be numbered by integration. This increment does not alter migrations or erase the earlier isolated migration incident. The queue candidate awaits review/composition and later field acceptance.
