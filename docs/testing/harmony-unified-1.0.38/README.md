# Harmony1.0.38 /1000038: PR#97 read-aloud ungating on the phone

The phone 1.0.37 package shipped the shared pre-#97 `useChatReadAloud.ts`
(2031b224e41a) through the `paired959` snapshot inside
`mobile/client/mobile/memoh/shared-home-admission-final.json`: manual replay
was gated on cached audio (`replayAvailability === 'ready'`), an uncached
replay surfaced `chat.readAloud.unavailable` and an identity/config mismatch
surfaced `chat.readAloud.configurationChanged`.

This freeze consumes shared upstream `635fe1722db70613d196a1004e28cc7e3e1ff0ac`
(`fix(chat): make manual read-aloud always available with on-demand synthesis`,
merged as PR#97 in `09564fa3162cccfe4bd570753a811fb9832db97f`) exactly on the frozen 1.0.37 web baseline:
between the 1.0.37 pin 4425e3c5f594 and this head only PR#97's six web files
and server-side queue183/docs work changed; no other phone-consumed file moved.

Ported behavior (equivalent to PR#97, phone carrier):

- `useChatReadAloud.ts` -> exact upstream 635fe1722db7: `canReplay` is the
  pure text condition; an uncached replay enqueues on-demand synthesis through
  the same live path; a ready cache replays only after the read-only
  `getBotsByBotIdTtsPlaybackConfig` identity check; an identity mismatch or
  failed validation re-synthesizes; the unavailable/incomplete/configurationChanged
  gates, notices and locale strings are removed. Automatic admission,
  interruption and account isolation are unchanged.
- `useChatReadAloud.cache.test.ts` / `useChatReadAloud.dom.test.ts` -> exact upstream.
- Phone-adapted `en/ja/zh.json`: the three gated read-aloud strings removed; the
  remaining `chat.readAloud` section equals upstream post-#97 verbatim; all other
  phone keys retained byte-for-byte.
- Controller `chat-read-aloud.ts`, speech cache/timing/render modules and every
  other paired file are unchanged; all 181 paired entries re-verified
  hash-consistent after the edit.

Version moves 1.0.37/1000037 -> 1.0.38/1000038 in
`mobile/client/mobile/harmony/app/AppScope/app.json5`; 1.0.38/1000038 was
verified unused before the bump (version declarations and artifacts branches).
All six PR-triggered CI classes completed successfully on the 09564fa merge
of PR#97 (Go CI, ESLint, Native delivery fixtures, Native large-byte recovery,
Docker PR, Contribution policy CI); no all-green or deployment assertion is
made for this phone package. Local unsigned HAP build evidence is recorded in
the build report; no signing, injection or distribution happens here.
