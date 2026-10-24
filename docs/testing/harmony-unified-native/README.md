# Harmony shared native-task and Provider alignment

The phone build now pins the complete shared Web/SDK source at
`9e6c47076bc507e0555002b73d3567297d914939`. All 5,802 public archive Git blobs
matched that commit. UI revision, dependency lock, app identity, permissions,
login storage and existing phone file adapters are unchanged. The existing
builder applies its original 44 platform seams and one targeted Provider save
fix described below; it does not copy a second set of phone pages.

The pin reuses `useNativeTask`, `native-task-content.vue`,
`useComputerTaskObservation`, the existing session-task reader/dialog, and the
shared Provider form. Content and follow-up use the existing generated SDK
`...TasksByTaskTypeByTaskIdContent` and `...Followup` calls. Reads stay within the
selected Bot/session/task; explicit interactive continuation retains its request
UUID before sending. An unknown result blocks a new request. Batch Pi remains
unsupported. Accepted input and terminal samples do not establish task success
or recovered deliverables. Older-server 404 is unsupported, not empty success.

## Confirmed save/reload defect and minimal fix

The real phone page submitted `images_api.protocol = openai-images` correctly.
Reloading 700 ms after save could hydrate a previous successful display-cache
snapshot and show "preserve existing behavior". A fresh profile reading the same
server response showed the new protocol. Diagnostic inspection confirmed that
the reopened form received the old cached provider, rather than rewriting the
server configuration or its Key.

The existing Provider save `onSettled` now waits for its existing providers-query
refresh and calls the existing `saveQueryCacheToDiskNow`, also used by Bot
settings. It retains the model/template invalidations. A refresh rejection is
caught so it cannot change the outcome of an accepted PUT; failed query entries
are omitted by the existing serializer. The serializer still strips secret
fields, and token/login storage is untouched. This is one guarded adaptation
in the current builder, with an independently applicable upstream patch in
[provider-cache-refresh.patch](provider-cache-refresh.patch).

## Executed checks

- First targeted shared/phone run: 114 passed, 1 failed across 10 files. The
  failure expected a browser download anchor. The existing platform-test
  preparer now also adapts that task-dialog fixture to assert `saveOriginal`
  with the explicit owner Bot/hash. The corrected suite passed 34/34, giving
  115 distinct targeted passing cases. The original failure log is retained.
- Additional Provider settings/deletion/Images/cache suites: 28 passed across
  four files. This includes the existing secret-stripping cache tests.
- Full phone Web typecheck passed; changed shared files and the new Provider
  adaptation passed targeted lint. No full repository/backend/native build is
  claimed by these checks.
- Actual Vite phone context at `http://127.0.0.1:4181`, Chromium 390×844, Chinese:
  task hierarchy, pending/failed results, verified synthetic PNG preview,
  native fetch/save command routing, escaped bounded terminal sample,
  unknown-result resend blocking, accepted receipt wording, batch suppression,
  old-server 404, transient read recovery and parent denial were exercised.
  The task run reached the Provider phase before its initial locator timeout;
  that combined run is not represented as an entirely successful run.
- The isolated Provider regression uses normal pointer/input interaction on
  the actual page. It checks ordinary save without Key/Images rewrites,
  explicit Images protocol save preserving other image policies, quick reload,
  blank Key input, sanitized disk cache, unchanged login and narrow dark layout.
  A second scenario rejects the post-save catalog refresh and verifies that
  the successful PUT is not reported as a failed save and old cache is not
  rehydrated. See `provider-normal.json` and `provider-refresh-failure.json`.
- One login survived page reload and browser-process restart in the task run;
  Provider reload also retained that session within its own isolated profile.
  Seven protected native/config/attachment inputs remain byte-identical.
- The unchanged source generator reproduced the complete archive byte-for-byte
  from already verified component archives. Source validation remains 125
  entries / 134 public files, with 19 historical later changes still unknown.

Screenshots are synthetic client evidence, not live-service or human QA:
[native sample](native-content-390.png),
[unknown request disabled](native-unknown-390.png),
[Provider after save/reload](provider-current.png).

## Reproduction and boundaries

Use `mobile/BUILD-REPRODUCTION.md` and the new lock to prepare a fresh phone
context. `docs/testing/harmony-task-delivery/prepare-platform-tests.py` adapts
the four upstream DOM fixtures to the existing native file boundary. Reuse the
pinned dependency cache; this change introduces no dependency.

With that context's Vite server running locally, run:

```sh
python3 docs/testing/harmony-unified-native/provider-cache-browser.py http://127.0.0.1:4181 /tmp/provider-normal
python3 docs/testing/harmony-unified-native/provider-cache-browser.py http://127.0.0.1:4181 /tmp/provider-failure --refresh-failure
```

All HTTP used by the browser proof is intercepted synthetic data. Native save
commands use a controlled OS boundary; no live NAS/Agent execution, physical
phone, signed upgrade or hardware permission/TLS acceptance was performed.
This alignment does not implement batch restart/recovery, cross-process file
resume or automatic LAN/IPv6 selection. It adds no API, transport service or
Agent core change.

No HAP was built, signed, uploaded or republished. The retained package still
belongs to source `f6403deaded6e76c0788aa66cffcaac5fef0badd` and does not contain
this alignment. Component notices from `5a5adecf` remain in the builder; only
their shared-source reference advances. The previously recorded UI license
coverage question and pending public-binary approval remain separate from this
source checkpoint. SDK, ZIP backups and private configuration are retained.
