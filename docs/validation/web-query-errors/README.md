# Web query error handling

This incremental branch is based on frozen PR #33 commit
`26acd283bd4aa6c3a9b394ab10c377781ca776f9`. It does not modify that commit,
the cloud-image branch, or a release candidate.

## Bot list

The list previously showed a successful empty state and a create tile after
its first query failed. It now renders the shared error banner and a retry
button. It never renders the rejected response text. During a retry, that
button is disabled. A successful empty response still shows the creation
entry. Cached Bots remain visible with an error banner after a failed refresh.

The existing global 401 handler still clears the browser credential and
returns to login. This change does not override authentication or permission
checks. The component can handle a rejected query while the host processes
that redirect.

- Real Colada component regression: 401, 403, network failure, manual retry,
  in-flight retry suppression, and cached-data refresh failure. The three
  initial-error cases failed against the previous component.
- Bot list and existing API-client suite: 19 tests passed. The API-client
  suite also covers credential clearing and stale 401 responses.
- Changed-source lint and UI contract guard passed (three existing warnings).
- Actual current-source Web app at `http://127.0.0.1:18082`, Chromium:
  synthetic 403 and connection failures showed the safe error banner;
  clicking retry recovered to a successful empty list. Narrow Chinese dark
  mode was checked. A synthetic 401 preserved the real redirect to login.
  No business action, connection, or authorization was submitted. Responses
  were controlled HTTP fixtures, not a production outage test.
- The browser shell has a separate Bot query, so aggregate browser GET counts
  are not a one-query assertion. The component regression verifies a single
  manual retry and rejects a second click while it is pending.

These screenshots are automated evidence, not human QA.

![403 list error with retry](bots-403-error.png)
![Narrow dark network error](bots-network-error.png)
![Existing 401 login redirect](bots-401-login.png)
