# Web typecheck baseline repair

## Scope and causes

Based solely on `851e8da4284c1f01e7e95f9547b16e7250bf553b`; independent of PR54's delegation observer. The fixed main/NAS834 snapshots previously produced the same **145 diagnostics across 55 files** (109 production-source, 36 test-source). See [complete before diagnostics](before-main851.txt). The previous PR54 150 figure was not the main baseline: its five extra gallery fixture errors were separately repaired in PR54.

This was not one generated-code failure. The repair covers:

- Align Vue runtime/type identity at the already used Web/Desktop version **3.5.40** via one pnpm override. UI/icons previously resolved 3.5.26. No unrelated dependency upgrade, UI submodule change or TypeScript/compiler-option relaxation. Preserve unrelated locked Babel resolutions.
- Consume the actual Input/Select/Checkbox payload contracts, including cleared numeric input; keep literal variant unions and model IDs. Guard optional indexes/values instead of asserting them away. Remove nonexistent Bot `type` / component props and unreachable idle-only voice glyph branches, without removing actual settings, permissions or recording controls.
- Import DateRange through the existing UI owner and MarkdownIt through its actual renderer owner. Preserve refined memory item types with a generic grouping helper, and use Vue's template-reference ownership for composables.
- Consolidate the existing ECharts internal live-layout access into one checked boundary using its exported `SeriesModel`, then exercise the installed library. This remains an internal accessor dependency, not a claim that ECharts made it public. Missing/incompatible models return existing safe defaults, not invented layout data.
- Add missing OAuth exchange path parameter annotations and regenerate Swagger + SDK. No OAuth runtime, scope or credential changes; no hand-edited generated contract.
- Correct incomplete test mocks/fixtures and stale assertions against existing contracts. ErrorIcon intentionally uses stroke 2; normal UI glyphs use 1.75. The quiet text workspace trigger owns its own tone. Modifier clicks on workspace-local URLs stay in that workspace. No production behavior was changed to satisfy these tests.

No new `any`, ignore/suppression, relaxed strictness, skipped check, removed business feature, provider, task layer or Agent core change. The obsolete `@ts-expect-error` is removed. Once baseline is clear, **Typecheck Web no longer has `continue-on-error`** in the existing ESLint workflow.

## Executed checks

On the cloud development environment, Node 25.9.0, TypeScript 6.0.3, vue-tsc 3.3.8:

| Check | Actual result |
| --- | --- |
| Frozen pnpm install; submodule/patched dependency checks | Passed; dependency lifecycle downloads were not run (`--ignore-scripts`); relevant repository verification scripts ran explicitly |
| `pnpm --filter @memohai/web typecheck` | **Exit 0, zero diagnostics** ([log](after-web.txt)) |
| SDK typecheck | Exit 0 |
| Desktop typecheck (including runtime preparation and shared Web) | Exit 0 |
| Full Web Vitest, 2 workers, 15-second test timeout | **253 files / 2,399 tests passed**, exit 0 ([log](web-tests.txt)) |
| Modified-file ESLint including new chart test | Exit 0; two existing multi-component test warnings and generated-file ignored warning |
| Web production build | Exit 0; existing large-chunk warnings remain |
| UI contract | Exit 0; three pre-existing warnings remain |
| Official core boundary | `runtime` and `contextview` remain identical to `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` |

A concurrent compiler run was killed with exit 137 during tests/Vite activity. It was not counted as a passing run; the final Web/SDK/Desktop compiler checks ran serially and all exited 0. The first full Web suite exposed stale mocks/assertions plus a storage mock regression from this work; those were repaired and the complete suite rerun. The Node SVG chart test logs the existing jsdom canvas limitation; real Chromium canvas was verified separately. No global prune or unrelated Docker rebuild was used.

## Browser evidence

Actual Chromium against the current Vite app on loopback port 18083 and the existing local synthetic server on 18080. The browser used already provisioned synthetic identity/data; no production access, paid model request, new identity, Bot creation submission or task dispatch. Observed **0 mutation requests and 0 page errors** in the checked flow.

- [Bot creation](bot-create.png): New Bot entry opens, editable name renders; form is deliberately not submitted.
- [General settings](bot-settings.png): settings/navigation and typed owner components mount. The fixture Bot's pre-existing `Create failed` status is shown honestly; this does not claim workspace provisioning succeeds.
- [Chat composer](chat-composer.png): existing synthetic conversation opens, typed draft is visible; no send.
- [Component wall](components.png): actual development page renders its owner components/variant matrix without injection errors.
- [Memory graph](memory-graph.png): synthetic graph endpoint, live ECharts layout/ref, wheel zoom, canvas render. After zooming out, both nodes have finite in-view pixel coordinates. No memory write occurred.

### Existing graph limitation observed, not hidden

The two-node force fixture initially auto-fits beyond the viewport. With a zoom-in wheel input, both this branch and temporarily restored **main851's four original graph implementation files** produce exactly the same zoom `2.5424983006428095`, center `[383, 239.99999999999977]`, and pixel Y values `728.1596737234199` / `-248.15967372341902` for a 766×480 canvas. Original files were restored byte-for-byte to this branch after the comparison. That is a graph-code comparison under the same installed dependency tree, not an independent production environment test.

A normal zoom-out interaction brings both nodes into view (Y approximately 310.48 / 169.52), captured above. The pre-existing automatic-fit issue is **not repaired by this typecheck change** and is not described as a fully successful initial graph layout. No expanded graph redesign is included.

[Read-only 13-commit upstream classification](UPSTREAM.md) is separate from this repair. Production provider/Pi/MCP/skills effective configuration was not inspected; a code-level absence of disabling flags does not prove live configuration or a historical pane changed. No merge, release, production deployment or upstream synchronization is performed by this validation.
