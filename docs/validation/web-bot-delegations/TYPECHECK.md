# Frozen Web typecheck and ancestry comparison

Checked on 2026-10-22. These are failing compiler runs, not a passing typecheck.
The earlier 150-diagnostic statement applied to this PR's PR #45-based source,
not to main. No compiler options, dependencies, source or SDK were repaired as
part of this comparison.

| Snapshot | Full revision | Diagnostics | Files | Ahead / behind official latest |
| --- | --- | ---: | ---: | ---: |
| main851 | `851e8da4284c1f01e7e95f9547b16e7250bf553b` | 145 | 55 | 109 / 13 |
| NAS834 candidate source | `83483850061f4c908c28ff5ef340209cf9e718d5` | 145 | 55 | 177 / 13 |
| PR54 before shared-body change | `667c410d2ff079d51c293ad61804e414a76faafc` | 150 | 56 | 117 / 13 |

“NAS834” identifies the reported deployment source; no production system was
accessed to run these checks. Main851 and NAS834 diagnostics match byte for byte.
PR54 adds exactly five errors and removes none, all in the inherited PR #45
`src/pages/home/composables/useMediaGallery.delegation.test.ts`: TS2769 twice at
line 5 and TS2345 at lines 19, 20 and 24. These are scoped-gallery fixture/type
contracts, not errors introduced by the task body or its reader.

## Reproduction boundary

Each revision was independently archived with its own checked-in Web, SDK,
config and icons packages. Workspace links were checked to resolve each
snapshot's own SDK. The installed third-party dependency tree was shared; no
packages were fetched or regenerated. All snapshots have lock SHA-256
`8708c58e4e59d792dc4ca71a13c8a431068427382d8d41d08ff7bb16197eac68`
and UI submodule `f69402a55dd87a59c10722fe11507ad8fd7048f5`.
Compiler caches were not shared between snapshots.

Command in each snapshot's `apps/web`: `vue-tsc --noEmit -p tsconfig.app.json`.
Installed tools: Node v25.9.0, TypeScript 6.0.3, vue-tsc 3.3.8. Each command exited
2. This is a reproducible comparison under one installed dependency baseline,
not a claim about an independently clean installation or every CI runner.

Full logs: [main851](typecheck-main851.txt), [NAS834](typecheck-nas834.txt),
[PR54 667](typecheck-pr54-667.txt). Only the dependency installation prefix is
normalized in these text copies; [metadata](typecheck-baselines.json) records
revision, own SDK tree, lock, original diagnostic hash and exit status.

## Classification

Main/NAS contain 36 test-file errors and 109 production-source errors.

| Main/NAS diagnostic | Count |
| --- | ---: |
| TS18048 | 21 |
| TS2307 | 2 |
| TS2322 | 28 |
| TS2339 | 11 |
| TS2341 | 9 |
| TS2345 | 25 |
| TS2352 | 6 |
| TS2353 | 3 |
| TS2367 | 2 |
| TS2430 | 1 |
| TS2493 | 6 |
| TS2532 | 10 |
| TS2538 | 1 |
| TS2578 | 1 |
| TS2769 | 9 |
| TS6133 | 6 |
| TS7006 | 4 |

These codes are not all one root cause. Concrete categories include:

- Component event/value contracts and nullable values: config-schema-form (5),
  create-model (3), bot-select (1), plus Bot/channel settings callers.
- Checked indexing and optional values: memory-chart construction, login dot
  matrix, file icons, and webhook URL parsing. These require actual guards or
  valid data contracts, not disabled strictness.
- Test fixture and mock contracts: API-client tests (12), transcript tests (11),
  and the five additional PR45 gallery errors above.
- Dependency/import contracts: `bot-memory.vue` cannot resolve `reka-ui` and
  `markdown-dollar.ts` cannot resolve `markdown-it`; two diagnostics contain
  incompatible Vue 3.5.26 / 3.5.40 type identities. The installed tree therefore
  matters; counts alone do not prove a pure generated-SDK cause.
- Existing private-member access, unused declarations and one obsolete
  `@ts-expect-error` also remain. This PR adds no suppression or `any` workaround.

| Main/NAS source area | Errors |
| --- | ---: |
| `src/pages/bots` | 46 |
| `src/lib` | 17 |
| `src/store` | 15 |
| `src/pages/home` | 11 |
| `src/pages/login` | 10 |
| `src/utils` | 10 |
| `src/pages/dev` | 6 |
| `src/components/config-schema-form` | 5 |
| `src/components/file-manager` | 4 |
| `src/components/sidebar` | 4 |
| `src/components/create-model` | 3 |
| `src/composables` | 3 |
| `src/pages/appearance` | 3 |
| `src/pages/onboarding` | 2 |
| `src/components/bot-select` | 1 |
| `src/components/key-value-editor` | 1 |
| `src/components/settings-sidebar` | 1 |
| `src/pages/oauth` | 1 |
| `src/pages/providers` | 1 |
| `src/pages/web-search` | 1 |

## Ancestry, not just different SHAs

The official main inspected is `8e5fcabb71684ba9b35da37113b86a686c195117`
(commit timestamp 2026-10-22T14:12:09Z). GitHub compare reports all three histories
as **diverged**, with common ancestor
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. Ahead/behind counts above are relative
to that fixed official head and each exact snapshot, not to one another. They
are commit counts, not a measure of features missing or behavior verified.

No upstream merge was performed. The Agent runtime/contextview baseline check
against bf0318 remains a separate, narrower guarantee; it does not mean the
entire fork is identical to the current official head. New upstream fixes,
including Web typing and Bot-switch behavior, need their own compatibility
review before adoption.
