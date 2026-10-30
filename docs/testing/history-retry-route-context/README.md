# Preserve the historical route during later initialize recovery

Base/deployed Web: `1fa17fd72aca0f8ee4ab46f1578a369fb039016b`.
Product: `500d86f52ae97ba92ac8245bbafbe66ba21543db`.
Independent branch: `fix/history-retry-ownership-20261030`. Prior cold-transport
and navigation branches remain intact. No production deployment is performed.

## Preserve the failed field outcome

The parent reports Web digest
`9cb53e5297d7ee5d2e5d36e3bf852f5e7759a41d49b7a1119bea35cc53a1ba6d`,
strict TLS200 and matching resource/container hash, with Server stillcefdf0.
Original task1d742/sessionc736/turne669 component REST returned the two original
messages, but45 seconds still showed eleven skeleton nodes and zero messages.
No positive target subscribe/snapshot was obtained; that is not proof of no
online socket. The writer cleaned its space/no in-flight action and invoked
no new message/paid work. Source/CI/Docker success did not establish field success.

The raw `F:/NAS-Assistant/tmp/deploy-web-1fa17fd7/OUTCOME.json` is not available
in this environment and was not personally read. The parent summary above is
not substituted for an original file read. One minimal existing-only excerpt
was requested: post-REST catalog/session/contact/WS lifecycle scope/timing and
saved URL changes. It is still pending; no new page/API/DB/private-state probe
or unchanged45-second field retry was made here.

The unified [full-app-review/OUTCOME.json](../history-runtime-bootstrap/full-app-review/OUTCOME.json)
and recovery JSONs **were** read at1fa17fd7. They show real initial improvement
from the cold-transport patch and explicitly leave a later risk: scheduled full
initialize selected the fixed contact. Keep those facts separate from0954 and
the new deployed failure. The earlier `ensureWebSocket` change is retained.

## Proven later lifecycle defect

`main.ts:38` creates `useWorkspaceTabsStore(pinia)` before constructing the Vue
app/installing Router atline76. That creates chat store transitively. The old
chat store captured `router = null` once when it had no injection context.
Its `hasHistoryEntry`/`hasContactEntry` callbacks therefore never saw Home's
actual route, even after Router was installed and an authorized history selected.

On a later native initialize retry, `preferContact` became true, overriding the
explicit historical selection. The lifecycle closed/unsubscribed its history
runtime, resolved the fixed contact, and subscribed that contact instead. Home
could still have `?session=S` and cached history visible while global selection
and runtime subscription belonged toC. A screenshot of two cached rows cannot
establish correct ownership or ongoing runtime coverage.

The correction uses a small per-store, unpersisted route context. Home binds
its existing real Router before starting its URL synchronization. Callback
reads are live thereafter, so full initialize distinguishes history from the
ordinary Bot/contact route. It does not import a Web-only global Router,
manipulate route/store selection, introduce a second history-intent state or
infer intent from a persisted task tab. Returning to a plain Bot route still
resolves its fixed contact. Existing authorization/session validation, native
socket establishment and atomic REST/snapshot commit stay unchanged.

## New complete-frontend counterexample and correction

[full_app_repro.py](full_app_repro.py) launches the actual1fa17 Web entry in
owned Chromium/Vite at `http://127.0.0.1:49971`. No page/component/store is replaced.
Only HTTP/WS **ports are synthetic**, with isolated fixture IDs/token/profile.
The initial Bot catalog returns502, while the exact Bot/history REST and runtime
ports are available. After history starts, the catalog recovers; the existing
production backoff loop performs full initialize. The app's public store API
is read in this owned dev page, never mutated or inspected through Vue internals.
All outside HTTP/WS is blocked, and the unused Vite proxy targets loopback port9.
No native Server, database, model, external service or field site is involved.

| Accepted run | Actual observation |
| --- | --- |
| [Red original1fa17](red/OUTCOME.json) | Zero page errors. Initially subscribes/snapshots historyS, then unsubscribesS after the recovery attempt. The next socket subscribes/snapshots contactC. URL remainsS but public store session isC. |
| [Green corrected](green/OUTCOME.json) | Zero page errors. The later socket subscribes/snapshotsS again. URL/store/runtime agree onS, and both original fixture message IDs remain rendered. No inference frames. |

Both cases render two original rows and no skeleton at the end. Thus this is a
proved **later ownership counterexample**, not a claimed reproduction of the
field's exact45-second eleven-skeleton symptom. It preserves the first patch's
initial-load benefit. Backend authorization/transport behavior is not validated
by synthetic ports, and full NAS incident attribution/closure remains open.

Screenshots from those real frontend runs were opened and visually inspected:
[red cached history](red/final.png), [green preserved history](green/final.png).
They intentionally look similar; the public-state and socket trace establish
the ownership difference. The existing outage toast is expected in the fixture.
No added UI/details/diagnostic panel or copy is part of the product change.

The script now verifies the recorded ownership, first/final subscription,
original rows, zero errors/inference and cleanup. Each run retained one blocked
external `/css2` resource attempt; no external traffic was allowed. Those
assertions were checked against saved results without rerunning the browser.
Initial setup failures (wrong Vite executable path; incorrect model response
array shape) are retained under `setup-*`, excluded from accepted red/green.

## Checks and an explicit unresolved test failure

Two new real-bootstrap cases check late Router attachment and a subsequent plain
Bot entry. Relevant existing Home navigation/not-found cases pass. The combined
targeted run has **29 passes,8 failures**; see [targeted-tests.txt](targeted-tests.txt).
All8 failures are the existing Home read-aloud admission fixture at
`api.player.dispatchEvent`: its mocked player was not created. Removing the
new Home binding call gives the same8 failures ([voice-baseline.txt](voice-baseline.txt));
the patched Home was restored byte-for-byte afterwards. The cause is not
established, and this is not presented as a fully green suite or a product audio
fix. The fixture only receives the new no-op store action; no speech production
code or test expectation is changed. Further voice investigation is outside
this task and remains an explicit validation gap.

Changed-file ESLint, Web typecheck, copy/contract and diff checks pass; the UI
contract retains three existing warnings outside changed files. Old cold-start
cases, observer8/4/3 suites, unchanged full builds and field samples were not
rerun. UI/Harmony/Server acceptance is not inferred from those checks.

All new browser contexts closed; Vite was a retained direct child and exited
normally on SIGINT (`-2`). Foreground exec sessions ended; [ownership receipt](owned-execution.json).
There is no detached fixture/Server/DB to stop. Old C05 protected-process reads
and unverified cleanup are untouched.

## Unified and native consumption boundary

Consume the three production deltas together: route-context helper, chat-store
closure/export, Home binding. [consumer-map.json](consumer-map.json) records
base/after SHA256 and [shared-web.increment.patch](shared-web.increment.patch)
contains only those deltas. Keep current native Home/Back/voice/file adaptations.
Current Harmony mirror paths/source/locks were not supplied here; they are not
updated or claimed verified. Native owner must map all three deltas into each
current mirror and update its actual source/lock hashes. No old whole-file
overlay, package/signing, installation or physical ArkWeb test was used.

Before another field attempt, parent/unified owner can assess the existing
failure trace against this proven later-ownership defect and the unresolved
voice fixture. The source owner does not occupy the field or propose another
unchanged45-second run. Any eventual corrected initial-load acceptance must
retain the exact original task/session/run identity and positive runtime scope
where captured; absent events, refresh recovery or cached rows are not success.
