# Harmony 1.0.14 unsigned signing handoff

Generic API26 release HAP and unchanged Windows configuration injector. This is not an installable signed release.

Source commit: 5d04609cd53b3b4cf788f2d0da96459ff99c78eb.
Version: 1.0.14 / 1000014.
The ZIP is 14288849 bytes; verify SHA256SUMS before extracting.
HAP: 35615952 bytes, SHA256 a7472679a36ef2f714e3b2165bae7fab0eb4558b9add16d4cd012d9d3c11ffbe.

Removes the unsolicited attachment recovery controls and reuses shared activity names, state and authorized session navigation. Empty public deployment configuration; no private signing/configuration material.

Windows must inject the authorized private configuration and sign using the existing identity/profile. Never publish that injected/signed private package here. Keep 1.0.13 for recovery. Real-phone acceptance remains pending.
