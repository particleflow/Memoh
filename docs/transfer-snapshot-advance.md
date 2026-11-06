# Advancing retained upload snapshots

The installed producer may advance a private, already admitted upload snapshot
without observing or activating a live task source. Use the dedicated capability:

```text
transferqueue.exe -action advance-snapshots
transferqueue.exe -action advance-snapshot -delivery <original-delivery-id>
```

The Go equivalent is `transferclient.AdvanceSnapshotProducer`. The plural command
selects one due delivery for the current exact HostBinding. The singular command
performs one bounded step for the specified delivery. Both recheck eligibility
and the protected snapshot/tree handles under the job lock before recording a
run or issuing a request. Normal upload, receipt reconciliation and NAS commit
retain their existing task identity, authentication, TLS and association rules.

Eligibility requires an upload with `Admitted=true`, a valid retained seal,
`Registered=false`, no `Source`, and no payload release state. A registered job
with an admitted hash index still reads its live source: it is excluded too.
Missing private snapshot bytes fail before network work. Downloads, foreign
bindings, unknown-create fences and released payloads are not admitted by this
capability. It does not collect manifests, open the source root, admit work,
index a file, invoke Pi, perform global reclaim or project producer feedback.
The selected delivery's existing confirmed-byte release remains unchanged.

Every response includes `advance_scope: "retained_upload_snapshots"`. Its `idle`
means only that no eligible retained upload snapshot is due. It is not evidence
that the full queue is empty, that live sources are healthy, or that general host
attention can be cleared. Feedback fields are intentionally absent in this
specific response; do not loosen feedback validation for ordinary actions.

Windows host integration must select this exact capability outside the Herdr
health gate. Keep ordinary `advance-results`, collection, registered-source
activation, indexing and downloads under their existing gates. An older binary
rejects the new action; never fall back to general advancement outside the gate.
The unchanged plugin version alone does not prove capability availability: use
the reviewed source/binary identity. Config, credentials, state roots, original
HostBinding and existing queue records must remain unchanged. This increment
does not convert registered sources into private snapshots or copy their bytes.

Validation uses real private files and isolated HTTPS peers, including a lost
PATCH reply, source removal, unchanged registered journals, indexed source
rejection, missing snapshot bytes, revoked credentials, absent credentials and
untrusted TLS. These are cloud isolation results. Windows NTFS ACL/handle and
the installed host health loop still require the sole field writer's acceptance.
