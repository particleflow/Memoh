# Harmony model label and local catalog source alignment

Shared candidate `7af02cb35f85582713213cb98c382d36df744ea3`; source base is the original 1.0.20 build commit `c4edeab2083246c55ad997723f2ff44031026f92`. This is source-only composition, not a new HAP or deployed model catalog.

Reuse the exact shared selected-model label, original local model lookup and 2-minute visible-app cache invalidation. A disabled/missing selection keeps its identity, displays its name as unavailable when known, and never adds that row back to the menu. No Bot-creation workflow, configuration rewrite, inference request, service or new endpoint is introduced. The generated Web differs from 1.0.20 in exactly ten expected App/model/locale/composer files; fourteen protected navigation, attachment, login/TLS, image-timing, theme and configuration files plus the dependency lock are byte-identical. Existing single-button adaptations remain.

Four affected unit tests passed; generated Web typecheck and build passed. Lint has zero errors and 26 inherited phone composer indentation warnings. The 45+2 adapter guard and 167-entry source check passed. Actual compiled generated Web was exercised at 390×844 with isolated synthetic API responses: [the unavailable model displayed its name without its internal UUID](model-unavailable-390.png), with no business writes or provider inference. [Numeric proof](model-catalog-browser.json) is synthetic only; no production catalog or automatic synchronization was exercised.

Generated SDK code is unchanged between the earlier shared baseline and 7af; its changed comments indicate that the interactive Agent follow-up route reads an existing receipt and does not support new requests. This source-only model task does not adjust that separate integration or claim that earlier follow-up behavior was verified against 7af. The independent Bot-creation owner retains that scope.

Original 1.0.20 unsigned artifact and SHA remain unchanged. No HAP/ArkTS compilation, signing, deployment, real-device acceptance or production synchronization is claimed here. The previously unknown 19 historical modifications remain unverified.
