# Connected-computer workflow validation

Scope: the native execution-location tool description/usage and the opt-in `contrib/bot-skills/windows-computer/SKILL.md`. No target routing, authorization, execution protocol, model core or default-target setting changed. No production skill installation, image publication or deployment occurred.

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
