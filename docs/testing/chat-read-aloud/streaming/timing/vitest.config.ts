import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'
const root = fileURLToPath(new URL('../../../../../', import.meta.url))
export default defineConfig({
  root: `${root}/apps/web`,
  plugins: [vue()],
  resolve: { dedupe: ['vue', 'vee-validate'], alias: [
    { find: /^@memohai\/sdk\/client$/, replacement: `${root}/packages/sdk/src/client.gen.ts` },
    { find: /^@memohai\/sdk$/, replacement: `${root}/packages/sdk/src/index.ts` },
    { find: '@', replacement: `${root}/apps/web/src` },
    { find: '#', replacement: '/workspace/Memoh/packages/ui/src' },
  ] },
  test: { include: ['src/pages/home/composables/speech-timing.test.ts'], globals: true, testTimeout: 15000, server: { deps: { inline: ['reka-ui', /@vueuse\//] } } },
})
