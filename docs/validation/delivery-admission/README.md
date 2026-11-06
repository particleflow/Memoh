# Admission recovery evidence

Base: PR #13 `c616db12747753156c3a61e34bc2b02a17f01e6b`.
This increment fixes partial admission; native Windows binding remains separate.

- `before.txt`: both regressions fail on the base: same-ID resume and releasing
  a canceled reservation.
- `race.txt`: complete transfer package with race detector and `pr8contract`
  tag. New tests cover 130 MiB over three finite actions, real expired deadline,
  controlled deadlines after progress, 100 canceled retries with unchanged
  journal size, process exit after checkpoint plus unacknowledged tail, same-ID
  conflict/idempotency, torn slots, corrupt progress, changed source/tree/identity,
  tree/finalization recovery, explicit legacy refusal, cancel and quota release.
- `benchmark.txt`: three 512 MiB admissions, each reopening the queue for eight
  64 MiB actions. Includes private copy, SHA, Merkle construction, 8 MiB progress
  flushes, two-slot metadata, queue reopen and final seal. 1.43–2.38 seconds in
  this cached Linux cloud filesystem; total allocations 8.7–8.8 MB across all
  actions (not peak RSS). No HTTP/NAS/NIC/Windows throughput claim.
- `handlers.txt`: real merged upload handler integration, synthetic JWT/TLS/
  task bindings only. No production identity or remote PC/NAS used.

Reproduce with Go 1.25.7:

```sh
go test -race -tags pr8contract -count=1 -v ./internal/transferclient
go test -race -count=1 -run 'TestPR10ClientAgainstIntegratedPR8Handlers|TestHerdrActionAgainstMergedUploadHandlers' ./internal/handlers
go test -run '^$' -bench '^BenchmarkDurableAdmissionResume$' -benchtime=1x -count=3 ./internal/transferclient
```

The completed package run has 40 top-level tests and 62 subtests.

The two-minute invocation timeout remains. Disk syscalls can exceed a canceled
context; no hard real-time bound is claimed. No background retry loop is added.
A caller repeats the same enqueue action when `ErrAdmissionPending` is returned.
Interrupted admission reports the reserved logical status with its error. A
newly admitted replay is a no-op. A hash mismatch requires explicit local cancel
and retirement before starting a new delivery; it never becomes upload-ready.

The fixed 8 KiB admission file and tree are quota-accounted and removed before
the reservation journal. A corrupt progress file does not silently reset; the
valid unadmitted journal still permits local cancellation and cleanup. Old
unadmitted records with no admission protocol version return `ErrVersion` on
resume and support cancellation/retirement, with no implicit migration. Older binaries reject the new optional journal field;
do not downgrade a queue after using the new admission protocol.

UI/development-stack evidence remains missing: the task forbids installing or
connecting the Memoh runtime, and the cloud environment has no mise. This is not
a waiver of AGENTS.md or human QA. Keep the PR draft until required acceptance.
