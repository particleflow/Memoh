# Settled speech cursor P1 correction

Source commit: `7a1784ce9a11ff31b76537431fb829debd25e87f`, based on frozen delivery `d90f0536e52ad782f0436abccb26994ad4d1bb3a`. Apply after e57d218b → ba44c062. This independent patch contains only the cursor correction and five narrow cases. Native incremental-input work remains isolated and is not part of this commit.

The final transcript row keeps its render ID but omits runtimeRunId. Existing code reads the spoken prefix from its observed run revision, then writes the tail cursor under the run-less row revision. Recollecting unchanged final DOM therefore resubmits the tail, and its duplicate exact source spans make the actual audio cache status `changed` instead of `ready`.

Reads and commits now use one `cursorRevision`: the observed run when the final row omits it, otherwise the explicit current revision. No cancellation, run identity, scope, authorization, caching policy or renderer logic is modified.

[Red](red.log.txt): missing-run repeat, actual EOF cache coverage and the equivalent next-run tail fail; retained-run and interruption/reset controls pass (3 failures /2 controls). [Green](green.log.txt): all five pass. The cache test uses the real SpeechReplayCache and fully consumed synthetic byte streams, not a mocked status. Explicit new-run cursor, interrupted-old-run suppression and silent baseline after scope reset remain covered. No old32/20 suite, unchanged browser matrix, paid model/TTS, ASR, build, signing or deployment was run.

[Normal commit hook](commit.log.txt) and [ESLint](eslint.log.txt) pass. [Exact source patch](source.patch.txt), [machine result](result.json). The review commit supplied by the parent was not retrievable from this remote (`upload-pack: not our ref`); no retry or alternate access route was attempted. The complete supplied counterexample was reproduced locally instead. No claim of browser, listening or ArkWeb acceptance follows from these source/byte-level tests.
