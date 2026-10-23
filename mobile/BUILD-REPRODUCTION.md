# Reproduction inputs and limits

The original public-subset status below is historical. The
[archive restoration checkpoint](../docs/harmony-archive-restoration.md) recovers
all 72 excluded paths and two additional tests. Integration of that older
builder/native shell with retained newer modules and target `40f0a69` is still
required; restored path coverage alone is not a successful build.

A clean checkout of this public source subset can run only its source/syntax
checker and standalone configuration regression. It cannot rebuild the full Web
or HAP without the 72 excluded inherited source paths listed in the provenance
register, including the overlay builder, native shell, packaging/toolchain
helpers and assets. The private full source checkpoint has a clean-export runner;
that runner is not advertised as functional from this incomplete public subset.

Additional full-build inputs are not vendored here:

- Python 3.12, Node 22.23.3, pnpm 10.27.0 and the dependencies matching the source
  lockfile. Existing clean tests used a fixed offline store and disabled install
  scripts; a cache alone is not a portable source package.
- Memoh `6ddf61dcd8199758e4110398645c556a3d6d7375`, archive SHA256
  `b92b9665acafd5a10ed8fb0b0dc6f51b465046950b1dfcd9a38eb991e3157f58`.
- UI `f69402a55dd87a59c10722fe11507ad8fd7048f5`, archive SHA256
  `55199ba9ad385c32f574906bda4fd43f21c106b6acb58f9e977d0da27222a4a9`.
  Combined archive SHA256:
  `473c5956c9691b54b5a4151ef0b9cececb1dc406a3076f7401abd744a36152f1`.
- Shared Git objects at `be6862497a58a01ef8c6f4168f02d5624cd44061`.
  `prepare_shared_observation.py` verifies inputs and rejects drift instead of
  falling back. Existing full build invokes it after a fresh baseline extraction.
- Playwright 1.62.0, Chromium 154.0.8037.57 and separately supplied historical
  compiled 1.0.3, 1.0.6 and runtime-config r1 fixtures for browser regressions.
  These fixtures may require private local configuration and are not in Git.
- Official Linux x64 CLT 26.0.0.851, SHA256
  `ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`,
  containing SDK 26.0.0.105 / API26, and LibRaw 0.22.2 archive SHA256
  `de86b035655accff8d4010f1a221fdf50d353cb7b1422ba26f14a0db92612cfa`.
  These require official acquisition and applicable terms; no signed download
  links, vendor binaries or credentials are copied.

The actual adapted-Web typecheck was `pnpm --filter @memohai/web typecheck`,
which expands to `vue-tsc --noEmit -p tsconfig.app.json` in `apps/web`. It does
not cover every workspace project, ArkTS, the Node/Vite project or raw upstream
Web. Passing it does not resolve the separately reported raw Web baseline issues.
