# Archived isolated fixture sources

These are the executed local fixture scripts, not self-provisioning installation scripts or a field runbook. Do not run them against production. The original private config, access token, local fixture metadata and original contact data are intentionally excluded. The scripts use those pre-existing authorized local files; they do not generate credentials. `start.py` now asserts the recovered isolated database and rejects enabled schedules/active runs. All configured provider URLs are asserted loopback. The local provider refuses HTTP proxy forwarding/CONNECT and never has an upstream transport.

`live/terminal.py` uses a real PostgreSQL row lock (`rowlock.go`) to hold the follow-up before admission while the source reaches completed, then kills only the owned isolated server. It restarts the same binary and reads the exact original invocation. `live/restart.py` records the earlier active-source kill variant. `busy/verify.py` records the independent HTTP duplicate/reconnect plus expert-result-while-B-active seam. Test observation/barrier deadlines are fixture control, not product behavior. Synthetic IDs/marker texts select controlled fixture records; product code has no sample-specific branches.

Files are archived in their final local form. `start.py` was tightened to the replacement database after the migration incident; it is not a claim the red/initial-green tests ran on that replacement. See the evidence table for chronology and exact-commit binding. Do not re-run the quarantined original config, restore private dumps into it, or run global migration down.

The product PostgreSQL tests can be run with an already-authorized disposable database migrated forward to the candidate schema and `MEMOH_FOLLOW_UP_TEST_DSN` supplied only in the process environment:

```sh
go test ./internal/followupqueue -run 'TestPostgresFollowUp' -count=1 -v
go test ./cmd/internal/core -run '^TestDurableFollowUpRecoveryKeepsUnknownAndOriginalLostRun$' -count=1 -v
```

They clean their synthetic queue rows only. Omitting the DSN skips them; the recorded run supplied it and had zero skips. No private DSN, config or token is in the archive.
