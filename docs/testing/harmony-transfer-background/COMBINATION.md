# Combination instructions for the unified source owner

The standalone stage-2 branch does not edit NativeClient again. The following
rules resolve the three NativeClient conflict regions against fixed general
bridge `22b7cad6fa71d4818d461796c9d79bcc9bbf5ea7`. They come from the original
Harmony owner's read-only merge review in
`libfile_30a741a4b4ac8191bacb47e451bcd5f8`; no combined compile or runtime test
is claimed here. Do not choose an entire side of the conflict.

| Region | Retain from ownership candidate | Retain from general bridge |
| --- | --- | --- |
| Fields | `owner`, `view`, `uploads = owner.uploads` | page-owned `resetNetwork` callback |
| Constructor | existing singleton owner and its engines | fifth callback supplied by current SharedChat |
| `stopTransfers` | `owner.revoke()` and voice stop | reset page-owned network sinks/credential epoch |

For the combined constructor, an optional default preserves existing four-argument
test callers while accepting SharedChat's fifth callback:

```ts
private resetNetwork: () => void;
constructor(controller: webview.WebviewController,
  stateChanged: (state: string) => void,
  themeChanged: (theme: string) => Promise<void>,
  context: common.UIAbilityContext,
  resetNetwork: () => void = (): void => {}) {
  this.controller = controller; this.stateChanged = stateChanged;
  this.themeChanged = themeChanged; this.resetNetwork = resetNetwork;
  this.voice = new NativeVoice(context);
  this.owner = nativeTransferOwner(context); this.uploads = this.owner.uploads;
}
```

Apply the callback at each page-network invalidation boundary:

```ts
// close(): old document gone; accepted native work stays with its owner.
this.active = false; this.ready = false;
this.resetNetwork(); this.voice.close(); this.owner.detach(this.view);
// Existing deleteJavaScriptRegister cleanup remains.

// stopTransfers(): real logout/config invalidation revokes both scopes.
this.ready = false; this.resetNetwork(); this.voice.close(); this.owner.revoke();

// client.state === 'failed': failed document loses page network and voice.
this.resetNetwork(); this.voice.close();
// Do not call owner.revoke() merely for document failure.
```

Keep ready authorization, account epoch checks, credential-scoped stale
`upload.close`, explicit abort/pause aliasing and late reply checks from stage 1.
Keep general bridge NativeWebNetwork frame checks, sink abort and credential
cleanup. A textual auto-merge of SharedChat or build_memoh_web.py still needs
semantic validation. Do not move native owner instances back into a page or
reuse the page AbortController for accepted native operations.

Stage 2 binds the background controller only in EntryAbility. Keep that binding
once; page callbacks must not destroy, reset or replace it on ordinary detach.
System cancellation pauses original engines; account logout revokes the owner
and drains both old I/O and entitlement cleanup before new account work.

The standalone manifest now has 188 declared text sources: all original180,
four stage-1 additions and four stage-2 additions. The old17 stale entries and
the newly modified module manifest were refreshed (18 existing entry hashes),
with the exact lists in `inventory-refresh.json`. The source guard still checks
exact membership, bytes, hashes, UTF-8 and the original seven PNGs; it now admits
the eight explicit additions. For a combination with only the two added22b7
sources this becomes190; declare their actual paths and rehash final merged
bytes. No unseen general-bridge file is pre-admitted here.

Required unified verification: NativeClient close and failed reset page HTTP
sinks without canceling accepted original transfers; explicit logout/config
change stops both; reattach joins one native promise/system grant; late old
callbacks cannot revoke a new account. Rerun the focused bridge/lifetime suites,
the exact final source guard and the existing owner's API26 narrow compilation.
Phone notification cancellation, background/lockscreen, frozen progress,
relaunch and receipt expiry acceptance remain distinct device checks.
