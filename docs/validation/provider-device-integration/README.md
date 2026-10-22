# Provider discovery and device observation integration

This follow-up preserves #38 at
`c4644d33b1377d36e634d90ee6c7f920c5194522` and adds #37 at
`a42d0b9592e9080f6e9d82df0bd2d365d43ce28a`. The 819575f6 discovery implementation
was independently reviewed; a42d0b95 subsequently corrects its invalid-ID error
to the repository field-error contract and adds an HTTP regression. The final
combination requires its own incremental review and CI before publication.

## Composition and contracts

- Both merges were conflict-free. There are no additional application edits:
  provider files match #37 and the existing device/Web behavior matches #38.
- Generated Swagger/SDK merge both contracts. All 270 existing paths and 487
  existing definitions from #38 remain unchanged. The sole new path is
  `GET /providers/{id}/model-discovery`, with four discovery definitions.
  Fresh Swagger/SDK generation produces no diff; SDK typecheck passes.
- The four generated files in #38 retain the original
  `userruntime.Runtime.availability`/`UserruntimeRuntime.availability`, alongside
  #35's MCP `bot_id` parameters. Keeping this field was a necessary preservation
  of #29/#32, not a new response schema. Both remain present after #37.
- Official Agent runtime/contextview are unchanged from
  `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`; no migration changed, local schema
  remains clean at 170, and all earlier 4521/813 integration ancestry remains.
- Discovery uses the existing team's configured provider. It does not register
  models, run inference, refresh OAuth, change credentials or change configured
  endpoint access. GET-only metadata transport and redirect rejection are not a
  comprehensive SSRF defense. Unknown capabilities and unknown list completeness
  stay unknown; Images API adapter support is separate from model support.

## Combination verification

Current Go server `http://127.0.0.1:18080`, Vite application
`http://127.0.0.1:18082` and local PostgreSQL were healthy. Authentication and
queries passed through both the server and Vite proxy. All fixtures were local
synthetic data; no production provider, PC, address, credential or content was
imported or published.

- Full provider-package race tests and focused handler race tests passed,
  including discovery, real Server/JWT authorization, private device and OAuth
  no-store, task authorization and workspace status. After a42d0b95, the error
  catalog guard and affected real HTTP tests were rerun successfully.
- Real PostgreSQL discovery passed with an isolated migrated database and a
  non-superuser/non-BYPASSRLS role inside a READ ONLY transaction. Out-of-team
  requests were rejected before HTTP. Session ancestry/stable-cursor integration
  also passed. These tests do not alter the running fixture database's schema.
- Running-application device checks retained six status observations and ten
  routing/authentication boundaries, with no-store on actual device responses.
  Authentication unavailable remains distinct from explicit offline and runtime
  unavailable. Native calls were limited to `workspace.list`, and operation
  records were unchanged. Seventeen task permission/revocation/pagination/PC
  scope checks passed with unchanged receipts.
- In the real provider detail page, the generated SDK made two discovery GETs
  followed by one rejected upstream GET: statuses 200, 200 and sanitized 502,
  all no-store. Undeclared model capabilities stayed unknown. Six direct HTTP
  rejection checks covered anonymous/invalid tokens, malformed/missing provider
  IDs, disabled provider and unsupported OAuth adapter, without an upstream
  request. The invalid ID used `request.field_invalid` with only `field: id`.
- The synthetic upstream observed exactly three GET `/models` calls and zero
  inference calls. No model rows were registered; provider row and operation
  ledger snapshots stayed unchanged. The browser made no business writes and
  reported no page errors. Temporary provider/native fixtures were removed or
  restored after verification.
- Targeted Web execution: 34 tests passed, one pre-existing test failed because
  `model-setting.test.ts` omits `SurfaceCard` from its UI mock. That exact failure
  reproduced on frozen #38 in the same environment, whose provider UI/test files
  are unchanged by this integration. It was not counted as a pass or changed as
  part of this backend increment. #38's prior 56-test regression remains separate
  evidence, not a claim that all current Web tests pass.
- Full Web typecheck still has **145 errors** (exit 2), matching #38. SDK
  typecheck, scoped Go lint, server build and Web build passed. The existing
  Web bundle-size warning remains.

The initial browser harness omitted the synthetic provider's explicit team ID;
PostgreSQL correctly rejected the insert before any discovery request. The
fixture was corrected; no access policy was changed. #37's initial Go CI failed
the BadRequest source guard; the a42d0b95 source fix is included here rather than
retrying the invalid source unchanged. Neither failure was counted as a pass.
One browser attempt also overlapped SDK regeneration and saw an incomplete Vite
module graph. After generation completed and the development services restarted,
the current-source browser verification passed. No application fix was needed.

## Fresh browser evidence

These actual application screenshots were captured and visually inspected on
the combined application, including the invalid-ID fix. The shown endpoint and
masked key are disposable loopback fixture values. They show that local model
registration remains empty, not that a real model can edit images.

[Before metadata GET](provider-before.png) ·
[After metadata GETs and rejected upstream read](provider-after.png)

This is agent verification, not human QA. Production provider discovery, paid
image inference, live PC recovery and mobile acceptance remain unperformed.
No main merge, deployment or promotion is part of this checkpoint.
