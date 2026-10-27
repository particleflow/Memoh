# Harmony Bot creation recovery source alignment

Source base `46fe31de6cdeab3ed11af88bcaa91bf9d1e7d51e`; stable shared source `434d82a46f48e4b2047d067282f0af434cc4f0bd`, including the upstream 793a1283 creation-recovery correction. Exactly thirteen generated files are byte-identical to that shared head: creation stream/store, pending/created-session records, create/progress/onboarding pages, onboarding guard and their affected tests. A single hash-guarded source overlay is composed by the existing builder. No replacement backend, permission mechanism or Bot creation implementation is added.

The upstream flow scopes pending creation to the current API base and authenticated account, retains the original request key for safe recovery, and checks scope/auth generation before applying delayed results. Pending storage persists only allowlisted creation metadata; arbitrary request metadata is rejected. Logout/account cleanup removes both named historical creation keys and scoped pending entries. This uses sessionStorage for the recorded browser refresh boundary; physical process-death recovery is not established here. Existing local service binding is not cleared by this account-state cleanup.

## Validation

- Four affected suites: 51 tests passed, including mounted progress/create components, request recovery and pending-state behavior. Two additional suites: 7 tests passed for created-target persistence and the phone-specific cleanup boundary. The one additional test verifies explicit account-state cleanup removes pending records while preserving the existing local service binding and unrelated storage. It is an isolated validation fixture, not another product module.
- Full generated Web typecheck and production Web build passed (existing chunk warning retained); changed-source lint has zero errors and two upstream multi-component test warnings. Source inventory: 168 entries / 177 source-support files. The existing exact 45+2 adaptation baseline is unchanged and passes.
- All thirteen composed files match shared 434d bytes. Native identity/permissions/configuration, HTTPS/login migration, 46fe model labels, original-session navigation and cf143 image timing inputs are unchanged. Existing 1.0.20 HAP and handoff ZIP hashes remain unchanged.
- These are isolated mocked-service tests and generated-source checks. No real Bot was created, no production account/permission/model was used, and no phone or production creation success is claimed.

## New package criterion and handoff

A new HAP is required after the final unified candidate is fixed: this tree changes packaged creation, storage and auth-cleanup Web resources, and already contains the post-1.0.20 model-label/catalog changes. Reusing or relabeling 1.0.20 would omit product changes. Once the unified owner locks the combination (including any later c7b/profile API work), compare the final Web/SDK contract to this source lock and build one next unused version with the retained official SDK. If a later change is documentation-only and generated/native/package inputs remain byte-identical, it does not justify another compile. Do not rebuild the qualified 1.0.20.

This checkpoint contains only stable 434d and the previously fixed phone overlays. Floating c7b/profile API work is not consumed or declared compatible. There is no new HAP, version bump, signing, release/deployment, download-index change or field-browser operation here. Final published package and rollout need their own explicit confirmation; the existing Windows writer retains signing/deployment ownership. Management scope remains the existing names, real state and original navigation; no new management dashboard is introduced. Historical 19 unidentified source changes remain unverified.

The additional scope test is replayed by copying `harmony-bot-create-scope.test.ts` into the prepared checkout at `apps/web/src/lib/` and using the existing Vitest command; it is not inserted into packaged application sources.
