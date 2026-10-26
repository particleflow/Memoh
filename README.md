# Garnet Assistant 1.0.16 unsigned signing handoff

Generic API26 release HAP and unchanged Windows configuration injector. Not a signed installable release.

Source: a2f3c9f6c597f71688c5677dccfcfc2ce659e95e. Approved UI: 591c05fca72626cfc25e4263a56ebc013a02e12a.
Version 1.0.16 / 1000016.
ZIP: 14286195 bytes; verify SHA256SUMS before extraction.
HAP: 35620288 bytes, SHA256 8663e5d352af4326ea4354d066e81d63f1ee9a1624b262ef71ec3e05089a481d.

Task entry uses the existing shared sidebar name/state/original-conversation list; no old detail dialog, image timing probe or new directory features. Public deployment slot is empty.

Full cross-Bot activity depends on production GET /bots/{bot_id}/activity. The shared4d image is published but production deployment/readback was still waiting_decision at build handoff. This is not full phone acceptance and local sidebar timing is not a device result.

On the authorized Windows machine retain the existing private config, productName Garnet Assistant, approved endpoint/legacy binding/HTTPS fields, original bundle/permissions/profile/device. Inject BEFORE signing. Never publish the privately injected or signed output here; use the established personal distribution channel. Existing packages are not overwritten.
