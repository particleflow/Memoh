# Provider display-cache refresh integration

Product head: `6c3de5e9ea1b2fcba07cc82fcf8879045a764f67`, directly based on
`51b19cbeb76e9803eb8d4b614cfabb03ef18cc73`. Integrated by fast-forward on the
existing candidate branch; no conflict or additional product edit.

The only product change is the existing Provider query's
`refetchOnMount: 'always'` plus two explanatory comments. Sanitized display
snapshots intentionally omit keys. A fresh snapshot must still be revalidated
before the form can learn that a masked credential exists on the server.
This does not persist keys or weaken missing-key validation. Existing
per-field dirty-value preservation and failed-query behavior remain intact.

Executed here on the combined source:

- `NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run` with
  `models-panel.test.ts`, `components/provider-form.test.ts`,
  `components/provider-form-images.test.ts`, and `model-setting-cache.test.ts`:
  4 files / 43 tests passed, exit 0. The source author's separate 55-test
  claim is not represented as this integration run.
- Complete Web `typecheck`: exit 0.
- ESLint on the two changed files: exit 0, no warnings.
- Web production build: exit 0, existing large-chunk warning retained.
- Official runtime/contextview boundary and `git diff --check`: exit 0.
  No backend, SDK, runtime, workflow or configuration file changed.

Actual browser check reused the existing isolated Server/PG and current-source
Vite application (loopback API 18400 / Web 18402). The Provider was an existing
synthetic fixture pointing to `provider.example.invalid`. After hydrating the
sanitized fresh cache, a quick reload issued a real Provider GET. The test
delayed delivery of that real response in the browser, edited the name, then
released it. The edit survived; Save returned 200 without an API key in the
PUT. Another reload retained the name; all original configuration values were
unchanged. No paid model, device, connection-test or import request was sent.

[Inspected saved page](quick-refresh-saved.png) shows the synthetic masked
placeholder restored after the real GET. The password input value remained
empty. This is agent browser verification, not human QA or a production test.
GET-failure and missing-key outcomes are covered by the source's query tests;
this integration browser run did not repeat its author's separate 503 case.

`evidence.json` records exit codes, hashes and normalization. The full build
log remains local; its readable tail includes the successful result and warning.
No recovery-reader, actual Herdr, whole Go suite, or unrelated feature was rerun.
Remote CI for the final head is still required and is read by the parent through
its existing authorized connection; the rejected `gh` API is not retried.
No publication, production configuration change, deployment or PR readiness
change is performed by this integration.
