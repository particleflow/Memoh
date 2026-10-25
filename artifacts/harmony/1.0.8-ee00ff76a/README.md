# Harmony 1.0.8 generic unsigned handoff

This file is the generic, **unsigned** API26 package. It is not directly installable.
Source: `ee00ff76a07ac27e08f4b4f58fd1354f0ecbeef3`.
Shared Web/SDK: `68be06092a1ea85d961115aac404082982bdecf4`.
Bundle: `cloud.begonia.workassistant`; version: `1.0.8 / 1000008`.
Size: 37,029,849 bytes. See `SHA256SUMS` for the exact public bytes.

The packaged configuration is empty. On the original Windows signing machine,
use `mobile/client/mobile/client_config.py` from the exact source commit to
inject the existing private configuration and independently verified old binding
into a separate unsigned output, then use the existing official signing flow.
[Configuration fields, validation and invocation](../../../docs/testing/harmony-service-migration/README.md).
Do not publish the configured/signed private derivative, private configuration,
old binding values or signing material. Do not change the existing app identity,
profile devices, permissions or stored phone data. Keep the original public hash
separate from the new configured/signed hashes.

Official API26 compilation and package integrity checks passed. Synthetic browser
migration and an injection into the actual compiled HAP passed. Actual Windows
injection/signing and phone overwrite-install acceptance remain to be performed
by the original delivery task. The old 1.0.7 artifact is retained unchanged.
