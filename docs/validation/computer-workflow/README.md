# Connected-computer workflow validation

**Historical evidence — the Required-system-fragment approach below has been withdrawn.** PR #5 at `3700f9bfa7a177ae187a5de542dab45b48f575dc` modified the native Agent assembler and context composition. It does not satisfy the requirement to leave the Agent core unchanged. The restoration removes that mechanism and its guarantees, while preserving ordinary tool descriptions and opt-in skill loading. See [the current boundary](../../agent-integration-boundary.md).

The following records describe their original validation stage. They are not acceptance evidence for deterministic service routing, conversation isolation or standard MCP interoperability. The former executable provider regression and frozen skill are retained as [withdrawn test source](withdrawn/computer_workflow_provider_test.go.txt) and [historical input](withdrawn/56a4d08f-SKILL.md), outside the unmodified engine tree. The published candidate at `3700f9bf` is a separate deployment checkpoint; restoring source here does not change that deployment.

Original scope: native execution-location guidance, an opt-in detailed skill, and a tool-assembly extension that preserved a code-owned routing contract as a Required system fragment. No target routing, authorization, execution protocol, budget arithmetic/pruning algorithm or default-target setting changed in that stage.

## Agent-core restoration checkpoint

- The complete `internal/agent/runtime/` and `internal/contextview/` trees match official baseline `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. The new CI boundary check passes the restored worktree and rejects `3700f9bf` as a negative control.
- Full Go tests passed for `cmd/internal/core`, `runtime/external`, `runtime/native` and `contextview`. Focused ordinary Usage, skill loading and execution-location tool regressions passed. The first broader test-name selection also hit two existing image-download tests that fail on this environment's proxy; those failures were not treated as passes and the proxy was not changed.
- The current server was rebuilt and used with the real Web application in Chromium at loopback ports 18080/18082, PostgreSQL 18 and schema 169. A new synthetic conversation called `list_execution_locations` once, returned its result, and reached a completed server run with the UI leaving streaming state. No command, agent, layout or grant mutation was dispatched.
- Both actual provider requests lacked the withdrawn `Computer routing contract` system text and retained ordinary tool Usage. The provider prescribed the tool call; this proves application integration after restoration, not autonomous GLM selection. [Sanitized observations](agent-restoration-observations.json) and [completed Web conversation](agent-restoration.png) preserve the evidence.
- The separate, uncommitted scope experiment was isolated before this change. It is not included or accepted here. Standard MCP computer-task routing and raw Agenvo scope enforcement remain pending. No candidate image, production deployment or human QA is implied.

## Withdrawn Required-routing revision

The detailed skill was reduced from 8,463 to 3,810 bytes by removing duplicate status/procedure text. It still uses ordinary skill activation and can disappear under pressure; moving critical sentences to its front would not protect them. The PC agent route was scoped to dynamically discovered Agenvo Windows targets, not future unrelated providers. In the withdrawn revision, three short routing/stop rules used `ToolRequiredUsage` and the existing `RetentionRequired` policy, separately from Preferred Usage. Skill metadata could not promote content. The legacy RunConfig collector preserved the same typed Usage fragments as the application path, instead of flattening their retention tiers.

The former `internal/contextview/computer_workflow_provider_test.go` drove the real native agent assembler, context applier, step selector and effective SDK provider requests. The provider was synthetic: it prescribed the next tool and unknown result, so these seven rounds established request retention under that withdrawn implementation, not autonomous GLM tool selection. A frozen copy of the original public skill preserves the 32K regression input; other context is explicitly synthetic padding, not production conversation text. The estimator was `bytes_ceil_div4_margin_v1` with 125% safety factor. The budgets below are estimated tokens, not vendor token counts:

| Case | Window | Output reserve | Tool definitions | System | Current request | History budget | Initial input |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Original skill reproduction | 32,768 | 8,192 | 16,687 | 6,247 | 15 | 1,627 | 22,949 |
| Compact skill + required rules | 32,768 | 8,192 | 16,687 | 6,528 | 15 | 1,346 | 23,230 |
| Reasoning + 110 synthetic messages | 65,536 | 16,384 | 16,687 | 7,706 | 15 | 24,744 | 37,425 |

- Original 32K: the first post-activation provider request contains a pruning stub, reproducing the failure.
- Compact 32K: the full skill reaches the first post-activation request (total input 24,512, within the 24,576 allowance). It is removed in a later discovery round. Required rules remain in `Request.System` through discovery and four read-only agent observations.
- 64K: output reserve resolves as `window_clamped`, not an asserted vendor output cap. The detailed skill is initially visible and later pruned; required rules remain across all seven requests. This is a compatibility/pressure case, not the intended GLM model context limit; no model registration is changed by the patch.
- Composition tests cover both typed-source and legacy RunConfig inputs, including a second fresh composition. Under pressure, ordinary Preferred Usage is dropped while the short Required contract survives. A smaller envelope raises `ErrProtectedContextOverflow` with zero provider calls. Other Usage is not elevated.
- A real multi-tool Container provider regression binds the contract to `list_execution_locations` even though `apply_patch` sorts first. Revoking discovery in the typed-source capability gate removes the contract while retaining ordinary Container usage. Initial assembly also rejects a provider contract whose declared capability is unavailable. The existing legacy flattened-input path does not implement roster revocation.
- The detailed skill also receives synthetic `priority`/`protected` metadata in the provider regression; it is still pruned, demonstrating that such metadata does not elevate it.

The rebuilt application was also tested in real Chromium at the same loopback URLs using a 32,768-token synthetic model, without raising the window. Captured requests 2 and 3 contained the full compact skill; requests 4–7 did not. The required routing contract remained in the system instructions for all seven requests. The chat invoked skill loading, target discovery, connector search and passive agent get/read fixtures only: no command, agent start/prompt, grant change or close was dispatched. The final reply kept the agent outcome unknown. [Historical 32K chat](required-32k.png), [unchanged per-Bot grants](required-grants.png).

This verifies delivery and retention, not that an unconstrained model obeys every rule. Actual GLM natural-language acceptance on the intended deployment remains a release gate. No image should be published based on these synthetic results alone.

## Historical first checkpoint (56a4d08f)

The following evidence records the initial checkpoint, including its failure. Its 128K follow-up did not resolve the 32K problem and is not the release criterion.

## Cloud development environment

The current Go server was built from the changed source and served at `http://127.0.0.1:18080`; the actual Vite Web application was exercised at `http://127.0.0.1:18082` using Chromium. PostgreSQL and the existing account/Bot services were real. The model, native workspace filesystem bridge and external Agenvo transport were controlled synthetic fixtures. No Windows/NAS access or real credentials were used.

## Passed

- Focused Go regressions: registration-gated computer guidance, optional skill references, parsing and native activation of the actual contributed skill, absent/unregistered skill isolation, current target listing, workdir-bound routing and existing Agenvo cwd/no-fallback cases.
- Native runtime prompt/usage injection tests and package lint. The server build succeeded.
- Real Web chat without the skill: the captured model request included the new guidance; `list_execution_locations` returned Bot-scoped availability and capabilities; `exec` received a discovered available Agenvo target explicitly with `work_dir: "."`. Exactly one command was dispatched in that chat and its completed receipt appeared in the same conversation. The conversation/default selection remained native. [Expanded receipt](workflow-basic.png).
- Existing authenticated skill-upload API installed only the synthetic Bot A's skill; its catalog then exposed the metadata. A later read-only Web chat invoked `use_skill`; the captured model request contained the full workflow. No command or agent was dispatched by this read-only chat. [Skill activation](workflow-installed.png).
- Bot B's target API did not expose the test Windows target. The existing grant dialog still showed A enabled and B disabled. No grants were changed. [Grant inspection](workflow-grants.png).
- No native exec fallback marker and no workspace close calls were recorded.

These deterministic model fixtures verify instruction delivery and the real application/tool path, not an unconstrained model's ability to choose the workflow independently. The Herdr/Pi/Magpie procedure was checked against the official Herdr lifecycle contract and uses dynamic connector discovery; it was not executed against a real agent by this cloud test.

## Failed attempt and limits

The first skill chat used the fixture model's existing 32,768-token context setting. Memoh's existing context selector replaced the 8.5 KB skill tool result with a pruning summary before the next model request. The fixture's assertion rejected that as missing instructions; the run ended with a synthetic provider error. Its Bash command had already completed once and was not retried.

The follow-up used a 131,072-token synthetic model window and requested only skill activation. The full skill was present in the actual request and the read-only turn completed. This does not establish that every model/budget retains the skill; deployment acceptance must inspect the actual configured model's effective context. No context-selection code was changed.

The initial browser script also failed to expand a grouped tool card. The completed chat was reopened read-only, the group and command receipt were expanded, and the screenshot was inspected; the command was not replayed to recover the screenshot.

No production natural-language autonomy, new Windows execution, real Herdr task completion, desktop control or human QA is claimed by these screenshots.
