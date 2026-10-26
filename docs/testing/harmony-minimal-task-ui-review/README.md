# Source-only review: one minimal activity list

This corrects the prior 1.0.15 regression fix: restoring the `SessionTasks`
trigger also restored the old detail dialog, which does not match the user's
requested interface. This source review has **no new HAP, signing or deployment**.
The existing 1.0.15 package is unchanged and does not contain this correction.

## Exact visible contract

The task button now selects the existing sessions sidebar and opens the existing
mobile navigation sheet. It never imports or mounts `SessionTasks`. The sidebar
uses the same `session-activity.vue` as unified
`4d23244c683aca4c436efec4db375921fdf60472`, tree
`45f71558fbbf110761b12864dd892a97e1f5ddd2`.

The task region renders a heading and two text fields per row:

| Name | State |
| --- | --- |
| 红玉 | 执行中 |
| 绘图 Agent | 等待决定 |
| 参考图整理 | 正在结束 |
| 图像专家 | 执行中 |

Each row opens its recorded original conversation after the shared permission
recheck. No time, body, result, workspace/device, log, raw ID/JSON, tabs or extra
operation controls appear in this region. There is no separate task panel. The
surrounding existing navigation still provides chat, files and Bot management.

These are synthetic review fixtures, not claims about live Bot names or state.
All four clicks reached the expected original Bot/session; one login remained,
no page errors and no business write requests occurred. See the machine-readable
[DOM inventory](minimal-activity-dom.json), [full narrow screen](minimal-activity-full-390.png),
[task region](minimal-activity-only-list.png), and
[original conversation after navigation](minimal-activity-opened-original-390.png).

## Shared source and deployment boundary

The activity component/test and `useBotActivity` reader/test are byte-identical
to the exact unified commit. Only the generated activity GET, its shapes and
exports are appended to the pinned phone SDK; unrelated SDK changes/removals
are excluded. [Source and scope proof](review-summary.json).

The full cross-Bot list requires the new production
`GET /bots/{bot_id}/activity`. Its production deployment/readback is **not
verified here**. The real shared reader's missing-route fallback remains scoped
to the existing conversation reads; it is not declared equivalent cross-Bot
coverage. Permission failures are not converted into successful cached results.

## Sidebar first visibility

The old mobile sheet mounts file and schedule panels even while chat is selected.
The file pane's immediate Bot watcher calls `reload()`/`listDirectory()`.
The patch mounts those panels only on first explicit selection, then retains
them during the open drawer. Activity reads also use the actual mobile host
visibility. No animation, placeholder or new loading UI is added.

With compilation/tests finished, paired local Chromium 390x844 runs produced:

| Three openings | Frozen 1.0.15 | Reviewed source |
| --- | --- | --- |
| First visible frame (ms) | 210.7 / 80.5 / 86.3 | 185.0 / 69.6 / 61.4 |
| Median first visible (ms) | 86.3 | 69.6 |
| Hidden file GETs | 3 | 0 |

This isolates avoidable hidden initialization and shows a limited local
improvement. Three samples and no long-task callback records do not establish
the phone's root cause or prove its stutter resolved. Programmatic DOM click
timing excludes physical touch/ArkWeb dispatch. Actual phone measurement remains
required. [Before](sidebar-profile-paired-before.json),
[after](sidebar-profile-paired-after.json).

## Verification and next gate

Full generated Web types/build passed; 108 targeted tests passed, including the
new trigger regression and shared aggregate pagination/permission tests. The
old rich component's independent tests remain unchanged; they do not demonstrate
the product entry, which is verified by the actual compiled-page DOM above.
Source check: 140 reviewed files / 149 source-support files, 43 existing phone
adaptations. ESLint: zero errors, two attribute-layout warnings on the trigger,
recorded without suppression. The post-build test addition was the only new
file; all 1,014 generated Web/SDK sources were compared against the final context
and matched. No official native compile is claimed for this source review.

Parent review of this exact display inventory and production endpoint readback
precede any next-version HAP. Image measurement work is paused in a separate
local checkpoint and is not included. The deleted attachment recovery button
and explanatory text remain absent.
