# B05 combined on the published 526 checkpoint

Source: `a5f087df76e3a9c86c3a3de683d6dab13c391ad7` on the existing media branch. Combined onto `52609f7db4e59d7e528c2c5f8aa0330aa3ebbbe3` with no conflicts. This report describes local synthetic validation; exact candidate CI is recorded separately after push. The source report remains at ../shared-provider-transcription/README.md and describes its own earlier fixtures, not this combination's database.

## Necessary change and boundaries

Reuse the existing native OpenAI transcription factory for the same saved openai-completions/openai-responses provider. Native transcription list, explicit transcription model and parameter schema use the existing provider identity/configuration. The existing account/session guard protects all ten management routes; chat-route credentials are denied before fixture reads/writes/upstream. No new provider, credentials, service, protocol, model ID heuristic, bootstrap preset or synthesis alias is introduced.

Only one additional defect was found during integration: transcription checked the model enable flag but not the provider enable flag. A disabled provider produced a successful result and one synthetic POST for both compatible types. The added red regression now passes with zero POST after checking the existing provider enable field. This is four product lines including an errors import, plus twenty regression lines beyond the exact source patch. Metadata editing and synthesis are unchanged. The first constant fmt.Errorf form failed perfsprint; errors.New satisfies the existing rule without changing its configuration.

Source files other than audio/service.go and its added regression are byte-identical to a5f087df. sqlc v1.31.1 regenerated the two-line query difference exactly. Swagger, SDK, schema/migrations (177), official Agent/runtime/contextview and workflows are byte-identical to 526. There is no migration or API contract to regenerate. The original operation resume/CAS, cancellation ownership and observation behavior are retained.

## Executed local evidence

All logs are readable copies, unchanged bytes from this task. They contain only synthetic fixture content and development source paths. Private server logs, configuration, tokens, fixture IDs, database URLs and helper files were excluded. SHA-256 and size inventory is in evidence-manifest.json. Empty logs mean the command produced no output, not that it was skipped.

| Check | Command or action | Exit / result |
|---|---|---|
| SQL generation | existing pinned sqlc v1.31.1 generate | 0; generated diff identical to source |
| New Go contract/auth differences | go test -p=1 -ldflags='-s -w' ./internal/audio ./internal/handlers -run 'TestSharedChatProviderTranscription\|TestSharedTranscriptionDoesNotRegisterChatOrSynthesis\|TestTranscriptionManagementRequiresAccount\|TestTranscriptionMetadataAllowsValidatedAccount' -count=1 -v | 0; audio 0.008s, handlers 0.061s |
| Additional disabled-provider red | go test ./internal/audio -run TestSharedTranscriptionRejectsDisabledProvider -count=1 -v on pre-guard code | 1; both types sent one fake POST |
| Guard green | shared-provider contract, registry and disabled-provider tests | 0; SDK cases retained, disabled POST=0 |
| Final constructor cleanup | go test -p=1 -ldflags='-s -w' ./internal/audio -run TestSharedTranscriptionRejectsDisabledProvider -count=1 -v | 0 |
| Web types | pnpm --filter @memohai/web typecheck | 0 |
| Affected Vue lint | pnpm exec eslint apps/web/src/pages/transcription/provider-setting.vue | 0 |
| Affected Go lint | golangci-lint run --concurrency=2 ./internal/audio ./internal/handlers ./internal/db/postgres/sqlc | 0 before extra guard; final audio lint repeated and 0 after errors.New |
| Web build | pnpm --filter @memohai/web build | 0; existing chunk-size warning retained |
| Final server build | go build -p=1 -ldflags='-s -w' -o task-owned temporary server ./cmd/agent | 0 |
| Real HTTP and PG | current compiled Server, existing isolated synthetic DB, existing account login | 200 list/models/capabilities; anonymous 401; schema177 clean; all provider rows and all original model rows unchanged |
| Actual Chromium/current Web | native Voice list → same provider → explicit synthetic model → native parameters | pass; language ja, verbose_json; zero writes/test/discovery/transcription/model requests, no page errors |
| Refresh/re-read | normal refresh, then remove only persisted display query cache using existing cache functions and reload | pass; same saved parameters, two HTTP model reads; login unchanged |

The real-PG baseline 526 returned 200 but omitted the already existing compatible provider. This combination reused that existing synthetic provider and added only one explicit synthetic transcription model. No provider or credential was created and no Bot binding was changed. The request-level tests use an in-process transport; the RIFF sample is deliberately not claimed to be playable audio or genuine ASR.

The first browser helper expected two HTTP reads across normal reload; the app legitimately served its persisted query cache, so that helper assertion failed after UI assertions passed. The retained first failure is not a product regression. The subsequent explicit uncached read proves current Server behavior. An earlier Python helper mishandled its expected HTTP401 exception variable; only the helper was corrected. That console-only failure has no saved raw log and is not invented here.

The final screenshot was visually inspected: the real existing native parameter dialog, no selected test audio, Transcribe disabled, synthetic provider display name, masked key. No human QA or field device acceptance is claimed.

## Version / delivery matrix

- Stable published server/Web: 526, Docker #59 / run40237564492, parent-confirmed manifests and dual architectures. This work does not rebuild or change that artifact or the field deployment.
- Next B05 candidate: 526 plus the precise a5 source change and the enable guard above. Pending exact-head remote CI and a later explicit candidate publication; not deployed.
- Harmony PR82 source: 2b0c281ecd3ecd8d712f104ea9de56d60636b4c6 aligned to 526, per source handoff (153 targeted, 178 config, types/page passed; generated-context CI pending at handoff). It is not included here. Existing signed field 1.0.11 has not been rebuilt and does not include the new526 shared UI.
- Windows/task/transfer contracts: unchanged by B05. Source alignment is not evidence of actual Host/Pi operation recovery or signed-package/TLS acceptance.

## Remaining acceptance

A real authorized ASR ID and native multipart endpoint compatibility must be confirmed before configuration. No real paid ASR was called, no Bot transcription model was selected, and native mic/audio ingestion/transcript quality/same-session attachment return remain unaccepted. No guessed model or second provider is needed. B06 video/TTS work is not included. The snow-scene abort's historical cause and original missing output remain unrecovered; the earlier ACK fix does not prove that history's cause.

Space recovery removed only two verified inactive, task-owned local test binaries (old58e rollback binary and old526 integration binary), about264MB total. Their source, raw evidence, Git history, databases, original files and published images were retained. Intermediate B05 binaries were stopped and regenerated at the same temporary path. This is unrelated to NAS deployment or data migration.
