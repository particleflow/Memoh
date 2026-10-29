import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
const root = fileURLToPath(new URL('../../../../../', import.meta.url))
const before = process.env.AUTOPLAY_BEFORE === '1'
const beforePath = '/tmp/nas-autoplay-resume-before.ts'
if (before) {
  const pinned = execFileSync('git', ['show', '65d5d4ad128317b65f394bd27cb4b674a4f6bbae:apps/web/src/pages/home/composables/useChatReadAloud.ts'], { cwd: root, encoding: 'utf8' })
  // Only resolve relative imports for the temporary module; behavior is pinned.
  writeFileSync(beforePath, pinned.replaceAll("from './", "from '@/pages/home/composables/"))
}
export default defineConfig({
  root: `${root}/apps/web`,
  plugins: [vue()],
  resolve: { dedupe: ['vue', 'vee-validate'], alias: [
    { find: /^@memohai\/sdk\/client$/, replacement: `${root}/packages/sdk/src/client.gen.ts` },
    { find: /^@memohai\/sdk$/, replacement: `${root}/packages/sdk/src/index.ts` },
    { find: '@autoplay-reviewed-composable', replacement: before ? beforePath : `${root}/apps/web/src/pages/home/composables/useChatReadAloud.ts` },
    { find: '@', replacement: `${root}/apps/web/src` },
    { find: '#', replacement: '/workspace/Memoh/packages/ui/src' },
  ] },
  test: { include: ['src/pages/home/composables/useChatReadAloud.resume-gesture.test.ts'], globals: true, testTimeout: 15000, server: { deps: { inline: ['reka-ui', /@vueuse\//] } } },
})
