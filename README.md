# Garnet Assistant 1.0.14 unsigned signing handoff

Use `garnet-assistant-1.0.14-minimal-navigation-unsigned-handoff.zip`, the final display-name revision. It is a generic unsigned release handoff, not an installable signed package.

Source: ea9746a92c6d9ea773ff131ac877b94f277a0b17. Version: 1.0.14 / 1000014.
ZIP: 14283253 bytes, SHA256 abeea7735a50fb8fdf53144bd13c62245e07fcf0736c0c119387c6bca4a89b6d.
HAP: 35615952 bytes, SHA256 cc065da0c516240b628ac53f9cb1898607b4e48dac501cbec4d88b253359a205.

Verify SHA256SUMS before extraction. Windows private config productName must be `Garnet Assistant`; preserve its existing authorized endpoints, legacy binding, HTTPS migration and all other private fields. Inject and sign only on the existing authorized Windows machine. Never upload the injected/signed private package here.

The older `memoh-harmony-1.0.14-minimal-navigation-unsigned-handoff.zip` is retained for evidence and is superseded; do not sign it. It was built before the explicit display-name correction. Existing signed 1.0.13 remains available for rollback. Real-device acceptance of 1.0.14 remains pending.
