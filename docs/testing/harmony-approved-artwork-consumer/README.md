# Harmony approved artwork: exact Git consumer integration

The original `review/harmony-image-timing-20261026` branch fast-forwarded from `5445b299657dfe092083c916f146b1e5ba078844` to the approved resource commit **bd0b3933d24d944973fdca8e7d22c6f6b2a507bc**, tree **45a2916d09d84ea81760faa302c3533a28702c1b**. All15 changed/deleted resource paths still match that exact commit. No Library read/materialization, image generation or derivation script execution was performed by this consumer. The two approved images were opened and inspected.

## Exact resource identities

| Resource | Bytes | SHA256 |
| --- | ---: | --- |
| Approved whale; both application/launcher icon copies |1480302|`cea27d0e1798158a79df69d449ddbf280017b28a238eab5dbad6d8199eaef858`|
| Approved koi-lotus; startup copy |1569576|`e8e00500e14b444789cc27a891206f29eaef61b4f48c3828b12ee669de1b2917`|
| Light watermark |29921|`04ae2d4003d46a782068a74c9b1865677b88ee33c391fe9d2ce6545b45ce4d6a`|
| Dark watermark |30195|`59e089b2e6f925c3581263c95310f27a832e7f0f92f29f231fcd6a812a9d2dbf`|

Both icon copies are byte-identical to the approved whale; startup is byte-identical to the approved koi-lotus. No resize, recolor, redraw or recompression occurred. Both derived watermark alpha ranges were read as0..6, maximum6/255. The two CSS data-URI PNGs exactly match the corresponding derived files. They use the already-copied/hash-checked `harmony-service.css`; there is no competing builder change or extra network resource fetch.

Application and ability icon references still resolve `$media:app_icon`, now uniquely to the approved PNG. Both old active SVG files are removed. The module's sole logical change is `startWindowIcon` to `$media:startup_art`; the color file's sole change is its startup background to white. Bundle/version, all permissions/abilities, login/service-origin policy, return consumer, shared UI/SDK pins and image/audio/task logic remain unchanged. Nine critical input hashes are checked byte-for-byte against5445 in the [verification record](resource-verification.json), alongside all15 resource paths.

## Necessary source inventory follow-up

The incoming resource commit intentionally left the consumer's build/source recipe alone. Its prior inventory still required two deleted SVGs, omitted ten new files and rejected every binary as non-UTF8. A direct merge would therefore fail the existing source CI before any compilation.

This consumer updates only `mobile/SOURCE-PROVENANCE.json` and `mobile/check_source_subset.py` in addition to the unmodified approved15-file delta. The active text inventory removes the two SVGs, adds the three derivation/metadata text files and refreshes changed hashes:174 text inputs. Original historical-source evidence is not removed. A separate **closed list of seven specific approved PNG paths** checks exact byte size/SHA, PNG signature, dimensions1254x1254, every chunk CRC, required IDAT/IEND and end-of-file. Original NUL-free UTF8 checks remain mandatory for every text source. SDK/CLT/HAP archives or arbitrary binary paths are not admitted.

[Source and existing45+2 adapter checks](source-check.txt) pass:174 text entries,7 approved PNG inputs,190 tracked mobile files. No rule or original source assertion was waived. The ordinary source producer includes the CSS and native resource directories through existing paths. No official SDK compilation, Web production build, version bump, HAP, signing, package publication, deployment or device acceptance occurred. The old distributed artifacts retain their identities. Wait for the unified line's stable shared input before its next single candidate build.

The upstream artwork provenance file describes its earlier review-patch delivery state; this document records the actual committed Git consumption without rewriting those approved source bytes. Historical/rejected ring artwork is not reintroduced. This is resource/source verification, not a claim of actual launcher/startup/background rendering on a device.
