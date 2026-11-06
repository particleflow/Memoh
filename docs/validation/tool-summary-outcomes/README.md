# Media invocation summaries

The process header counted `generate_image`, `generate_video` and `transcribe_audio`
calls as generated media files, even if every call failed. It now counts media
**calls**, without inferring artifact creation. Existing protocol error flags add
failed counts; unfinished calls add pending counts, including late replies after
the chat turn ends. A completed call is not a count of ready files. Plain text is
not classified as an error by its wording.

This changes the existing group header only. Tool execution, detailed result
rendering, attachments, task state and Agent internals are unchanged.

## Verification

- Five new component regressions fail before the fix: all failures, mixed
  success/failure, pending-to-success after the turn ends, a missing reply with
  transcription output, and a mixed command/media group.
- The updated group and registry suites pass 46 tests. ESLint passes for the two
  changed component files. UI contract guard passes with three existing warnings.
- English, Chinese and Japanese locale JSON parses. No new styles or controls.
- Real development Web at `http://127.0.0.1:18082`, using the running strict-guard
  server (`HEAD /health` 200), reopens the existing synthetic rejection conversation.
  It displays **4 media calls, 4 failed**. Expanding a row shows the actual rejected
  argument error. Chinese/dark at 768 px also shows **4 次媒体调用、4 次失败**.
  These are read-only history interactions: zero chat POSTs and zero page errors.
  No synthetic or production business request is resubmitted.
- Mixed/late-response cases above are component tests, not a real model test.
  The browser verifies existing persisted failures and the disclosure interaction.
  No claim of human QA or successful image generation is made.

Before: [strict rejection screenshot with misleading caption](../image-workflow-cleanup/strict-result.png).
After: [summary](summary.png), [error detail](error-detail.png),
[Chinese dark/narrow summary](summary-dark-zh.png).
