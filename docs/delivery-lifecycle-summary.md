# Finite delivery lifecycle projection

`transferqueue -action lifecycle-summary` and the Herdr plugin action of the
same name return exactly this bounded, non-secret JSON object:

```json
{"schema":"memoh.delivery.lifecycle.v1","state":"waiting","next_due_at":"2030-01-01T00:00:00Z","error_class":"transient"}
```

The existing finite `advance-results` action adds the same four fields beside
its original delivery/reference fields. Exit status alone is not a retry or
delivery decision. `reference` at ready still requires NAS association.

States are `due`, `idle`, `waiting`, `blocked`, `unknown`. Eligible independent
work wins over unrelated blocked/unknown records. Waiting uses the earliest
eligible `max(job.next_attempt, association_due)` without changing that deadline.
Due, idle, blocked and unknown have a null deadline. Idle covers this existing
queue only: it does not claim business completion, enumerate new producer
manifests, or account for a result that crashed before closure/admission.
Incomplete admissions require the original producer collection hook; uncertain
creates never automatically recreate. Unknown association outcomes remain
eligible only while their existing reconciliation budget permits it. Confirmed
payload release can be due; summary never performs the release itself.

`error_class` is `none`, `transient`, `busy`, `authentication`, `permission`,
`configuration`, `capacity` or `unknown`. Authentication includes a missing local
credential or a recorded remote 401/403 authorization refusal; it does not claim
to distinguish them. Permission describes verified private-storage permission
failures. An old blocked association with no recorded cause stays unknown.
Transient/busy without a durable deadline are unknown and permit **at most one
host retry after 30 seconds per external event**; a repeated result suspends
automatic advancement until a new completion/startup/network/owner event. This
hint never changes transfer retry counters. Never poll unknown/blocked in a tight
loop, reset attempts, create a new delivery, or rerun Pi to make progress.

Summary validates existing configuration and credential syntax locally, without
contacting the NAS. Every network request still reauthorizes. It opens only
existing queue files and locks, and uses the same bounded checksummed frames,
identity-bound compaction witness and release receipt as normal recovery. It
does not create directories/locators/locks, repair a torn tail, compact, sync,
reclaim, enqueue or write credentials. Missing/incompatible storage fails
explicitly. It is not an initialization command. Corrupt metadata can require
owner recovery and is never interpreted as an empty queue.

The queue selects one finite job using persisted due times, then last finite
execution time and logical ID as deterministic tie breakers. This scheduling
metadata and a bounded association error class live in the existing journal;
there is no additional registry. New fields are rejected by older strict JSON
readers. Do not roll an older writer onto a queue written by this version.
Transfer IDs, request scopes, payload format and migrations are unchanged.

Herdr invokes the fixed relative executable in the existing plugin environment.
The caller correlates stdout with that one invocation or its Herdr log ID, never
with terminal acceptance. Each call remains bounded to 120 seconds (115 seconds
for work); existing process locks prevent duplicate work across sessions. After
host exit, startup invokes summary, then the existing finite action when due.
The `particleflow/windows-endpoint` owner supplies event/timer coalescing, shutdown
and lifecycle integration; this component does not add a worker or service.

Malformed/truncated JSON, an unsupported schema or absent fields must suspend
automatic wakes. This feature does not implement sustained high-file-count
receipt turnover, remote original pinning, physical power-loss guarantees or
the endpoint's restart scheduler. Production installation and trust configuration
are separate from isolated synthetic fixture tests.
