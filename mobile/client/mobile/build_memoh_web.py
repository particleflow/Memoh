"""Build the official Memoh Vue release for Harmony or the browser.

The complete release includes its selected UI submodule. pnpm dependencies and
links stay inside the local official Docker builder; no source is sent to NAS.
"""
import argparse
import copy
import hashlib
import html
import io
import json
from pathlib import Path
import subprocess
import shutil
import os
import sys
import tarfile
import uuid
from client_config import read_config, public_deployment_bytes

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'tools'))
from artifact_paths import checked
BASE = ROOT / 'mobile/.build/memoh-web'
DIST = BASE / 'official-dist'
PIN = ROOT / 'config/memoh_web.lock.json'
NOTICES = {
    'lucide.txt': 'lucide-voice.txt', 'wavesurfer.txt': 'wavesurfer.txt',
    'noble-hashes.txt': 'noble-hashes.txt', 'inter.txt': 'inter.txt',
    'instrument-sans.txt': 'instrument-sans.txt', 'katex.txt': 'katex.txt',
    'seti.txt': 'seti.txt', 'misans-package.txt': 'misans-package.txt',
    'misans-original-license.txt': 'misans-original-license.txt',
    'llvm-libcxx.txt': 'llvm-libcxx.txt', 'llvm-libcxxabi.txt': 'llvm-libcxxabi.txt',
    'sources.txt': 'sources.txt',
}


def sha(path):
    return hashlib.sha256(checked(path).read_bytes()).hexdigest()


def inputs():
    files = [ROOT / name for name in (
        'mobile/build_memoh_web.py', 'mobile/memoh/harmony.ts', 'mobile/memoh/harmony-files.ts',
        'mobile/memoh/harmony-image-timing.ts', 'mobile/memoh/harmony-image-timing.test.ts', 'mobile/memoh/harmony-image-timing-node.test.ts', 'mobile/memoh/harmony-attachment-preparation.test.ts',
        'mobile/memoh/harmony-voice.ts', 'mobile/memoh/harmony-voice-geometry.ts',
        'mobile/memoh/HarmonyVoiceInput.vue', 'mobile/memoh/HarmonyVoiceFan.vue', 'mobile/memoh/HarmonyVoicePlayer.vue',
        'mobile/memoh/HarmonyAttachments.vue', 'mobile/memoh/harmony-resumable.ts',
        'mobile/memoh/voiceFan.svg', 'mobile/memoh/voiceFan.css', 'contracts/ui_theme.json',
        'config/memoh_web.lock.json', 'config/memoh_client.json',
        'mobile/client_config.py', 'mobile/prepare_memoh_source.py',
        'mobile/memoh/harmony-download.ts', 'mobile/memoh/harmony-service-url.ts',
        'mobile/memoh/harmony-entry.ts', 'mobile/memoh/harmony-back.ts', 'mobile/memoh/harmony-service-config.ts', 'mobile/memoh/harmony-service.css',
        'tests/stage2/harmony_task_attachments.vitest.ts', 'tests/stage2/harmony_task_list_attachment.vitest.ts')]
    files.extend(ROOT / 'mobile/memoh' / name for name in ('shared-session-activity.vue', 'shared-session-activity.test.ts', 'shared-useSessionActivity.ts'))
    files.extend(ROOT / 'mobile/memoh' / name for name in ('shared-useBotActivity.ts', 'shared-useBotActivity.test.ts', 'shared-activity-sdk.json'))
    files.extend(ROOT / 'mobile/memoh' / name for name in (
        'shared-conversation-monitor-trigger.vue', 'shared-conversation-monitor-trigger.test.ts',
        'shared-useConversationRelations.ts', 'shared-useConversationRelations.test.ts',
        'shared-conversation-relation-row.vue', 'shared-composer-error.ts', 'shared-composer-error.test.ts',
        'shared-conversation-store-changes.json',
        'shared-conversation-chat-pane-changes.json', 'shared-conversation-phone-test-changes.json'))
    files.extend(ROOT / 'mobile/memoh' / name for name in (
        'shared-workspace-storage.vue', 'shared-workspace-storage.test.ts',
        'shared-workspace-directory-picker.vue', 'shared-workspace-routes.ts',
        'shared-workspace-settings-sidebar.vue', 'shared-workspace-storage-sdk.json',
        'shared-workspace-storage-i18n.json'))
    files.extend(ROOT / 'mobile/memoh' / name for name in ('shared-useAcceptedUserInputRecovery.ts', 'shared-useAcceptedUserInputRecovery.test.ts', 'shared-accepted-input-sdk.json', 'shared-accepted-input-i18n.json'))
    files.extend(ROOT / 'mobile/memoh/licenses' / name for name in NOTICES)
    return {p.relative_to(ROOT).as_posix(): sha(p) for p in files}


def package_notices(dist, memoh_license):
    """Copy reviewed notices verbatim; include them in the normal resource map."""
    licenses = checked(dist / 'licenses')
    licenses.mkdir(exist_ok=True)
    checked(licenses / 'memoh.txt').write_bytes(memoh_license)
    for source, name in NOTICES.items():
        checked(licenses / name).write_bytes(
            checked(ROOT / 'mobile/memoh/licenses' / source).read_bytes())


def verify_modules(modules):
    lock = json.loads(PIN.read_text())
    assert modules == {lock['archive']: lock['archiveSHA256']}, 'Use the complete official release source'
    assert all(sha(ROOT / name) == digest for name, digest in modules.items()), 'Upstream archive drift'


def context_archive(lock, base=BASE, platform='harmony'):
    entry_name = json.loads((ROOT / 'config/memoh_client.json').read_text())['productName'] or 'Memoh'
    assert isinstance(entry_name, str) and entry_name.strip(), 'Product entry name is required'
    warm = json.loads((ROOT / 'contracts/ui_theme.json').read_text())['themes']['warm-theme']
    colors = warm['colors']
    # Map the centralized warm surfaces onto the existing official semantic tokens.
    # The independent upstream accent/color-scheme selection remains unchanged.
    warm_tokens = {
        'background': colors['Background']['Container'],
        'background-chrome': colors['SurfaceVariant']['Container'],
        'foreground': colors['Surface']['OnContainer'],
        'card': colors['Surface']['Container'],
        'card-foreground': colors['Surface']['OnContainer'],
        'secondary': colors['SurfaceVariant']['Container'],
        'secondary-foreground': colors['SurfaceVariant']['OnContainer'],
        'muted': colors['SurfaceVariant']['Container'],
        'muted-foreground': warm['muted'],
        'accent': colors['Background']['ContainerHover'],
        'accent-foreground': colors['Background']['OnContainer'],
        'border': colors['Surface']['ContainerLine'],
        'input': colors['Surface']['ContainerLine'],
        'sidebar': colors['Background']['Container'],
        'sidebar-foreground': colors['Background']['OnContainer'],
        'sidebar-accent': colors['Background']['ContainerHover'],
        'sidebar-accent-foreground': colors['Background']['OnContainer'],
        'sidebar-border': colors['Background']['ContainerLine'],
        'surface-editor': colors['Surface']['Container'],
        'surface-chrome': colors['SurfaceVariant']['Container'],
        'surface-composer': colors['Surface']['Container'],
        'workspace-divider-color': colors['SurfaceVariant']['ContainerLine'],
    }
    warm_css = '\n:root.warm[data-color-scheme] {\n  color-scheme: light;\n' + ''.join(
        '  --' + name + ': ' + value + ';\n' for name, value in warm_tokens.items()) + '}\n'
    changes = {
        'apps/web/src/pages/home/components/thinking-block.vue': [
            ('''// Persisted, user-driven toggle (survives the post-turn refetch/remount).
const collapseKey = computed(() => reasoningCollapseKey(props.messageId, props.block))
const open = ref(getCollapseOpen(collapseKey.value) ?? false)
watch(collapseKey, (key) => {
  open.value = getCollapseOpen(key) ?? false
})''', '''// Live text opens in the original page flow. Keep live toggles local so a
// terminal refetch/remount lands collapsed; historical toggles stay persisted.
const collapseKey = computed(() => reasoningCollapseKey(props.messageId, props.block))
const open = ref(false)
watch([collapseKey, () => props.streaming], ([key, streaming]) => {
  if (streaming) {
    open.value = true
    setCollapseOpen(key, false)
  } else {
    open.value = getCollapseOpen(key) ?? false
  }
}, { immediate: true })''', 1),
            ('''  open.value = !open.value
  setCollapseOpen(collapseKey.value, open.value)''', '''  open.value = !open.value
  if (!props.streaming) setCollapseOpen(collapseKey.value, open.value)''', 1),
        ],
        'apps/web/src/pages/home/components/tool-call-group.vue': [
            ('''// Open state is purely user-driven and persisted across the post-turn refetch:
// a process is collapsed until the user opens it, then stays as they left it
// (no auto-open while streaming, no auto-close on completion). The header
// tracks progress live while streaming, without forcing the body open.
const collapseKey = computed(() => groupCollapseKey(props.messageId, props.items))
const open = ref(getCollapseOpen(collapseKey.value) ?? false)
watch(collapseKey, (key) => {
  open.value = getCollapseOpen(key) ?? false
})''', '''// Reveal a live reasoning tail through the original group disclosure; tool-only
// and historical groups retain their user-driven state and original page flow.
const liveThinking = computed(() => props.active === true
  && props.items[props.items.length - 1]?.type === 'reasoning')
const collapseKey = computed(() => groupCollapseKey(props.messageId, props.items))
const open = ref(false)
watch([collapseKey, liveThinking], ([key, thinking]) => {
  if (thinking) {
    open.value = true
    setCollapseOpen(key, false)
  } else {
    open.value = getCollapseOpen(key) ?? false
  }
}, { immediate: true })''', 1),
            ('''  open.value = !open.value
  setCollapseOpen(collapseKey.value, open.value)''', '''  open.value = !open.value
  if (!liveThinking.value) setCollapseOpen(collapseKey.value, open.value)''', 1),
        ],
        'apps/web/src/pages/home/components/process-collapse.ts': [
            ('''// Open/closed state for chain-of-process collapsibles (process groups, single
// tool detail rows, thinking blocks).
//
// The whole assistant turn is re-fetched and re-mounted once it finishes
// streaming, which would otherwise discard any expand/collapse the user did
// mid-stream (the classic "I opened it, then the turn ended and it snapped
// shut"). We keep the toggle here, keyed by a *stable* signature of the block
// (assistant render id + backend block id) so the re-mounted "done" component
// recovers exactly what the user left open without confusing mutable content
// for identity.
//
// Semantics: purely user-driven. Nothing auto-opens on stream start or
// auto-closes on completion — a CoP is collapsed until the user opens it, and
// then stays however they left it for the life of the session. Cross-reload it
// resets to collapsed (acceptable; matches "reduce info, focus on output").''', '''// UI-only disclosure state for process groups, tool details and thinking blocks.
// Stable assistant render/block identities preserve tool and historical user
// toggles across the post-turn refetch without using mutable text as identity.
// Live thinking and groups with a live reasoning tail expand locally and clear
// stored openness so the terminal refetch lands collapsed. Cross-reload state
// resets to collapsed.''', 1),
        ],
        'apps/web/index.html': [
            ('<title>Memoh</title>', '<title>' + html.escape(entry_name) + '</title>', 1),
        ],
        'apps/web/src/pages/home/index.vue': [
            ('useTitle(() => {', 'const appTitle = ' + json.dumps(entry_name) + '\nuseTitle(() => {', 1),
            ("if (!isChatRoute() || !currentBotId.value || !session || chatStore.isSessionTentative(session.id)) return 'Memoh'",
             'if (!isChatRoute() || !currentBotId.value || !session || chatStore.isSessionTentative(session.id)) return appTitle', 1),
            ('return `Memoh · ${title}`', 'return `${appTitle} · ${title}`', 1),
            ("restoreOnUnmount: () => 'Memoh'", 'restoreOnUnmount: () => appTitle', 1),
        ],
        'apps/web/src/style.css': [
            ('/* Shell-divider owners outside dockview.', warm_css + '\n/* Shell-divider owners outside dockview.', 1),
        ],

        'apps/web/src/store/settings/index.ts': [
            ("export type ThemePreference = 'light' | 'dark' | 'system'",
             "export type ThemePreference = 'light' | 'dark' | 'warm' | 'system'", 1),
            ('useColorMode({ emitAuto: true })', "useColorMode({ emitAuto: true, modes: { warm: 'warm' } })", 1),
            ('computed(() => colorMode.state.value)', "computed(() => colorMode.state.value === 'dark' ? 'dark' : 'light')", 1),
        ],
        'apps/web/src/App.vue': [
            ("import { computed } from 'vue'", "import { computed, watch } from 'vue'", 1),
            ("import MainSection from '@/pages/main-section/index.vue'",
             "import MainSection from '@/pages/main-section/index.vue'\nimport { publishTheme } from '@/lib/harmony'", 1),
            ('useSettingsStore()\n', '''const settingsStore = useSettingsStore()
watch(() => settingsStore.theme === 'warm' ? 'warm' : settingsStore.resolvedColorMode,
  publishTheme, { immediate: true })
''', 1),
        ],
        'apps/web/src/pages/appearance/index.vue': [
            ('{ Monitor, Moon, SlidersHorizontal, Sun }', '{ Monitor, Moon, SlidersHorizontal, Sun, SunDim }', 1),
            ("import { isMermaidTheme } from '@/store/settings/mermaid'",
             "import { isMermaidTheme } from '@/store/settings/mermaid'\nimport uiTheme from '@/lib/harmony-theme.json'", 1),
            ('const { t } = useI18n()', 'const { t, locale } = useI18n()', 1),
            ('''const themeItems: SegmentedItem<ThemePreference>[] = [
  { value: 'system' },
  { value: 'light' },
  { value: 'dark' },
]''', '''const themeItems = computed<SegmentedItem<ThemePreference>[]>(() => {
  const language = locale.value.startsWith('zh') ? 'zh-CN' : 'en'
  return [
    { value: 'system', label: uiTheme.labels[language].system },
    { value: 'light', label: uiTheme.themes['light-theme'].name[language] },
    { value: 'dark', label: uiTheme.themes['dark-theme'].name[language] },
    { value: 'warm', label: uiTheme.themes['warm-theme'].name[language] },
  ]
})''', 1),
            ('  dark: Moon,', '  dark: Moon,\n  warm: SunDim,', 1),
            (''':is="themeIcons[item.value]"
                class="size-4"
              />''', ''':is="themeIcons[item.value]"
                class="size-4"
                :title="item.label"
                :aria-label="item.label"
              />
              <span class="sr-only">{{ item.label }}</span>''', 1),
        ],
        'apps/web/src/main.ts': [("import router from './router'", "import router from './router'\nimport './lib/harmony'", 1)],
        'apps/web/src/router.ts': [('createWebHistory', 'createWebHashHistory', 2)],
        'apps/web/src/pages/login/login-submit.ts': [
            ("import type { UserInfo } from '@/store/user'", "import type { UserInfo } from '@/store/user'\nimport { apiErrorStatus } from '@/utils/api-error'", 1),
            ('  notifyInvalidCredentials: () => void', '  notifyInvalidCredentials: () => void\n  notifyLoginFailed: (error: unknown) => void\n  notifyInvalidResponse: () => void', 1),
            ('''    } catch {
      dependencies.notifyInvalidCredentials()
      return false
    }

    if (!data?.access_token || !data.user_id) {
      dependencies.notifyInvalidCredentials()''', '''    } catch (error) {
      const status = (error as { response?: { status?: number } } | null)?.response?.status ?? apiErrorStatus(error)
      if (status === 401) dependencies.notifyInvalidCredentials()
      else dependencies.notifyLoginFailed(error)
      return false
    }

    if (!data?.access_token || !data.user_id) {
      dependencies.notifyInvalidResponse()''', 1),
        ],
        'apps/web/src/pages/login/index.vue': [
            ("import { resolveApiErrorMessage } from '@/utils/api-error'", "import { apiErrorStatus, resolveApiErrorMessage } from '@/utils/api-error'", 1),
            ('authenticate: (body) => postAuthLogin({ body })', '''authenticate: async (body) => {
        const result = await postAuthLogin({ body })
        if (!result.response?.ok) throw { response: result.response, error: result.error }
        return result
      }''', 1),
            ("toast.error(t('auth.invalidCredentials'), {", "toast.error(t('auth.loginRejected'), {", 1),
            ("description: t('auth.retryHint'),", "description: t('auth.loginRejectedHint'),", 1),
            ('''      notifyInvalidCredentials: () => {''', '''      notifyLoginFailed: (error) => {
        toast.error(apiErrorStatus(error) === undefined
          ? t('auth.connectionFailed')
          : resolveApiErrorMessage(error, t('auth.loginFailed')))
      },
      notifyInvalidResponse: () => toast.error(t('auth.invalidResponse')),
      notifyInvalidCredentials: () => {''', 1),
        ],
        'apps/web/src/pages/login/login-submit.test.ts': [
            ('    notifyInvalidCredentials: vi.fn(),', '    notifyInvalidCredentials: vi.fn(),\n    notifyLoginFailed: vi.fn(),\n    notifyInvalidResponse: vi.fn(),', 1),
            ("it('notifies and releases the form when authentication fails'", "it('reports an invalid response and releases the form'", 1),
            ('expect(dependencies.notifyInvalidCredentials).toHaveBeenCalledTimes(1)', 'expect(dependencies.notifyInvalidResponse).toHaveBeenCalledTimes(1)\n    expect(dependencies.notifyInvalidCredentials).not.toHaveBeenCalled()', 1),
            ("  it('reports the browser timezone after login and applies the server-confirmed value'", '''  it.each([[500, 401, 'http.unauthorized'], [401, 500, 'internal']] as const)(
    'uses HTTP %i before conflicting body status %i', async (status, bodyStatus, code) => {
      const isSubmitting = ref(false)
      const error = { response: { status }, error: { code, status: bodyStatus } }
      const dependencies = createDependencies({ authenticate: vi.fn().mockRejectedValue(error) })
      await expect(submitLogin({ username: 'alice', password: 'secret' }, isSubmitting, dependencies)).resolves.toBe(false)
      expect(dependencies.notifyInvalidCredentials).toHaveBeenCalledTimes(status === 401 ? 1 : 0)
      expect(dependencies.notifyLoginFailed).toHaveBeenCalledTimes(status === 401 ? 0 : 1)
      if (status !== 401) expect(dependencies.notifyLoginFailed).toHaveBeenCalledWith(error)
      expect(dependencies.applyLogin).not.toHaveBeenCalled()
      expect(isSubmitting.value).toBe(false)
    },
  )

  it('reports the browser timezone after login and applies the server-confirmed value' ''', 1),
        ],
        'apps/web/vite.config.ts': [('  return {\n    plugins:', "  return {\n    base: './',\n    plugins:", 1)],
        'apps/web/src/pages/home/components/chat-pane.vue': [(':open-index="galleryOpenIndex"', ':open-index="galleryOpenIndex"\n        :origin="galleryOrigin"', 1), (':open-index="composerPreviewIndex"', ':open-index="composerPreviewIndex"\n        :origin="composerPreviewOrigin"', 1), ('  openBySrc: galleryOpenBySrc,', '  openBySrc: galleryOpenBySrc,\n  origin: galleryOrigin,', 1), ('  composerPreviewIndex,', '  composerPreviewIndex,\n  composerPreviewOrigin,', 1), ('const paneView = computed(() => chatStore.chatView(paneTarget.value))', 'const paneView = computed(() => chatStore.chatView(paneTarget.value))\nconst attachmentPreparing = computed(() => !!paneView.value.attachmentPreparation.value)', 1), (':disabled="!currentBotId || activeChatReadOnly || loadingMessages || firstSendAwaiting || attachmentUploading"', ':disabled="!currentBotId || activeChatReadOnly || loadingMessages || firstSendAwaiting || attachmentUploading || attachmentPreparing"', 1), ('    && isActive.value && isVisible.value,\n  onPaste: handlePaste,', '    && isActive.value && isVisible.value && !attachmentPreparing.value,\n  onPaste: handlePaste,', 1), ('function handleSendButton() {', 'function handleSendButton() {\n  if (paneView.value.attachmentPreparation.value && !showSend.value) {\n    paneView.value.attachmentPreparation.value.abort()\n    return\n  }', 1), ('            <input\n              ref="fileInput"', '            <input ref="harmonyPhotoInput" type="file" accept="image/*" multiple class="hidden" @change="handleFileInputChange">\n            <input\n              ref="fileInput"', 1), ('                    <DropdownMenuItem\n                      :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages"\n                      @select="fileInput?.click()"', '                    <DropdownMenuItem\n                      :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages"\n                      @select="harmonyPhotoInput?.click()"\n                    >\n                      <ImagePlus />\n                      <span class="min-w-0 flex-1 truncate">{{ $t(\'chat.attachPhotos\') }}</span>\n                    </DropdownMenuItem>\n                    <DropdownMenuItem\n                      :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages"\n                      @select="fileInput?.click()"', 1), ('const { t, te, locale } = useI18n()', 'const harmonyPhotoInput = ref<HTMLInputElement>()\nconst { t, te, locale } = useI18n()', 1), (', type ChatAttachment, type CommandActionListItem', ', type CommandActionListItem', 1), ('import { ATTACHMENT_ANIM_MS,', "import { prepareSubmission } from '@/lib/harmony-files'\nimport { nativeVoiceAvailable, type VoiceSubmission } from '@/lib/harmony-voice'\nimport HarmonyVoiceInput from '@/lib/HarmonyVoiceInput.vue'\nimport HarmonyVoicePlayer from '@/lib/HarmonyVoicePlayer.vue'\nimport { ATTACHMENT_ANIM_MS,", 1), ('                        <ChatAttachmentCard\n                          v-for="preview in pendingPreviews"\n                          :key="preview.key"', '                        <template v-for="preview in pendingPreviews" :key="preview.key">\n                        <HarmonyVoicePlayer v-if="preview.file.type.startsWith(\'audio/\')" :file="preview.file" removable @remove="removeAttachment(preview.i)" />\n                        <ChatAttachmentCard\n                          v-else', 1), ('                          @preview="preview.isPasted ? (pastedViewerText = preview.pastedText) : openComposerPreview(preview.url)"\n                        />', '                          @preview="preview.isPasted ? (pastedViewerText = preview.pastedText) : openComposerPreview(preview.url)"\n                        />\n                        </template>', 1), (':clickable="preview.isPasted || (preview.isMedia && !!preview.url)"', ':clickable="preview.isPasted || preview.isText || (preview.isMedia && !!preview.url)"', 1), ('openComposerPreview(preview.url)"', 'openComposerPreview(preview.url, $event, preview.file).catch(error => { composerError = error instanceof Error ? error.message : String(error) })"', 1), ("{{ $t('chat.pastedViewerTitle') }}", "{{ $t('common.preview') }}", 1), ('async function handleSend() {', 'function harmonyVoiceEditorTarget() {\n  const resolve = () => {\n    const node = textareaEl.value\n    if (!node) return\n    const rect = node.getBoundingClientRect()\n    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height, radius: Number.parseFloat(getComputedStyle(node).borderRadius) || 0 }\n  }\n  const bounds = resolve()\n  return bounds ? { ...bounds, resolve } : undefined\n}\nfunction insertHarmonyVoiceText(text: string) {\n  const field = textareaEl.value\n  if (!field || !text.trim()) return\n  const start = field.selectionStart, end = field.selectionEnd\n  inputText.value = inputText.value.slice(0, start) + text + inputText.value.slice(end)\n  void nextTick(() => field.setSelectionRange(start + text.length, start + text.length))\n}\nasync function handleSend() { return sendComposer() }\nasync function sendComposer(voice?: VoiceSubmission) {\n  if (paneView.value.attachmentPreparation.value) return', 1), ('(!text && !files.length && !skills.length)', '(!text && !files.length && !skills.length && !voice)', 1), ("  let attachments: ChatAttachment[] | undefined\n  try {\n    if (files.length) {\n      attachments = await uploadAttachments(sentContext.target.botId, files)\n    }\n  } catch (error) {\n    pairSend.releaseReads()\n    draftSendStarting.value = false\n    if (!matchesChatPaneSendContext(\n      sentContext,\n      paneTarget.value,\n      inputDraftKey.value || 'chat',\n    )) return\n    // The draft remains visible throughout upload; cancellation must not\n    // resurrect a removed file or overwrite a changed composer.\n    if (error instanceof DOMException && error.name === 'AbortError') return\n    composerError.value = error instanceof AttachmentUploadLimitError\n      ? t('chat.attachmentUploadTooLarge', { name: error.fileName, limit: error.maxBytes.toLocaleString() })\n      : resolveApiErrorMessage(error, t('chat.sendFailed'))\n    return\n  }\n", '  preparingView = paneView.value\n  const nativeSubmission = prepareSubmission(sentContext.target.botId, files, voice)\n  const attachments = nativeSubmission.attachments\n', 1), ('const result = await chatStore.sendMessage(sendText, attachments, {', 'const result = await chatStore.sendMessage(sendText, attachments, {\n    prepareAttachments: nativeSubmission.prepareAttachments,', 1), ("    pendingFiles.value = files\n    requestedSkills.value = skills\n    if (commandPanelEvent.value?.type !== 'command_error')", "    pendingFiles.value = nativeSubmission.files\n    requestedSkills.value = skills\n    if (commandPanelEvent.value?.type !== 'command_error')", 1), ('                  <!-- While voice owns the composer', '                  <HarmonyVoiceInput\n                    v-if="nativeVoiceAvailable"\n                    :key="inputDraftKey"\n                    :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages || composerConfigPending || composerHasNoModel || goalSubmissionBlocked || !!runtimeModeUnavailableReason"\n                    :editor-target="harmonyVoiceEditorTarget"\n                    :send="sendComposer"\n                    @transcribe="insertHarmonyVoiceText"\n                    @error="composerError = $event instanceof Error ? $event.message : $t(\'chat.sendFailed\')"\n                  />\n                  <!-- While voice owns the composer', 1), ('                    v-else\n                    class="relative size-8 max-md:size-11 shrink-0"', '                    v-else\n                    v-show="!nativeVoiceAvailable || !inactiveSlotVisible"\n                    class="relative size-8 max-md:size-11 shrink-0"', 1),       ('  const pairSend = composerPair.captureSend()\n  const sentModelId', '  const sentInput = inputText.value\n  const sentField = textareaEl.value\n  const sentSelection = sentField ? {\n    start: sentField.selectionStart,\n    end: sentField.selectionEnd,\n    direction: sentField.selectionDirection,\n  } : null\n  const pairSend = composerPair.captureSend()\n  const sentModelId', 1), ('    inputText.value = restore.input\n    saveInputDraft(sentDraftKey, restore.input)', '    inputText.value = sentInput\n    saveInputDraft(sentDraftKey, sentInput)', 1), ('      composerError.value = restore.error\n    }\n    return', "      composerError.value = restore.error\n    }\n    await nextTick()\n    if (sentSelection && inputText.value === sentInput\n      && matchesChatPaneSendContext(sentContext, paneTarget.value, inputDraftKey.value || 'chat')) {\n      textareaEl.value?.setSelectionRange(sentSelection.start, sentSelection.end, sentSelection.direction)\n    }\n    return", 1), ("import { AttachmentUploadLimitError, useComposerUpload } from '../composables/useComposerUpload'", '', 1), ('const {\n  uploading: attachmentUploading,\n  upload: uploadAttachments,\n  cancel: cancelAttachmentUpload,\n} = useComposerUpload()', 'let preparingView: ReturnType<typeof chatStore.chatView> | undefined\nconst attachmentUploading = computed(() => !!preparingView?.attachmentPreparation.value)\nfunction cancelAttachmentUpload() { preparingView?.attachmentPreparation.value?.abort() }\nonBeforeUnmount(cancelAttachmentUpload)', 1), ('onDeactivated(() => {\n  onDeactivatedResetScroll()', 'onDeactivated(() => {\n  cancelAttachmentUpload()\n  onDeactivatedResetScroll()', 1), ('const attachmentUploading = computed(() => !!preparingView?.attachmentPreparation.value)', 'const attachmentUploading = computed(() => !!paneView.value.attachmentPreparation.value)', 1)],
        'apps/web/src/store/chat/types.ts': [
            ('  onBeforeMessageSend?: () => void', '''  onBeforeMessageSend?: () => void
  /** Harmony originals become ready before the existing single WS submission. */
  prepareAttachments?: (signal: AbortSignal) => Promise<ChatAttachment[]>''', 1),
        ],
        'apps/web/src/store/chat/view-registry.ts': [
            ('  transcript: ChatTranscriptController',
             '  attachmentPreparation: Ref<AbortController | null>\n  transcript: ChatTranscriptController', 1),
            ('      transcript,\n      attachedPanelIds:',
             '      transcript,\n      attachmentPreparation: ref(null),\n      attachedPanelIds:', 1),
            ('    if (view.visiblePanelIds.size > 0) return true',
             '    if (view.attachmentPreparation.value || view.visiblePanelIds.size > 0) return true', 1),
            ('  function evict(view: ChatViewEntry) {',
             '  function evict(view: ChatViewEntry) {\n    view.attachmentPreparation.value?.abort()\n    view.attachmentPreparation.value = null', 1),
        ],
        'apps/web/src/store/chat/views.ts': [
            ('''  function isChatViewStreaming(target: ChatViewTarget, composerScope?: string) {
    const resolved = normalizeTarget(target)''', '''  function isChatViewStreaming(target: ChatViewTarget, composerScope?: string) {
    const resolved = normalizeTarget(target)
    if (chatView(resolved).attachmentPreparation.value) return true''', 1),
        ],
        'apps/web/src/store/chat/send.ts': [("import type { Ref } from 'vue'", "import { attachmentForWire, prepareAttachmentsForMessage } from '@/lib/harmony-files'\nimport type { Ref } from 'vue'", 1), ('    const inband = wasDraft && !isExternalAgent', '    const preparationController = options.prepareAttachments || attachments?.length ? new AbortController() : null\n    if (preparationController) initialView.attachmentPreparation.value = preparationController\n    const inband = wasDraft && !isExternalAgent', 1), ('    try {\n      options.onBeforeMessageSend?.()', "    try {\n      if (preparationController) attachments = await prepareAttachmentsForMessage(\n        viewTarget.botId, attachments, options.prepareAttachments, preparationController.signal)\n      options.onBeforeMessageSend?.()", 1), ('      const appendTurns = () => {', '      // The exact batch is ready; draft clearing must not cancel its own send.\n      if (initialView.attachmentPreparation.value === preparationController) initialView.attachmentPreparation.value = null\n      const appendTurns = () => {', 1), ('        attachments,\n        requested_skills:', '        attachments: attachments?.map(attachmentForWire),\n        requested_skills:', 1), ('    } finally {\n      stopConfirmationWatch()', '    } finally {\n      if (preparationController && initialView.attachmentPreparation.value === preparationController) {\n        preparationController.abort()\n        initialView.attachmentPreparation.value = null\n      }\n      stopConfirmationWatch()', 1)],
        'apps/web/src/store/chat/transcript.ts': [('  ): ChatUserTurn {\n    return {', '  ): ChatUserTurn {\n    return reactive({', 1), ('      invocationId: invocationId.trim() || undefined,\n    }\n  }\n\n  function bindRuntimeTurn', '      invocationId: invocationId.trim() || undefined,\n    })\n  }\n\n  function bindRuntimeTurn', 1)],
        'apps/web/src/pages/home/components/attachment-block.vue': [
            ('      contentHash: contentHash || undefined,', '''      contentHash: contentHash || undefined,
      storageKey: String(att.storage_key ?? att.metadata?.storage_key ?? '') || undefined,
      size: att.size,
      mime: att.mime,''', 1),
            ('onOpenMedia?: (src: string) => void', 'onOpenMedia?: (src: string, origin?: HTMLElement) => void', 1),
            ('@click="handleMediaClick(att)"', '@click="handleMediaClick(att, $event)"', 2),
            ('function handleMediaClick(att: AttachmentItem)', 'function handleMediaClick(att: AttachmentItem, event: Event)', 1),
            ('props.onOpenMedia(src)', 'props.onOpenMedia(src, event.currentTarget instanceof HTMLElement ? event.currentTarget : undefined)', 1),
            ('<ChatAttachmentCard\n', '<ChatAttachmentCard\n          v-bind="harmonyTransfer(att)"\n', 4),
            ("import ChatAttachmentCard from './chat-attachment-card.vue'", """function harmonyTransfer(att: AttachmentItem) {
  return { transfer: typeof att.metadata?.harmony_transfer === 'string' ? att.metadata.harmony_transfer : undefined,
    wireLoaded: typeof att.metadata?.harmony_upload_wire_loaded === 'number' ? att.metadata.harmony_upload_wire_loaded : undefined,
    wireTotal: typeof att.metadata?.harmony_upload_wire_total === 'number' ? att.metadata.harmony_upload_wire_total : undefined }
}
import ChatAttachmentCard from './chat-attachment-card.vue'""", 1),
            ("import ChatAttachmentCard from './chat-attachment-card.vue'", "import ChatAttachmentCard from './chat-attachment-card.vue'\nimport HarmonyVoicePlayer from '@/lib/HarmonyVoicePlayer.vue'\nimport { saveOriginal } from '@/lib/harmony-files'\nfunction harmonyAudioFile(att: AttachmentItem): Blob | undefined {\n  const file = att.metadata?.harmony_voice_file\n  return file instanceof Blob ? file : undefined\n}", 1),
            ('      <!-- Audio player -->', '''      <HarmonyVoicePlayer
        v-else-if="isAudio(att)"
        :src="getUrl(att) || undefined"
        :file="harmonyAudioFile(att)"
        :peaks="Array.isArray(att.metadata?.voice_peaks) ? att.metadata.voice_peaks as number[] : undefined"
        :duration-ms="Number(att.metadata?.voice_duration_ms) || undefined"
        :handle="typeof att.metadata?.voice_handle === 'string' ? att.metadata.voice_handle : undefined"
        :transfer="typeof att.metadata?.harmony_transfer === 'string' ? att.metadata.harmony_transfer : undefined"
        :wire-loaded="harmonyTransfer(att).wireLoaded"
        :wire-total="harmonyTransfer(att).wireTotal"
        :save-original="att.content_hash ? () => saveOriginal(att) : undefined"
      />
      <!-- Audio player -->''', 1),
            # The shared waveform component replaces only audio presentation.
            ('''      <div
        v-else-if="isAudio(att) && getUrl(att)"
        class="rounded-lg border bg-muted/30 px-3 py-2 min-w-[280px] max-w-[400px]"
      >
        <audio
          controls
          preload="metadata"
          class="w-full"
          :src="getUrl(att)"
        />
      </div>

''', '', 1),
                    ("import { saveOriginal } from '@/lib/harmony-files'", "import { originalBlob, saveOriginal } from '@/lib/harmony-files'", 1),
            ('function getUrl(att: AttachmentItem): string {\n  return resolveUrl(att)\n}', "// This component only exposes complete, verified NAS image bytes.\nconst imageUrls = reactive<Record<string, string>>({})\n\nfunction imageReferenceKey(att: AttachmentItem): string {\n  return JSON.stringify([att.content_hash, att.bot_id ?? att.metadata?.bot_id,\n    att.storage_key ?? att.metadata?.storage_key, att.size, att.mime, att.metadata?.delegation_url])\n}\n\nwatch(() => JSON.stringify([...new Set(props.block.attachments.filter(isImage).map(imageReferenceKey))].sort()),\n  (_keys, _previous, onCleanup) => {\n    const controller = new AbortController()\n    const urls: string[] = []\n    const seen = new Set<string>()\n    onCleanup(() => {\n      controller.abort()\n      for (const url of urls) URL.revokeObjectURL(url)\n      for (const key of Object.keys(imageUrls)) delete imageUrls[key]\n    })\n    for (const att of props.block.attachments.filter(isImage)) {\n      const key = imageReferenceKey(att)\n      if (seen.has(key)) continue\n      seen.add(key)\n      void originalBlob(att, controller.signal).then((blob) => {\n        if (controller.signal.aborted) return\n        const url = URL.createObjectURL(blob)\n        urls.push(url)\n        imageUrls[key] = url\n      }).catch(() => { /* Existing non-accessible file card stays visible. */ })\n    }\n  }, { immediate: true })\n\nfunction getUrl(att: AttachmentItem): string {\n  return isImage(att) ? imageUrls[imageReferenceKey(att)] || '' : resolveUrl(att)\n}", 1),
            ('function handleMediaClick(att: AttachmentItem, event: Event) {\n  const src = getUrl(att)', 'function handleMediaClick(att: AttachmentItem, event: Event) {\n  const src = isImage(att) ? resolveUrl({ ...att }) : getUrl(att)', 1),
        ],
        'apps/web/src/pages/home/components/chat-attachment-card.vue': [
            ("emit('preview')", "emit('preview', $event)", 3),
            ('remove: []; preview: []', 'remove: []; preview: [event: Event]', 1),
            ('    <!-- Remove (composer only).', '''    <span v-if="transfer" class="absolute bottom-1 right-1 inline-flex min-w-4 items-center justify-center rounded-sm bg-surface-composer px-1 text-caption tabular-nums text-muted-foreground"
      :title="transferLabel" :aria-label="transferLabel">
      <Check v-if="transfer === 'ready'" class="size-4" />
      <span v-else-if="transfer === 'uploading' && wireTotal">{{ Math.min(100, Math.floor((wireLoaded || 0) * 100 / wireTotal)) }}%</span>
      <span v-else class="size-1.5 rounded-full bg-current" />
    </span>
    <!-- Remove (composer only).''', 1),
            ('import { X, Image as ImageIcon,', 'import { Check, X, Image as ImageIcon,', 1),
            ('  removeLabel?: string', '  transfer?: string\n  wireLoaded?: number\n  wireTotal?: number\n  removeLabel?: string', 1),
            ("const emit = defineEmits", '''const transferLabel = computed(() => props.transfer === 'ready' ? 'NAS 原件已完整校验'
  : props.transfer === 'uploading' && props.wireLoaded != null
    ? `上传请求 ${props.wireLoaded}${props.wireTotal ? '/' + props.wireTotal : ''} B（含 multipart 封装）`
    : props.transfer === 'verifying' ? '正在完整校验 NAS 原件' : '正在准备附件')
const emit = defineEmits''', 1),
        ],
        'apps/web/src/components/sidebar/files-pane.vue': [
            ("import { computed,", "import { saveWorkspaceFile } from '@/lib/harmony-files'\nimport { computed,", 1),
            ('''function handleDownload(entry: HandlersFsFileInfo) {
  const url = sdkApiUrl({''', '''async function handleDownload(entry: HandlersFsFileInfo) {
  if (!entry.isDir && entry.path) {
    try { await saveWorkspaceFile(props.botId, entry.path, entry.name ?? 'file', entry.size) }
    catch (error) { toast.error(resolveApiErrorMessage(error, t('bots.files.downloadFailed'))) }
    return
  }
  const url = sdkApiUrl({''', 1),

        ],
        'apps/web/src/pages/home/composables/useFileManagerProvider.ts': [
            ('  contentHash?: string', '  contentHash?: string\n  storageKey?: string\n  size?: number\n  mime?: string', 1),
        ],
        'apps/web/src/pages/home/components/dockview/panel-asset.vue': [
            ('import { sdkApiUrl, sdkAuthQuery }', "import { saveOriginal } from '@/lib/harmony-files'\nimport { sdkApiUrl, sdkAuthQuery }", 1),
            ('contentHash?: string, delegationUrl?: string, src?: string', 'contentHash?: string, delegationUrl?: string, storageKey?: string, size?: number, mime?: string, src?: string', 1),
            ('''function handleDownload() {
  const url = renderUrl.value
  if (!url) return
  const a = document.createElement('a')
  a.href = url
  a.download = name.value || 'file'
  a.click()
}''', '''async function handleDownload() {
  const original = props.params.params
  try {
    await saveOriginal({ type: 'file', name: name.value, bot_id: original.botId,
      content_hash: original.contentHash, storage_key: original.storageKey,
      size: original.size, mime: original.mime, metadata: { delegation_url: original.delegationUrl } })
  } catch (error) { toast.error(resolveApiErrorMessage(error, t('bots.files.downloadFailed'))) }
}''', 1),
        ],
        'apps/web/src/store/workspace-tabs.ts': [
            ('        contentHash: args.contentHash,', '        contentHash: args.contentHash,\n        storageKey: args.storageKey,\n        size: args.size,\n        mime: args.mime,', 1),
        ],
        'apps/web/src/pages/main-section/components/mobile-top-bar.vue': [
            ('        <DropdownMenuContent align="end">', '''        <DropdownMenuContent align="end">
          <DropdownMenuItem v-if="downloadActiveFile" :disabled="downloading" @select="handleDownload">
            <Download class="mr-2 size-3.5" />
            {{ t('bots.files.download') }}
          </DropdownMenuItem>''', 1),
            ('Monitor, Plus, Terminal, X }', 'Monitor, Plus, Terminal, X, Download }', 1),
            ('  Button,\n  DropdownMenu,', '  Button,\n  toast,\n  DropdownMenu,', 1),
            ("import { hasBotPermission } from '@/utils/bot-permissions'", "import { hasBotPermission } from '@/utils/bot-permissions'\nimport { saveOriginal, saveWorkspaceFile } from '@/lib/harmony-files'\nimport { resolveApiErrorMessage } from '@/utils/api-error'", 1),
            ('function closeActivePanel() {', '''// Reuse this panel's original identity and the existing system download chain.
const downloadActiveFile = computed(() => {
  const id = activeId.value
  const panel = id ? api.value?.getPanel(id) : undefined
  const value = panel?.params as { name?: string; botId?: string; contentHash?: string;
    storageKey?: string; size?: number; mime?: string; filePath?: string } | undefined
  if (id?.startsWith('asset:') && value?.botId && value.contentHash) {
    const attachment = { type: 'file', name: value.name, bot_id: value.botId,
      content_hash: value.contentHash, storage_key: value.storageKey, size: value.size, mime: value.mime }
    return () => saveOriginal(attachment)
  }
  const botId = currentBotId.value
  if (id?.startsWith('file:') && botId && value?.filePath) {
    const path = value.filePath
    return () => saveWorkspaceFile(botId, path, path.split('/').at(-1) || 'file')
  }
  return undefined
})
const downloading = ref(false)
async function handleDownload() {
  const download = downloadActiveFile.value
  if (!download || downloading.value) return
  downloading.value = true
  try { await download() }
  catch (error) { toast.error(resolveApiErrorMessage(error, t('bots.files.downloadFailed'))) }
  finally { downloading.value = false }
}

function closeActivePanel() {''', 1),
        ],
        'apps/web/src/pages/home/composables/useMediaGallery.ts': [
            ('  const openIndex = ref<number | null>(null)', '  const openIndex = ref<number | null>(null)\n  const origin = ref<HTMLElement | null>(null)', 1),
            ('function openBySrc(src: string)', 'function openBySrc(src: string, trigger?: HTMLElement)', 1),
            ('      openIndex.value = idx', '      origin.value = trigger ?? null\n      openIndex.value = idx', 1),
            ('    openIndex,', '    openIndex,\n    origin,', 1),
            ("import { computed,", "import { originalBlob } from '@/lib/harmony-files'\nimport { computed,", 1),
            ("            src,\n            type:", "            src,\n            original: type === 'video' ? undefined : () => originalBlob(att),\n            type:", 2),


        ],
        'apps/web/src/pages/home/components/media-gallery-lightbox.vue': [
            ('<Transition name="lightbox">', '<Transition name="lightbox" @enter="enterFromThumbnail" @before-leave="leaveToThumbnail">', 1),
            ('  openIndex: number | null', '  openIndex: number | null\n  origin?: HTMLElement | null', 1),
            ('            draggable="false"', '            draggable="false"\n            @load="fitImage($event.currentTarget)"', 1),
            ('''v-if="currentItem?.type === 'image'"
            :src="currentItem.src"''', '''v-if="currentItem?.type === 'image'"
            :src="currentItem.original ? harmonyImageSrc || undefined : currentItem.src"''', 1),
            ("import { computed, watchEffect,", "import { computed, watch, watchEffect,", 1),
            ('  src: string\n', '  src: string\n  original?: () => Promise<Blob>\n', 1),
            ('function close() {', '''const harmonyImageSrc = ref('')
let motionOrigin: HTMLElement | null = null
let motionSource = ''

function fitImage(target: EventTarget | null, thumbnail?: HTMLImageElement | null) {
  if (!(target instanceof HTMLImageElement)) return
  const source = target.naturalWidth && target.naturalHeight ? target : thumbnail
  if (source?.naturalWidth && source.naturalHeight) {
    target.style.setProperty('--lightbox-ratio', String(source.naturalWidth / source.naturalHeight))
  }
}

function thumbnailTransform(element: Element) {
  const dialog = element as HTMLElement
  dialog.style.setProperty('--lightbox-from-transform', 'none')
  dialog.style.setProperty('--lightbox-from-clip', 'inset(0px)')
  const image = dialog.querySelector<HTMLImageElement>('.lightbox-media img')
  const thumbnail = motionOrigin instanceof HTMLImageElement ? motionOrigin : motionOrigin?.querySelector('img')
  if (!image) return
  fitImage(image, thumbnail)
  if (!thumbnail?.isConnected) return
  const start = thumbnail.getBoundingClientRect()
  const end = image.parentElement!.getBoundingClientRect()
  if (!start.width || !start.height || !end.width || !end.height) return
  const scale = Math.max(start.width / end.width, start.height / end.height)
  const x = start.x + start.width / 2 - end.x - end.width / 2
  const y = start.y + start.height / 2 - end.y - end.height / 2
  const vertical = Math.max(0, (end.height - start.height / scale) / 2)
  const horizontal = Math.max(0, (end.width - start.width / scale) / 2)
  const radius = Number.parseFloat(getComputedStyle(thumbnail.parentElement ?? thumbnail).borderTopLeftRadius) || 0
  dialog.style.setProperty('--lightbox-from-transform', `translate(${x}px, ${y}px) scale(${scale})`)
  dialog.style.setProperty('--lightbox-from-clip', `inset(${vertical}px ${horizontal}px round ${radius / scale}px)`)
}

function enterFromThumbnail(element: Element) {
  motionOrigin = props.origin ?? null
  motionSource = currentItem.value?.src ?? ''
  thumbnailTransform(element)
  const image = element.querySelector<HTMLImageElement>('.lightbox-media img')
  if (!image) return
  // Establish the measured first state before Vue removes its enter-from class.
  image.style.transition = 'none'
  image.style.transform = (element as HTMLElement).style.getPropertyValue('--lightbox-from-transform')
  image.style.clipPath = (element as HTMLElement).style.getPropertyValue('--lightbox-from-clip')
  void image.getBoundingClientRect()
  image.style.removeProperty('transition')
  image.style.removeProperty('transform')
  image.style.removeProperty('clip-path')
}

function leaveToThumbnail(element: Element) {
  thumbnailTransform(element)
}

// Polling can replace the item object without changing its scoped media URL.
// Keep the verified Blob while that identity and its original-reader mode match.
watch([() => currentItem.value?.src, () => !!currentItem.value?.original], (_identity, _previous, onCleanup) => {
  const item = currentItem.value
  if (item && item.src !== motionSource) motionOrigin = null
  let active = true
  let url = ''
  harmonyImageSrc.value = ''
  if (item?.type === 'image' && item.original) {
    void item.original().then((blob) => {
      if (!active) return
      url = URL.createObjectURL(blob)
      harmonyImageSrc.value = url
    }).catch(() => { if (active) close() })
  }
  onCleanup(() => { active = false; if (url) URL.revokeObjectURL(url) })
}, { immediate: true })

function close() {''', 1),
            ('''/* The media scales in from slightly small for a quick, light pop — the scrim
   only fades, so the motion reads on the image rather than the whole screen. */
.lightbox-enter-active .lightbox-media,
.lightbox-leave-active .lightbox-media {
  transition: transform 0.18s cubic-bezier(0.16, 1, 0.3, 1);
}
.lightbox-enter-from .lightbox-media,
.lightbox-leave-to .lightbox-media {
  transform: scale(0.94);
}''', '''.lightbox-media img {
  width: min(90vw, calc(90vh * var(--lightbox-ratio, 1)));
  clip-path: inset(0px);
}
.lightbox-enter-active .lightbox-media img,
.lightbox-leave-active .lightbox-media img {
  transition: transform 0.26s cubic-bezier(0.22, 1, 0.36, 1),
    clip-path 0.26s cubic-bezier(0.22, 1, 0.36, 1);
}
.lightbox-leave-active .lightbox-media img {
  transition-duration: 0.18s;
}
.lightbox-enter-from .lightbox-media img,
.lightbox-leave-to .lightbox-media img {
  transform: var(--lightbox-from-transform, none);
  clip-path: var(--lightbox-from-clip, inset(0px));
}''', 1),
            ('  transition: opacity 0.18s ease;', '  transition: opacity 0.26s cubic-bezier(0.22, 1, 0.36, 1);', 1),
            ('.lightbox-enter-from,\n.lightbox-leave-to {', '.lightbox-leave-active { transition-duration: 0.18s; }\n.lightbox-enter-from,\n.lightbox-leave-to {', 1),
            ('''  .lightbox-enter-active .lightbox-media,
  .lightbox-leave-active .lightbox-media''', '''  .lightbox-enter-active .lightbox-media img,
  .lightbox-leave-active .lightbox-media img''', 1),
            ('''  .lightbox-enter-from .lightbox-media,
  .lightbox-leave-to .lightbox-media''', '''  .lightbox-enter-from .lightbox-media img,
  .lightbox-leave-to .lightbox-media img''', 1),
            ('    transform: none;\n  }', '    transform: none;\n    clip-path: none;\n  }\n  .lightbox-enter-active, .lightbox-leave-active { transition-duration: 0.12s; }', 1),
            ('<Transition name="lightbox" @enter="enterFromThumbnail" @before-leave="leaveToThumbnail">', '<Transition name="lightbox" @enter="enterFromThumbnail" @after-enter="imageEntered" @before-leave="leaveToThumbnail" @after-leave="resetImageInteraction">', 1),
            ('            @load="fitImage($event.currentTarget)"\n            @click="close"', '            @load="fitImage($event.currentTarget)"\n            @pointerdown="imagePointerDown"\n            @pointermove="imagePointerMove"\n            @pointerup="imagePointerUp"\n            @pointercancel="imagePointerCancelled"\n            @lostpointercapture="imagePointerCancelled"\n            @contextmenu="imageContextMenu"\n            @click="imageClick"', 1),
            ("import { computed, watch, watchEffect, nextTick, ref } from 'vue'", "import { computed, watch, watchEffect, nextTick, ref, onBeforeUnmount } from 'vue'", 1),
            ('function close() {', "type ImagePoint = { x: number; y: number }\ntype ImagePose = ImagePoint & { scale: number }\ntype ImageBounds = { width: number; height: number; center: ImagePoint; viewportWidth: number; viewportHeight: number }\n\nconst imagePointers = new Map<number, ImagePoint>()\nlet interactionImage: HTMLImageElement | null = null\nlet imagePose: ImagePose = { scale: 1, x: 0, y: 0 }\nlet imageBounds: ImageBounds | null = null\nlet imageAnchor: { points: ImagePoint[]; pose: ImagePose } | null = null\nlet imageClickConsumed = false\nlet imageMotionActive = false\n\nfunction boundImagePose(pose: ImagePose, bounds: ImageBounds): ImagePose {\n  const scale = Math.max(1, pose.scale)\n  const xLimit = Math.max(0, (bounds.width * scale - bounds.viewportWidth) / 2)\n  const yLimit = Math.max(0, (bounds.height * scale - bounds.viewportHeight) / 2)\n  return { scale, x: Math.max(-xLimit, Math.min(xLimit, pose.x)), y: Math.max(-yLimit, Math.min(yLimit, pose.y)) }\n}\n\nfunction projectImagePose(points: ImagePoint[], anchor: { points: ImagePoint[]; pose: ImagePose }, bounds: ImageBounds): ImagePose {\n  const first = points[0]\n  const start = anchor.points[0]\n  if (!first || !start) return anchor.pose\n  if (points[1] && anchor.points[1]) {\n    const second = points[1]\n    const startSecond = anchor.points[1]\n    const distance = Math.hypot(second.x - first.x, second.y - first.y)\n    const startDistance = Math.hypot(startSecond.x - start.x, startSecond.y - start.y)\n    if (!startDistance) return anchor.pose\n    const scale = Math.max(1, anchor.pose.scale * distance / startDistance)\n    const startX = (start.x + startSecond.x) / 2 - bounds.center.x\n    const startY = (start.y + startSecond.y) / 2 - bounds.center.y\n    return boundImagePose({ scale,\n      x: (first.x + second.x) / 2 - bounds.center.x - (startX - anchor.pose.x) * scale / anchor.pose.scale,\n      y: (first.y + second.y) / 2 - bounds.center.y - (startY - anchor.pose.y) * scale / anchor.pose.scale,\n    }, bounds)\n  }\n  return boundImagePose({ scale: anchor.pose.scale,\n    x: anchor.pose.x + first.x - start.x, y: anchor.pose.y + first.y - start.y,\n  }, bounds)\n}\n\nfunction clearImagePointers() {\n  const ids = [...imagePointers.keys()]\n  imagePointers.clear()\n  imageAnchor = null\n  for (const id of ids) {\n    if (interactionImage?.hasPointerCapture(id)) interactionImage.releasePointerCapture(id)\n  }\n}\n\nfunction resetImageInteraction() {\n  clearImagePointers()\n  interactionImage?.style.removeProperty('transform')\n  interactionImage?.style.removeProperty('transition')\n  interactionImage = null\n  imageBounds = null\n  imagePose = { scale: 1, x: 0, y: 0 }\n  imageClickConsumed = false\n  imageMotionActive = false\n}\n\nfunction rebaseImageGesture() {\n  const image = interactionImage\n  const dialog = image?.closest<HTMLElement>('[role=\"dialog\"]')\n  if (!image || !dialog || imageMotionActive || !imagePointers.size) return\n  const rect = image.getBoundingClientRect()\n  const viewport = dialog.getBoundingClientRect()\n  if (!rect.width || !rect.height || !viewport.width || !viewport.height) return\n  imageBounds = { width: rect.width / imagePose.scale, height: rect.height / imagePose.scale,\n    center: { x: rect.left + rect.width / 2 - imagePose.x, y: rect.top + rect.height / 2 - imagePose.y },\n    viewportWidth: viewport.width, viewportHeight: viewport.height }\n  imageAnchor = { points: [...imagePointers.values()].slice(0, 2), pose: { ...imagePose } }\n  // Only the actual image transform changes during direct manipulation.\n  image.style.transition = 'none'\n}\n\nfunction imagePointerDown(event: PointerEvent) {\n  if (event.pointerType !== 'touch' || !(event.currentTarget instanceof HTMLImageElement)) return\n  if (!imagePointers.size) imageClickConsumed = false\n  interactionImage = event.currentTarget\n  imagePointers.set(event.pointerId, { x: event.clientX, y: event.clientY })\n  interactionImage.setPointerCapture(event.pointerId)\n  if (imagePointers.size > 1) imageClickConsumed = true\n  rebaseImageGesture()\n}\n\nfunction imagePointerMove(event: PointerEvent) {\n  if (!imagePointers.has(event.pointerId)) return\n  imagePointers.set(event.pointerId, { x: event.clientX, y: event.clientY })\n  if (imageMotionActive) return\n  if (!imageAnchor || !imageBounds) { rebaseImageGesture(); return }\n  // Coincident fingers have no scale baseline; establish one without a jump.\n  const first = imageAnchor.points[0]\n  const second = imageAnchor.points[1]\n  if (first && second && first.x === second.x && first.y === second.y) {\n    rebaseImageGesture()\n    return\n  }\n  const next = projectImagePose([...imagePointers.values()].slice(0, 2), imageAnchor, imageBounds)\n  if (next.scale === imagePose.scale && next.x === imagePose.x && next.y === imagePose.y) return\n  imageClickConsumed = true\n  imagePose = next\n  interactionImage!.style.transform = `translate(${imagePose.x}px, ${imagePose.y}px) scale(${imagePose.scale})`\n}\n\nfunction imagePointerUp(event: PointerEvent) {\n  if (!imagePointers.delete(event.pointerId)) return\n  if (interactionImage?.hasPointerCapture(event.pointerId)) interactionImage.releasePointerCapture(event.pointerId)\n  imageAnchor = null\n  rebaseImageGesture()\n}\n\nfunction imagePointerCancelled(event: PointerEvent) {\n  if (!imagePointers.has(event.pointerId)) return\n  imageClickConsumed = true\n  imagePointerUp(event)\n}\n\nfunction imageContextMenu() {\n  // Observe only. ArkWeb's existing authenticated system sheet remains in charge.\n  imageClickConsumed = true\n  clearImagePointers()\n}\n\nfunction imageClick(event: MouseEvent) {\n  if (imageClickConsumed) { event.preventDefault(); event.stopPropagation(); return }\n  close()\n}\n\nfunction imageEntered() {\n  imageMotionActive = false\n  // Touches begun during the thumbnail transition start from its settled pose.\n  rebaseImageGesture()\n}\n\nfunction beginImageLeave(element: Element) {\n  imageMotionActive = true\n  const dialog = element as HTMLElement\n  const image = element.querySelector<HTMLImageElement>('.lightbox-media img')\n  if (image) {\n    // Vue's leave-from class keeps this exact pose until the return transition.\n    dialog.style.setProperty('--lightbox-close-from-transform', getComputedStyle(image).transform)\n    image.style.removeProperty('transform')\n    image.style.removeProperty('transition')\n  }\n  clearImagePointers()\n}\n\nwatch(() => [props.openIndex, currentItem.value?.src], () => {\n  if (!isOpen.value) return // Keep the closing pose until Vue's after-leave.\n  const hadPointers = imagePointers.size > 0\n  resetImageInteraction()\n  imageClickConsumed = hadPointers\n})\nonBeforeUnmount(resetImageInteraction)\n\nfunction close() {", 1),
            ('function enterFromThumbnail(element: Element) {\n', 'function enterFromThumbnail(element: Element) {\n  resetImageInteraction()\n  imageMotionActive = true\n', 1),
            ('function leaveToThumbnail(element: Element) {\n  thumbnailTransform(element)', 'function leaveToThumbnail(element: Element) {\n  beginImageLeave(element)\n  thumbnailTransform(element)', 1),
            ('.lightbox-media img {\n  width: min(90vw,', '.lightbox-media img {\n  touch-action: none;\n  width: min(90vw,', 1),
            ('.lightbox-enter-from .lightbox-media img,\n.lightbox-leave-to .lightbox-media img {', '.lightbox-leave-from .lightbox-media img {\n  transform: var(--lightbox-close-from-transform, none);\n}\n.lightbox-enter-from .lightbox-media img,\n.lightbox-leave-to .lightbox-media img {', 1),
            ('v-if="isOpen"', 'v-if="isOpen && (!currentItem?.original || !!harmonyImageSrc)"', 1),
            ('void item.original().then((blob) => {\n      if (!active) return\n      url = URL.createObjectURL(blob)\n      harmonyImageSrc.value = url', 'void item.original().then(async (blob) => {\n      if (!active) return\n      url = URL.createObjectURL(blob)\n      const decoded = new Image()\n      decoded.src = url\n      await decoded.decode()\n      if (active) harmonyImageSrc.value = url', 1),
            ('watchEffect(() => {\n  if (isOpen.value) {', 'watchEffect(() => {\n  if (isOpen.value && (!currentItem.value?.original || harmonyImageSrc.value)) {', 1),
            ('.lightbox-media img {\n  touch-action: none;', '.lightbox-media img {\n  touch-action: none;\n  aspect-ratio: var(--lightbox-ratio, auto);', 1),
        ],
        'apps/web/src/pages/home/components/message-item.vue': [
            ('onOpenMedia?: (src: string) => void', 'onOpenMedia?: (src: string, origin?: HTMLElement) => void', 1),


        ],
        'apps/web/src/pages/home/composables/useComposerAttachments.ts': [
            ('  const composerPreviewIndex = ref<number | null>(null)', '  const composerPreviewIndex = ref<number | null>(null)\n  const composerPreviewOrigin = ref<HTMLElement | null>(null)', 1),
            ('function openComposerPreview(url: string) {', '''async function openComposerPreview(url: string, event?: Event, file?: File) {
    if (file && isTextLikeFile(file)) {
      const text = await file.text()
      if (pendingFiles.value.includes(file)) pastedViewerText.value = text
      return
    }''', 1),
            ('        isPasted,', '        isPasted,\n        isText: isTextLikeFile(file),', 1),
            ('    if (idx >= 0) composerPreviewIndex.value = idx', '''    if (idx >= 0) {
      composerPreviewOrigin.value = event?.currentTarget instanceof HTMLElement ? event.currentTarget : null
      composerPreviewIndex.value = idx
    }''', 1),
            ('    composerPreviewIndex,', '    composerPreviewIndex,\n    composerPreviewOrigin,', 1),
        ],
        'apps/web/src/i18n/locales/zh.json': [
            ('"loginFailed": "登录失败",', '''"loginFailed": "登录失败",
    "loginRejected": "登录被拒绝",
    "loginRejectedHint": "请检查用户名、密码或账号状态。",
    "connectionFailed": "无法连接登录服务器，请检查网络后重试。",
    "invalidResponse": "登录服务器返回异常，请重试。",''', 1),
            ('"attachFiles": "上传文件或图片",', '"attachPhotos": "选择照片",\n    "attachFiles": "上传文件或图片",', 1),
        ],
        'apps/web/src/i18n/locales/en.json': [
            ('"loginFailed": "Login Failed",', '''"loginFailed": "Login Failed",
    "loginRejected": "Sign-in rejected",
    "loginRejectedHint": "Check your username, password, or account status.",
    "connectionFailed": "Cannot connect to the sign-in server. Check your network and try again.",
    "invalidResponse": "The sign-in server returned an invalid response. Please try again.",''', 1),
            ('"attachFiles": "Upload Files or Images",', '"attachPhotos": "Select Photos",\n    "attachFiles": "Upload Files or Images",', 1),
        ],
        'apps/web/src/i18n/locales/ja.json': [
            ('"loginFailed": "ログインに失敗しました",', '''"loginFailed": "ログインに失敗しました",
    "loginRejected": "ログインが拒否されました",
    "loginRejectedHint": "ユーザー名、パスワード、アカウントの状態を確認してください。",
    "connectionFailed": "ログインサーバーに接続できません。ネットワークを確認して再試行してください。",
    "invalidResponse": "ログインサーバーから無効な応答が返されました。再試行してください。",''', 1),
            ('"attachFiles": "ファイルまたは画像をアップロード",', '"attachPhotos": "写真を選択",\n    "attachFiles": "ファイルまたは画像をアップロード",', 1),
        ],
        'apps/web/src/components/file-manager/file-viewer.vue': [

            ("import { sdkApiUrl, sdkAuthQuery } from '@/lib/api-client'", "import { saveWorkspaceFile } from '@/lib/harmony-files'", 1),
            ('''function handleDownload() {
  const url = sdkApiUrl({
    url: '/bots/{bot_id}/container/fs/download',
    path: { bot_id: props.botId },
    query: { path: filePath.value, ...sdkAuthQuery() },
  })
  const a = document.createElement('a')
  a.href = url
  a.download = filename.value
  a.click()
}''', '''async function handleDownload() {
  try { await saveWorkspaceFile(props.botId, filePath.value, filename.value, props.file.size) }
  catch (error) { toast.error(resolveApiErrorMessage(error, t('bots.files.downloadFailed'))) }
}''', 1),
        ],
        'apps/web/src/composables/useBotDisplayConnection.ts': [],
        'apps/web/src/pages/home/components/display-pane.vue': [],
















































        'packages/ui/src/components/label-swap/LabelSwap.vue': [
            ("import type { ComponentPublicInstance } from 'vue'\n", '', 1),
            ('el: Element | ComponentPublicInstance | null', 'el: unknown', 1),
        ],






    }
    # Temporary opt-in image timing adds no display, request, or authorization path.
    changes['apps/web/src/pages/home/components/attachment-block.vue'].extend([
        ("import { originalBlob, saveOriginal } from '@/lib/harmony-files'",
         "import { originalBlob, saveOriginal } from '@/lib/harmony-files'\nimport { imageTimingCard, imageTimingURL, imageTimingElement, imageTimingLoaded, imageTimingFailed } from '@/lib/harmony-image-timing'", 1),
        ('      seen.add(key)\n      void originalBlob',
         '      seen.add(key)\n      imageTimingCard(att)\n      void originalBlob', 1),
        ('        const url = URL.createObjectURL(blob)\n        urls.push(url)',
         '        const url = URL.createObjectURL(blob)\n        imageTimingURL(blob, url)\n        urls.push(url)', 1),
        ('          loading="eager"\n          decoding="async"',
         '          loading="eager"\n          decoding="async"\n          :elementtiming="imageTimingElement(getUrl(att))"\n          @load="imageTimingLoaded"\n          @error="imageTimingFailed"', 1),
        ('          :src="getUrl(att)"\n          :video="isVideo(att)"',
         '          :src="getUrl(att)"\n          :video="isVideo(att)"\n          :element-timing="imageTimingElement(getUrl(att))"\n          @load.capture="imageTimingLoaded"\n          @error.capture="imageTimingFailed"', 1),
    ])
    changes['apps/web/src/pages/home/components/chat-attachment-card.vue'].extend([
        ('  src?: string', '  src?: string\n  elementTiming?: string', 1),
        ('          v-else-if="src"\n          :src="src"', '          v-else-if="src"\n          :src="src"\n          :elementtiming="elementTiming"', 1),
    ])
    changes['apps/web/src/pages/home/composables/useComposerAttachments.ts'].extend([
        ("import { computed,", "import { selectedAttachmentFile } from '@/lib/harmony-files'\nimport { computed,", 1),
        ('const uploaded = uploadedAttachmentFile(attachment)',
         'const uploaded = selectedAttachmentFile(attachment) ?? uploadedAttachmentFile(attachment)', 1),
    ])
    # The unified upstream already owns its SDK and Web type fixes.
    changes['apps/web/src/pages/home/components/chat-pane.vue'].extend([('let preparingView: ReturnType<typeof chatStore.chatView> | undefined', 'let preparingView: ReturnType<typeof chatStore.chatView> | undefined\nlet attachmentPreparationCancelled = false', 1), ('function cancelAttachmentUpload() { preparingView?.attachmentPreparation.value?.abort() }', 'function cancelAttachmentUpload() {\n  const controller = preparingView?.attachmentPreparation.value\n  if (controller) { attachmentPreparationCancelled = true; controller.abort() }\n}', 1), ('  preparingView = paneView.value', '  attachmentPreparationCancelled = false\n  preparingView = paneView.value', 1), ('  if (!holdsInput) clearSentInput()\n', '', 1), ('      if (holdsInput) {\n        clearSentInput(onSentPane)', '      clearSentInput(onSentPane)\n      if (holdsInput) {', 1), ('  if (restore) {\n    if (!matchesChatPaneSendContext(', '  if (restore) {\n    // Selection/lifecycle cancellation leaves the live composer untouched.\n    if (attachmentPreparationCancelled) return\n    if (!matchesChatPaneSendContext(', 1)])
    # One composer and one send: native handles join the existing preparation
    # callback, while clipboard/voice Files keep their established path.
    changes['apps/web/src/pages/home/components/chat-pane.vue'].extend([
        ("import { prepareSubmission } from '@/lib/harmony-files'",
         "import { prepareSubmission } from '@/lib/harmony-files'\nimport HarmonyAttachments from '@/lib/HarmonyAttachments.vue'\nimport { nativeAttachmentsAvailable, type NativeAttachment } from '@/lib/harmony-resumable'", 1),
        ('const harmonyPhotoInput = ref<HTMLInputElement>()', '''const harmonyPhotoInput = ref<HTMLInputElement>()
const harmonyAttachmentPicker = ref<InstanceType<typeof HarmonyAttachments>>()
const pendingNativeAttachments = ref<NativeAttachment[]>([])
const nativeAttachmentScope = computed(() => paneTarget.value.sessionId
  ? 'session:' + paneTarget.value.sessionId : 'draft:' + paneTarget.value.viewId)
const nativeAttachmentBusy = computed(() => !!harmonyAttachmentPicker.value?.busy)''', 1),
        ('            <input ref="harmonyPhotoInput"', '''            <HarmonyAttachments
              ref="harmonyAttachmentPicker"
              v-model="pendingNativeAttachments"
              :bot-id="currentBotId || ''"
              :scope="nativeAttachmentScope"
              :active="isActive && isVisible"
              :disabled="!isActive || !isVisible || !currentBotId || activeChatReadOnly || streaming || loadingMessages || attachmentPreparing"
            />
            <input ref="harmonyPhotoInput"''', 1),
        ('@select="harmonyPhotoInput?.click()"', '@select="nativeAttachmentsAvailable ? harmonyAttachmentPicker?.select(true) : harmonyPhotoInput?.click()"', 1),
        ('@select="fileInput?.click()"', '@select="nativeAttachmentsAvailable ? harmonyAttachmentPicker?.select(false) : fileInput?.click()"', 1),
        ('|| pendingFiles.value.length > 0 || requestedSkills.value.length', '|| pendingFiles.value.length > 0 || pendingNativeAttachments.value.length > 0 || requestedSkills.value.length', 1),
        ('watch(() => pendingFiles.value.slice(), cancelAttachmentUpload, { flush: \'sync\' })',
         "watch(() => pendingFiles.value.slice(), cancelAttachmentUpload, { flush: 'sync' })\nwatch(() => pendingNativeAttachments.value.slice(), cancelAttachmentUpload, { flush: 'sync' })", 1),
        ('  if (!isActive.value || attachmentUploading.value || firstSendAwaiting.value) return',
         '  if (!isActive.value || attachmentUploading.value || nativeAttachmentBusy.value || firstSendAwaiting.value) return', 1),
        ('  const files = [...pendingFiles.value]', '  const files = [...pendingFiles.value]\n  const nativeFiles = [...pendingNativeAttachments.value]\n  const attachmentScope = nativeAttachmentScope.value', 1),
        ('if (files.length || skills.length)', 'if (files.length || nativeFiles.length || skills.length)', 1),
        ('composerError.value = files.length\n', 'composerError.value = files.length || nativeFiles.length\n', 1),
        ('if (!text || files.length || skills.length ||', 'if (!text || files.length || nativeFiles.length || skills.length ||', 1),
        ('(!text && !files.length && !skills.length && !voice)', '(!text && !files.length && !nativeFiles.length && !skills.length && !voice)', 1),
        ("(goalSend || /^\\/goal(?:\\s|$)/.test(text)) && (files.length || skills.length)",
         "(goalSend || /^\\/goal(?:\\s|$)/.test(text)) && (files.length || nativeFiles.length || skills.length)", 1),
        ('    pendingFiles.value = []\n    requestedSkills.value = []', '    pendingFiles.value = []\n    pendingNativeAttachments.value = []\n    requestedSkills.value = []', 1),
        ("  pendingFiles.value = []\n  composerError.value = ''", "  pendingFiles.value = []\n  pendingNativeAttachments.value = []\n  composerError.value = ''", 1),
        ('prepareSubmission(sentContext.target.botId, files, voice)',
         'prepareSubmission(sentContext.target.botId, files, voice, { scope: attachmentScope, files: nativeFiles })', 1),
        ('    pendingFiles.value = nativeSubmission.files', '    pendingFiles.value = nativeSubmission.files\n    pendingNativeAttachments.value = nativeSubmission.nativeFiles', 1),
        ("  rollbackPin = null\n  pairSend.finish(result.messageSent === true || result.stage === 'stream')",
         "  rollbackPin = null\n  if (result.messageSent === true) await nativeSubmission.consumeNative()\n  pairSend.finish(result.messageSent === true || result.stage === 'stream')", 1),
    ])
    changes['apps/web/src/lib/api-client.ts'] = [
        ('function createApiFetch(', 'export function createApiFetch(', 1),
    ]
    # A task body can mount without the dock provider. Reuse the same native
    # save path as the asset preview instead of the Web-only anchor fallback.
    changes['apps/web/src/pages/home/components/attachment-block.vue'].extend([
        ("import { originalBlob, saveOriginal } from '@/lib/harmony-files'",
         "import { originalBlob, saveOriginal } from '@/lib/harmony-files'\nimport { toast } from '@felinic/ui'", 1),
        ("  const a = document.createElement('a')\n  a.href = url\n  a.download = name || 'file'\n  a.click()",
         "  void saveOriginal(att).catch(() => { toast.error('原件未保存，请重试') })", 1),
    ])
    # The complete shared source already includes observation recovery and task
    # delivery UI. Keep image originals on the existing verified phone path.
    changes['apps/web/src/pages/home/components/session-task-results.vue'] = [
        ("import { resolveUrl, isMediaType }", "import { originalBlob, saveOriginal } from '@/lib/harmony-files'\nimport { toast } from '@felinic/ui'\nimport { resolveUrl, isMediaType }", 1),
        ('// Use the same attachment resolver, preview host and browser download as chat.',
         '// Use the same attachment resolver, preview host and verified save path as chat.', 1),
        ('const mediaItems = computed<MediaGalleryItem[]>', '''async function saveTaskOriginal(id: string) {
  const attachment = attachments.value.get(id)
  if (!attachment) return
  try { await saveOriginal(attachment) }
  catch { toast.error(t('bots.files.downloadFailed')) }
}
const mediaItems = computed<MediaGalleryItem[]>''', 1),
        (".filter(isMediaType).map(att => ({ src: resolveUrl(att), type: att.mime?.startsWith('video/') ? 'video' : 'image', name: att.name })))",
         ".filter(isMediaType).map(att => ({ src: resolveUrl(att), type: att.mime?.startsWith('video/') ? 'video' : 'image', name: att.name, original: att.mime?.startsWith('video/') ? undefined : () => originalBlob(att) })))", 1),
        ('''                    as-child
                  >
                    <a
                      :href="resolveUrl(attachments.get(file.delivery_id!)!)"
                      :download="file.delivery_id"
                    >{{ t('bots.files.download') }}</a>''', '''                    @click="saveTaskOriginal(file.delivery_id!)"
                  >
                    {{ t('bots.files.download') }}''', 1),
    ]
    # Reuse only approved shared activity navigation; no Web media-transfer overlay.
    shared_changes = {
        'apps/web/src/components/sidebar/index.vue': [('    <div class="relative min-h-0 flex-1 overflow-hidden">\n      <PanelSessions\n        v-show="sidebarView === \'sessions\'"\n        class="h-full"\n      />\n      <PanelFiles\n', '    <div class="relative min-h-0 flex-1 overflow-hidden">\n      <PanelSessions\n        v-show="sidebarView === \'sessions\'"\n        :activity-visible="workbenchOpen && sidebarView === \'sessions\'"\n        class="h-full"\n      />\n      <PanelFiles\n', 1)],
        'apps/web/src/components/sidebar/panel-sessions.vue': [('      class="sidebar-scroll min-h-0 flex-1"\n      :scroll-hide-delay="300"\n    >\n      <FoldersSection />\n\n      <Recents :scroll-el="listScrollEl" />\n', '      class="sidebar-scroll min-h-0 flex-1"\n      :scroll-hide-delay="300"\n    >\n      <SessionActivity :visible="activityVisible" />\n      <FoldersSection />\n\n      <Recents :scroll-el="listScrollEl" />\n', 1), ("import FoldersSection from './folders-section.vue'\nimport Recents from './recents.vue'\nimport SessionSelectionBar from './session-selection-bar.vue'\nimport { createSessionSelection, SessionSelectionKey } from './session-selection'\nimport '@/styles/sidebar-scroll.css'\n\nconst { t } = useI18n()\n\n// The one scrollport for Folders + Recents. Recents needs the scrolling\n// element itself for its load-more sentinel root, so it is passed down rather\n", "import FoldersSection from './folders-section.vue'\nimport Recents from './recents.vue'\nimport SessionSelectionBar from './session-selection-bar.vue'\nimport SessionActivity from './session-activity.vue'\nimport { createSessionSelection, SessionSelectionKey } from './session-selection'\nimport '@/styles/sidebar-scroll.css'\n\nconst { t } = useI18n()\nwithDefaults(defineProps<{ activityVisible?: boolean }>(), { activityVisible: true })\n\n// The one scrollport for Folders + Recents. Recents needs the scrolling\n// element itself for its load-more sentinel root, so it is passed down rather\n", 1), ("        {{ t('chat.botSettings') }}\n      </SidebarNavButton>\n    </div>", '        {{ t(\'chat.botSettings\') }}\n      </SidebarNavButton>\n      <SidebarNavButton @click="handleManageBots">\n        <Settings2 :stroke-width="1.75" class="size-[18px]" />\n        {{ t(\'sidebar.manageBots\') }}\n      </SidebarNavButton>\n    </div>', 1), ("// Navigate to the current bot's settings overview.", "// Reuse the existing management route and dismiss the mobile drawer.\nfunction handleManageBots() {\n  void router.push({ name: 'bots' })\n  workspaceTabs.closeMobileNav()\n}\n\n// Navigate to the current bot's settings overview.", 1)],
        'apps/web/src/pages/home/composables/useBotDelegationHistory.ts': [("\n// Navigation rechecks the source relation and the target's ordinary session\n// permission. It never dispatches a turn or treats a UUID as an access grant.\nexport function useBotDelegationHistory(scope: Ref<TaskScope | null>, visible: Ref<boolean>, reader: ReturnType<typeof useBotDelegations>, navigate: (target: { bot: string, session: string, title?: string, identity: string }) => Promise<void>) {\n  const pending = ref(false), error = shallowRef<unknown>()\n  let epoch = 0, controller: AbortController | undefined\n  function invalidate() { epoch++; controller?.abort(); pending.value = false; error.value = undefined }\n  async function open() {\n    const current = scope.value, task = reader.detail.value\n    if (!current || !visible.value || pending.value || reader.error.value || reader.loading.value\n      || !task?.target_run_id || !task.target_session_id || task.source_session_id !== current.session\n      || task.delegation_id !== reader.selected.value) return\n    const generation = epoch, control = new AbortController()\n    controller = control; pending.value = true; error.value = undefined\n", "\n// Navigation rechecks the source relation and the target's ordinary session\n// permission. It never dispatches a turn or treats a UUID as an access grant.\n// Run-specific history keeps its guard; a compact session link may open an\n// admitted conversation before a run exists, using exactly the same permissions.\nexport function useBotDelegationHistory(scope: Ref<TaskScope | null>, visible: Ref<boolean>, reader: Pick<ReturnType<typeof useBotDelegations>, 'detail' | 'selected' | 'error' | 'loading'>, navigate: (target: { bot: string, session: string, title?: string, identity: string }) => Promise<void>, requireRun = true) {\n  const pending = ref(false), error = shallowRef<unknown>()\n  let epoch = 0, controller: AbortController | undefined\n  function invalidate() { epoch++; controller?.abort(); pending.value = false; error.value = undefined }\n  async function open() {\n    const current = scope.value, task = reader.detail.value\n    if (!current || !visible.value || pending.value || reader.error.value || reader.loading.value\n      || !task || (requireRun && !task.target_run_id) || !task.target_session_id || task.source_session_id !== current.session\n      || task.delegation_id !== reader.selected.value) return\n    const generation = epoch, control = new AbortController()\n    controller = control; pending.value = true; error.value = undefined\n", 1), ("      if (generation !== epoch) return\n      if (fresh.delegation_id !== task.delegation_id || fresh.source_session_id !== current.session\n        || fresh.target_bot_id !== task.target_bot_id || fresh.target_session_id !== task.target_session_id\n        || fresh.target_run_id !== task.target_run_id) throw new Error('Delegation run changed')\n      const { data: session } = await getBotsByBotIdSessionsBySessionId({\n        path: { bot_id: fresh.target_bot_id, session_id: fresh.target_session_id! },\n        signal: control.signal, cache: 'no-store', throwOnError: true,\n", "      if (generation !== epoch) return\n      if (fresh.delegation_id !== task.delegation_id || fresh.source_session_id !== current.session\n        || fresh.target_bot_id !== task.target_bot_id || fresh.target_session_id !== task.target_session_id\n        || (requireRun && fresh.target_run_id !== task.target_run_id)) throw new Error('Delegation run changed')\n      const { data: session } = await getBotsByBotIdSessionsBySessionId({\n        path: { bot_id: fresh.target_bot_id, session_id: fresh.target_session_id! },\n        signal: control.signal, cache: 'no-store', throwOnError: true,\n", 1), ("    finally { clearTimeout(timeout); if (generation === epoch) pending.value = false }\n  }\n  watch([scope, visible, reader.selected], invalidate, { flush: 'sync' })\n  watch(() => [reader.detail.value?.delegation_id, reader.detail.value?.target_bot_id, reader.detail.value?.target_session_id, reader.detail.value?.target_run_id].join(':'), invalidate, { flush: 'sync' })\n  watch(reader.error, cause => { if (cause) invalidate() }, { flush: 'sync' })\n  onScopeDispose(invalidate)\n  return computed<BotDelegationHistoryAction>(() => ({ pending: pending.value, error: error.value, open }))\n", "    finally { clearTimeout(timeout); if (generation === epoch) pending.value = false }\n  }\n  watch([scope, visible, reader.selected], invalidate, { flush: 'sync' })\n  watch(() => [reader.detail.value?.delegation_id, reader.detail.value?.target_bot_id, reader.detail.value?.target_session_id, requireRun ? reader.detail.value?.target_run_id : ''].join(':'), invalidate, { flush: 'sync' })\n  watch(reader.error, cause => { if (cause) invalidate() }, { flush: 'sync' })\n  onScopeDispose(invalidate)\n  return computed<BotDelegationHistoryAction>(() => ({ pending: pending.value, error: error.value, open }))\n", 1)],
        'apps/web/src/pages/home/components/chat-pane.vue': [('                <SessionTasks\n                  v-if="currentBotId && activeSessionId && !isWelcome"\n                  :key="`${currentBotId}:${activeSessionId}`"\n                  :bot-id="currentBotId"\n                  :session-id="activeSessionId"\n                  :selection="selectedWorkspaceTargetId"\n                  :visible="isVisible"\n                />', lock['sharedConversationMonitor']['footer']['text'], 1), ("import SessionTasks from './session-tasks.vue'", "import SessionActivityTrigger from '@/components/sidebar/session-activity-trigger.vue'", 1)],
        'apps/web/src/pages/main-section/components/mobile-nav-sheet.vue': [('          <PanelSessions\n            v-show="sidebarView === \'sessions\'"\n            class="h-full"', '          <PanelSessions\n            v-show="sidebarView === \'sessions\'"\n            :activity-visible="mobileNavOpen && sidebarView === \'sessions\'"\n            class="h-full"', 1), ('            v-if="canWorkspaceRead"\n            v-show="sidebarView === \'files\'"', '            v-if="canWorkspaceRead && filesMounted"\n            v-show="sidebarView === \'files\'"', 1), ('          <PanelSchedule\n            v-show="sidebarView === \'schedule\'"', '          <PanelSchedule\n            v-if="scheduleMounted"\n            v-show="sidebarView === \'schedule\'"', 1), ("import { computed, watch, type Component } from 'vue'", "import { computed, ref, watch, type Component } from 'vue'", 1), ('const { mobileNavOpen, sidebarView, activeId } = storeToRefs(workspaceTabs)', "const { mobileNavOpen, sidebarView, activeId } = storeToRefs(workspaceTabs)\n// Mount a panel on first selection, then retain it while this drawer stays open.\nconst filesMounted = ref(false), scheduleMounted = ref(false)\nwatch(sidebarView, view => {\n  if (view === 'files') filesMounted.value = true\n  if (view === 'schedule') scheduleMounted.value = true\n}, { immediate: true })", 1)],
    }
    assert sha(ROOT / 'mobile/memoh/shared-activity-sdk.json') == lock['sharedActivity']['sdkOverlaySHA256'], 'Activity SDK source drift'
    sdk_overlay = json.loads((ROOT / 'mobile/memoh/shared-activity-sdk.json').read_text())
    workspace = lock['sharedWorkspaceStorage']
    monitor = lock['sharedConversationMonitor']
    assert sha(ROOT / 'mobile/memoh' / monitor['chatPaneChanges']['local']) == monitor['chatPaneChanges']['sha256'], 'Conversation composer patch drift'
    shared_changes['apps/web/src/pages/home/components/chat-pane.vue'].extend(
        json.loads((ROOT / 'mobile/memoh' / monitor['chatPaneChanges']['local']).read_text()))
    assert hashlib.sha256(monitor['footer']['text'].encode()).hexdigest() == monitor['footer']['sha256'], 'Conversation footer drift'
    assert sha(ROOT / 'mobile/memoh' / monitor['phoneTestChanges']['local']) == monitor['phoneTestChanges']['sha256'], 'Phone cleanup fixture drift'
    changes['apps/web/src/store/chat-list.test.ts'] = json.loads((ROOT / 'mobile/memoh' / monitor['phoneTestChanges']['local']).read_text())
    assert sha(ROOT / 'mobile/memoh' / monitor['storeChanges']['local']) == monitor['storeChanges']['sha256'], 'Conversation store patch drift'
    conversation_store_changes = json.loads((ROOT / 'mobile/memoh' / monitor['storeChanges']['local']).read_text())
    shared_file_metadata = {**workspace['files'], **monitor['files']}
    workspace_sources = {}
    for destination, item in shared_file_metadata.items():
        source_path = ROOT / 'mobile/memoh' / item['local']
        assert sha(source_path) == item['sha256'], 'Workspace shared source drift'
        workspace_sources[destination] = source_path.read_bytes()
    for kind in ('sdkOverlay', 'locales'):
        assert sha(ROOT / 'mobile/memoh' / workspace[kind]['local']) == workspace[kind]['sha256'], 'Workspace contract drift'
    workspace_sdk = json.loads((ROOT / 'mobile/memoh' / workspace['sdkOverlay']['local']).read_text())
    for name, addition in workspace_sdk.items():
        assert name in sdk_overlay, 'Unexpected workspace SDK target'
        sdk_overlay[name] += addition
    workspace_locales = json.loads((ROOT / 'mobile/memoh' / workspace['locales']['local']).read_text())
    recovery = lock['sharedAcceptedRecovery']
    for kind in ('sdkOverlay', 'locales'):
        assert sha(ROOT / 'mobile/memoh' / recovery[kind]['local']) == recovery[kind]['sha256'], 'Accepted recovery source drift'
    recovery_sdk = json.loads((ROOT / 'mobile/memoh' / recovery['sdkOverlay']['local']).read_text())
    for name, addition in recovery_sdk.items():
        assert name in {'packages/sdk/src/types.gen.ts', 'packages/sdk/src/sdk.gen.ts', 'packages/sdk/src/index.ts', 'packages/sdk/src/@pinia/colada.gen.ts'}, 'Unexpected recovery SDK target'
        sdk_overlay[name] = sdk_overlay.get(name, '') + addition
    recovery_locales = json.loads((ROOT / 'mobile/memoh' / recovery['locales']['local']).read_text())


    # Apply this small shared UI delta after the reviewed phone adaptations.
    # Keep native attachment/voice preparation and the full existing theme set.
    composer_appearance_changes = {'apps/web/src/pages/home/components/chat-pane.vue': [('                  <!-- While voice owns the composer',
                                                           '                  <Button\n'
                                                           '                    v-if="streaming"\n'
                                                           '                    type="button"\n'
                                                           '                    variant="secondary"\n'
                                                           '                    size="icon-sm"\n'
                                                           '                    shape="circle"\n'
                                                           '                    '
                                                           ':aria-label="$t(\'chat.stopGenerating\')"\n'
                                                           '                    class="shrink-0 max-md:size-11"\n'
                                                           '                    '
                                                           '@click="chatStore.abort(paneTarget)"\n'
                                                           '                  >\n'
                                                           '                    <svg\n'
                                                           '                      viewBox="0 0 24 24"\n'
                                                           '                      fill="currentColor"\n'
                                                           '                      class="size-4 '
                                                           'max-md:size-4.5"\n'
                                                           '                      aria-hidden="true"\n'
                                                           '                    >\n'
                                                           '                      <rect\n'
                                                           '                        x="4"\n'
                                                           '                        y="4"\n'
                                                           '                        width="16"\n'
                                                           '                        height="16"\n'
                                                           '                        rx="3"\n'
                                                           '                      />\n'
                                                           '                    </svg>\n'
                                                           '                  </Button>\n'
                                                           '\n'
                                                           '                  <!-- While voice owns the composer',
                                                           1),
                                                          ('                    <!-- Send and stop are one brand '
                                                           'circle: the surface never\n'
                                                           '                         changes between the two '
                                                           'states, only the glyph cross-fades\n'
                                                           '                         (arrow ⇄ stop square), so '
                                                           "the button can't blink color or\n"
                                                           '                         shape mid-turn. While '
                                                           'streaming it stays clickable to abort. -->\n',
                                                           '                    <!-- Sending during a run uses '
                                                           'the existing follow-up queue.\n'
                                                           '                         Stop has its own control so '
                                                           'an emptied draft or double\n'
                                                           '                         click can never turn a send '
                                                           'gesture into an abort. -->\n',
                                                           1),
                                                          ('                        '
                                                           ':disabled="attachmentUploading || (streaming ? false '
                                                           ': (!showSend || !currentBotId || activeChatReadOnly '
                                                           '|| loadingMessages || composerConfigPending || '
                                                           'composerHasNoModel || goalSubmissionBlocked || '
                                                           '!!runtimeModeUnavailableReason))"',
                                                           '                        '
                                                           ':disabled="attachmentUploading || '
                                                           'nativeAttachmentBusy || !showSend || !currentBotId '
                                                           '|| activeChatReadOnly || loadingMessages || '
                                                           '(streaming ? pendingFiles.length > 0 || '
                                                           'pendingNativeAttachments.length > 0 || '
                                                           'requestedSkills.length > 0 : composerConfigPending '
                                                           '|| composerHasNoModel || goalSubmissionBlocked || '
                                                           '!!runtimeModeUnavailableReason)"',
                                                           1),
                                                          ('                        :aria-label="streaming && '
                                                           "showSend ? $t(composerQueueCommand?.mode === 'steer' "
                                                           "? 'chat.queue.enqueueSteer' : "
                                                           "'chat.queue.enqueueFollowUp') : (streaming ? 'Stop "
                                                           'generating response\' : \'Send message\')"',
                                                           '                        :aria-label="streaming && '
                                                           "showSend ? $t(composerQueueCommand?.mode === 'steer' "
                                                           "? 'chat.queue.enqueueSteer' : "
                                                           '\'chat.queue.enqueueFollowUp\') : $t(\'chat.send\')"',
                                                           1),
                                                          ('                            :class="streaming && '
                                                           "!firstSendAwaiting ? 'opacity-0' : "
                                                           '\'opacity-100\'"\n',
                                                           '',
                                                           1),
                                                          ('                          <svg\n'
                                                           '                            viewBox="0 0 24 24"\n'
                                                           '                            fill="currentColor"\n'
                                                           '                            class="col-start-1 '
                                                           'row-start-1 size-4 max-md:size-4.5 '
                                                           'transition-opacity duration-200 ease-out '
                                                           'motion-reduce:transition-none"\n'
                                                           '                            :class="streaming && '
                                                           "!firstSendAwaiting ? 'opacity-100' : "
                                                           '\'opacity-0\'"\n'
                                                           '                          >\n'
                                                           '                            <rect\n'
                                                           '                              x="4"\n'
                                                           '                              y="4"\n'
                                                           '                              width="16"\n'
                                                           '                              height="16"\n'
                                                           '                              rx="3"\n'
                                                           '                            />\n'
                                                           '                          </svg>\n',
                                                           '',
                                                           1),
                                                          ('// Streaming always wins the slot for stop, same as '
                                                           'before; unlike the old\n'
                                                           '// ring-era rule this no longer special-cases ACP: '
                                                           'the inactive occupant —\n'
                                                           '// not a dimmed disabled send — is what fills the '
                                                           'slot on empty input in\n'
                                                           '// EVERY mode (the mic when voice input is on, a '
                                                           'look-alike placeholder while\n'
                                                           "// it's shelved).\n",
                                                           '// Keep the send slot visible during a run, disabled '
                                                           'on empty input.\n'
                                                           '// The separate stop control remains available '
                                                           'alongside it.\n',
                                                           1),
                                                          ('  if (streaming.value && !showSend.value) {\n'
                                                           '    chatStore.abort(paneTarget.value)\n'
                                                           '    return\n'
                                                           '  }\n',
                                                           '',
                                                           1)],
     'apps/web/src/pages/appearance/index.vue': [('        <SettingsRow :label="t(\'settings.theme\')">',
                                                  '        <SettingsRow\n'
                                                  '          :label="t(\'settings.theme\')"\n'
                                                  '          stack="sm"\n'
                                                  '        >',
                                                  1),
                                                 ('                :title="item.label"\n'
                                                  '                :aria-label="item.label"',
                                                  '                aria-hidden="true"',
                                                  1),
                                                 ('<span class="sr-only">{{ item.label }}</span>',
                                                  '<span>{{ item.label }}</span>',
                                                  1)],
     'apps/web/src/pages/onboarding/steps/Step2Appearance.vue': [('{ Moon, Sun }', '{ Monitor, Moon, Sun }', 1),
                                                                 ('              <div class="flex gap-2">\n',
                                                                  '              <div class="flex flex-wrap '
                                                                  'gap-2">\n'
                                                                  '                <Button\n'
                                                                  '                  type="button"\n'
                                                                  '                  variant="outline"\n'
                                                                  '                  class="justify-start '
                                                                  'gap-2"\n'
                                                                  '                  :data-ui-selected="theme '
                                                                  '=== \'system\' ? \'\' : undefined"\n'
                                                                  '                  '
                                                                  '@click="setTheme(\'system\')"\n'
                                                                  '                >\n'
                                                                  '                  <Monitor class="size-4" />\n'
                                                                  '                  {{ '
                                                                  "t('settings.themeSystem') }}\n"
                                                                  '                </Button>\n',
                                                                  1)],
     'apps/web/src/i18n/locales/en.json': [('    "sendFailed":',
                                            '    "stopGenerating": "Stop generating response",\n'
                                            '    "sendFailed":',
                                            1)],
     'apps/web/src/i18n/locales/zh.json': [('    "sendFailed":',
                                            '    "stopGenerating": "停止生成回复",\n    "sendFailed":',
                                            1)],
     'apps/web/src/i18n/locales/ja.json': [('    "sendFailed":',
                                            '    "stopGenerating": "応答の生成を停止",\n    "sendFailed":',
                                            1)],
     'apps/web/src/style.css': [('@import "./styles/misans-vf.css";',
                                 '@import "./styles/misans-vf.css";\n'
                                 '\n'
                                 '/* Dark palettes use a light brand fill; its glyphs need the dark surface '
                                 'tone. */\n'
                                 ':root.dark {\n'
                                 '  --brand-foreground: var(--background);\n'
                                 '}',
                                 1)]}
    for name, replacements in composer_appearance_changes.items():
        changes.setdefault(name, []).extend(replacements)
    changes['apps/web/src/pages/appearance/index.vue'].append((
        '            :items="themeItems"',
        '            :items="themeItems"\n            class="max-w-full flex-wrap"', 1))
    patches = {}
    context = base / 'official-context.tar'
    if platform == 'harmony':
        changes['apps/web/index.html'].append(('/src/main.ts', '/src/lib/harmony-entry.ts', 1))
        changes['apps/web/src/main.ts'].extend([
            ("import { setupApiClient }", "import { readServiceConfig } from './lib/harmony-service-config'\nimport { setupApiClient }", 1),
            ('setupApiClient({', "const service = readServiceConfig()\nif (!service || service.resetPending) throw new Error('SERVICE_CONFIG_REQUIRED')\nsetupApiClient({\n  baseUrl: service.apiOrigin,", 1),
        ])
    with tarfile.open(ROOT / lock['archive'], 'r:gz') as source, tarfile.open(context, 'w') as target:
        for member in source:
            assert member.name.startswith(lock['archivePrefix']) or member.name.rstrip('/') == lock['archivePrefix'].rstrip('/')
            name = member.name[len(lock['archivePrefix']):]
            if not name:
                continue
            assert not Path(name).is_absolute() and '..' not in Path(name).parts
            entry = copy.copy(member)
            entry.name = name
            if entry.linkname.startswith(lock['archivePrefix']):
                entry.linkname = entry.linkname[len(lock['archivePrefix']):]
            data = source.extractfile(member).read() if member.isfile() else None
            if name in conversation_store_changes:
                review = conversation_store_changes[name]
                assert data is not None and hashlib.sha256(data).hexdigest() == review['baseSHA256'], 'Conversation store baseline drift'
                text = data.decode()
                for before, after, expected in review['replacements']:
                    assert text.count(before) == expected, 'Conversation store seam drift'
                    text = text.replace(before, after)
                data = text.encode()
                assert hashlib.sha256(data).hexdigest() == review['sharedSHA256'], 'Conversation shared output drift'
            if name in workspace_sources:
                assert data is not None and hashlib.sha256(data).hexdigest() == shared_file_metadata[name]['baseSHA256'], 'Workspace baseline drift'
                data = workspace_sources[name]
            if name in {f'apps/web/src/i18n/locales/{language}.json' for language in recovery_locales}:
                language = Path(name).stem
                value = json.loads(data)
                assert 'userInputRecoveryState' not in value['chat']['tools'], 'Recovery locale already present'
                value['chat']['tools']['userInputRecoveryState'] = recovery_locales[language]
                data = (json.dumps(value, ensure_ascii=False, indent=2) + '\n').encode()
            if name in {f'apps/web/src/i18n/locales/{language}.json' for language in workspace_locales}:
                language = Path(name).stem
                value = json.loads(data)
                assert 'workspaceStorage' not in value, 'Workspace locale already present'
                value['workspaceStorage'] = workspace_locales[language]['workspaceStorage']
                assert 'storage_unavailable' not in value['errors']['workspace'], 'Workspace error already present'
                value['errors']['workspace']['storage_unavailable'] = workspace_locales[language]['storageUnavailable']
                data = (json.dumps(value, ensure_ascii=False, indent=2) + '\n').encode()

            if name in sdk_overlay:
                data += sdk_overlay[name].encode('utf-8')
                entry.size = len(data)
            # Apply the fixed shared UI delta before the existing phone adapters.
            if name in shared_changes:
                shared_text = data.decode('utf-8')
                for old, new, expected in shared_changes[name]:
                    assert shared_text.count(old) == expected, ('Shared activity baseline drift', name)
                    shared_text = shared_text.replace(old, new)
                data = shared_text.encode('utf-8')
                entry.size = len(data)
            original_data = data
            # One declared, dependency-free waveform player; keep every other
            # upstream package/lock resolution byte-for-byte unchanged.
            wave = lock['nativeDependencies']['wavesurfer.js']
            if name == 'apps/web/package.json':
                text = data.decode()
                assert '"wavesurfer.js"' not in text
                data = text.replace('    "zod":', '    "wavesurfer.js": "' + wave['version'] + '",\n    "zod":').encode()
            elif name == 'pnpm-lock.yaml':
                text = data.decode()
                start = text.index('  apps/web:\n')
                end = text.index('\n  packages/', start)
                importer = text[start:end]
                assert importer.count('      zod:\n') == 1
                importer = importer.replace('      zod:\n', '      wavesurfer.js:\n        specifier: ' + wave['version'] + '\n        version: ' + wave['version'] + '\n      zod:\n')
                text = text[:start] + importer + text[end:]
                text = text.replace('\npackages:\n', '\npackages:\n\n  wavesurfer.js@' + wave['version'] + ':\n    resolution: {integrity: ' + wave['integrity'] + '}\n', 1)
                data = text.replace('\nsnapshots:\n', '\nsnapshots:\n\n  wavesurfer.js@' + wave['version'] + ': {}\n', 1).encode()
            if name in {'apps/web/package.json', 'pnpm-lock.yaml'}:
                patches[name] = {'upstreamSHA256': hashlib.sha256(original_data).hexdigest(),
                                 'adaptedSHA256': hashlib.sha256(data).hexdigest()}
            if data is not None:
                entry.size = len(data)
            if name == 'apps/web/src/pages/home/components/session-task-results.test.ts':
                # The locked shared tests assert browser anchors and synchronous
                # media URLs. Exercise the phone's real bridge/verified Blob path
                # instead, preserving the other shared task/observation tests.
                original = data.decode('utf-8')
                start = "const hash = 'a'.repeat(64)\n"
                end = "describe.each(['web', 'native'])('computer observation in %s host'"
                assert original.count(start) == original.count(end) == 1
                block = original[original.index(start):original.index(end)]
                assert hashlib.sha256(block.encode()).hexdigest() == '143633a6813ce27d15c61261600809af6e2b96c637930de18d3cba163d0959f4', 'Shared attachment assertions changed; review the phone test adapter'
                changes[name] = [
                    ("vi.mock('@memohai/sdk', () => ({", "vi.mock('@memohai/sdk', async () => ({\n  getBotsByBotIdMediaByContentHash: (await vi.importActual<typeof import('@memohai/sdk')>('@memohai/sdk')).getBotsByBotIdMediaByContentHash,", 1),
                    (block, (ROOT / 'tests/stage2/harmony_task_attachments.vitest.ts').read_text(), 1),
                ]
            if name == 'apps/web/src/pages/home/components/session-tasks.test.ts':
                # The existing phone save is a button/OS bridge, not a browser anchor.
                # Preserve the shared pagination, identity and failure assertions.
                original = data.decode('utf-8')
                start = "    const links = document.querySelector('[role=dialog]')!.querySelectorAll('a')\n"
                end = "    expect(api.deliveries.mock.lastCall![0].query.after).toBe('16')\n"
                assert original.count(start) == original.count(end) == 1
                block = original[original.index(start):original.index(end)]
                assert hashlib.sha256(block.encode()).hexdigest() == '451eb71811487e88bf49fb6a10178d69b43cdac8e268da7c62a46be54ca05384', 'Shared list attachment assertions changed'
                changes[name] = [
                    ("vi.mock('@memohai/sdk', () => ({", "vi.mock('@memohai/sdk', async () => ({\n  getBotsByBotIdMediaByContentHash: (await vi.importActual<typeof import('@memohai/sdk')>('@memohai/sdk')).getBotsByBotIdMediaByContentHash,", 1),
                    (block, (ROOT / 'tests/stage2/harmony_task_list_attachment.vitest.ts').read_text(), 1),
                ]
            if name in changes:
                original = data.decode('utf-8')
                adapted = original
                for old, new, expected in changes[name]:
                    assert adapted.count(old) == expected, (name, old)
                    adapted = adapted.replace(old, new)
                data = adapted.encode('utf-8')
                entry.size = len(data)
                patches[name] = {'upstreamSHA256': hashlib.sha256(original.encode()).hexdigest(),
                                 'adaptedSHA256': hashlib.sha256(data).hexdigest()}
            if name in lock['downstreamObservation']['files']:
                expected = lock['downstreamObservation']['files'][name]
                assert hashlib.sha256(original_data).hexdigest() == expected['baseSHA256'], 'Observation baseline drift'
                assert hashlib.sha256(data).hexdigest() == expected['sha256'], 'Observation candidate drift'
            target.addfile(entry, io.BytesIO(data) if data is not None else None)
        assert patches.keys() == changes.keys() | {'apps/web/package.json', 'pnpm-lock.yaml'}, 'Upstream host seam missing'
        assert 'apps/web/src/pages/home/components/session-task-results.test.ts' in patches, 'Phone attachment test seam missing'
        host_sources = {name: ROOT / 'mobile/memoh' / name for name in (
            'harmony.ts', 'harmony-files.ts', 'harmony-image-timing.ts', 'harmony-image-timing.test.ts', 'harmony-image-timing-node.test.ts', 'harmony-attachment-preparation.test.ts', 'harmony-voice.ts', 'harmony-voice-geometry.ts',
            'HarmonyVoiceInput.vue', 'HarmonyVoiceFan.vue', 'HarmonyVoicePlayer.vue', 'voiceFan.svg', 'voiceFan.css',
            'HarmonyAttachments.vue', 'harmony-resumable.ts',
            'harmony-download.ts', 'harmony-service-url.ts')}
        if platform == 'harmony':
            for name in ('harmony-entry.ts', 'harmony-back.ts', 'harmony-service-config.ts', 'harmony-service.css'):
                host_sources[name] = ROOT / 'mobile/memoh' / name
        host_sources['harmony-theme.json'] = ROOT / 'contracts/ui_theme.json'
        for name, path in host_sources.items():
            data = path.read_bytes()
            entry = tarfile.TarInfo('apps/web/src/lib/' + name)
            entry.size = len(data)
            entry.mode = 0o644
            target.addfile(entry, io.BytesIO(data))
        for local, destination in {'shared-session-activity.vue': 'apps/web/src/components/sidebar/session-activity.vue', 'shared-session-activity.test.ts': 'apps/web/src/components/sidebar/session-activity.test.ts', 'shared-useSessionActivity.ts': 'apps/web/src/pages/home/composables/useSessionActivity.ts', 'shared-useBotActivity.ts': 'apps/web/src/pages/home/composables/useBotActivity.ts', 'shared-useBotActivity.test.ts': 'apps/web/src/pages/home/composables/useBotActivity.test.ts'}.items():
            data = (ROOT / 'mobile/memoh' / local).read_bytes()
            assert hashlib.sha256(data).hexdigest() == lock['sharedActivity']['files'][destination], 'Shared activity source drift'
            entry = tarfile.TarInfo(destination)
            entry.size = len(data)
            entry.mode = 0o644
            target.addfile(entry, io.BytesIO(data))
        for destination, item in shared_file_metadata.items():
            if item['baseSHA256'] is not None:
                continue
            data = workspace_sources[destination]
            entry = tarfile.TarInfo(destination)
            entry.size = len(data)
            entry.mode = 0o644
            target.addfile(entry, io.BytesIO(data))
        if platform == 'harmony':
            data = public_deployment_bytes()
            entry = tarfile.TarInfo('apps/web/public/memoh_client.json')
            entry.size = len(data)
            entry.mode = 0o644
            target.addfile(entry, io.BytesIO(data))
    return context, patches


def native_build(base, context, patches, lock, before, modules, api_url, platform, pnpm):
    """Use the same pinned Vue source/dependencies without another Docker image."""
    checkout = checked(base / 'checkout')
    checkout.mkdir(parents=True, exist_ok=True)
    with tarfile.open(context) as source:
        source.extractall(checkout, filter='data')
    report = {'frontend': 'official Memoh Vue', 'builder': 'native-pnpm',
              'platform': platform, 'inputs': before, 'modules': modules,
              'version': lock['version'], 'upstreamCommit': lock['commit'],
              'hostPatches': patches, 'checks': [], 'exitCode': None,
              'apiBaseSHA256': hashlib.sha256(api_url.encode()).hexdigest(),
              'appInstalled': False, 'UIExecuted': False, 'nativeRuntime': 'NOT_EXECUTED'}
    report_path = base / 'build.json'
    environment = dict(os.environ, VITE_API_URL=api_url)

    def run(command, name):
        with (base / (name + '.log')).open('wb') as log:
            result = subprocess.run(command, cwd=checkout, env=environment,
                                    stdout=log, stderr=subprocess.STDOUT)
        report['checks'].append({'name': name, 'exitCode': result.returncode})
        report_path.write_text(json.dumps(report, indent=2) + '\n')
        assert result.returncode == 0, 'Frontend check failed: ' + name

    run(['node', 'scripts/ensure-submodules.mjs', '--check'], 'submodule-check')
    run([pnpm, '--filter', '@memohai/web...', 'install', '--frozen-lockfile',
         '--ignore-scripts', '--store-dir', str(ROOT / 'tests/.isolated/pnpm-store')], 'dependencies')
    run(['node', 'scripts/check-patched-deps.mjs'], 'patched-dependencies')
    run([pnpm, '--filter', '@memohai/web', 'typecheck'], 'typecheck')
    run([pnpm, '--filter', '@memohai/web', 'build'], 'vue-build')
    assert before == inputs(), 'Frontend host inputs changed during build'
    verify_modules(modules)
    output = checkout / 'apps/web/dist'
    assert (output / 'index.html').is_file()
    dist = checked(base / 'official-dist')
    if dist.exists():
        # Only replace a previous output owned by this build entry.
        receipt = base / 'native-output.json'
        assert receipt.is_file(), 'Unowned frontend output'
        actual = {p.relative_to(dist).as_posix(): sha(p) for p in dist.rglob('*') if p.is_file()}
        assert actual == json.loads(receipt.read_text()), 'Frontend output drift'
        shutil.rmtree(dist)
    shutil.copytree(output, dist)
    package_notices(dist, (checkout / 'LICENSE').read_bytes())
    resources = {p.relative_to(dist).as_posix(): sha(p) for p in dist.rglob('*') if p.is_file()}
    (base / 'native-output.json').write_text(json.dumps(resources, indent=2) + '\n')
    report.update(exitCode=0, resources=resources)
    report_path.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'officialVueBuild': True, 'builder': 'native-pnpm',
                      'platform': platform, 'resources': len(resources), 'nativeRuntime': 'NOT_EXECUTED'}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', choices=('harmony', 'web'), default='harmony')
    parser.add_argument('--builder', choices=('native', 'docker'), default='native')
    parser.add_argument('--pnpm', default='pnpm', help='Pinned pnpm 10 executable')
    args = parser.parse_args()
    base = BASE if args.platform == 'harmony' else ROOT / 'mobile/.build/memoh-web-browser'
    dist = base / 'official-dist'
    checked(base).mkdir(parents=True, exist_ok=True)
    previous = json.loads((base / 'build.json').read_text()) if (base / 'build.json').exists() else {}
    # A failed image export must not discard ownership of the last packaged output.
    package_receipt = base / 'resource-package.json'
    previous_resources = previous.get('resources')
    if previous_resources is None and package_receipt.exists():
        previous_resources = json.loads(package_receipt.read_text())['resources']
    lock = json.loads(PIN.read_text())
    modules = {lock['archive']: lock['archiveSHA256']}
    verify_modules(modules)
    before = inputs()
    context, patches = context_archive(lock, base, args.platform)
    # Native selects the validated API base at runtime before importing main.
    # No server address or credential is baked into the distributable.
    api_url = '/api'
    if args.builder == 'native':
        native_build(base, context, patches, lock, before, modules, api_url, args.platform, args.pnpm)
        return
    target_inputs = {'platform': args.platform, 'apiBaseSHA256': hashlib.sha256(api_url.encode()).hexdigest(), 'inputs': before, 'modules': modules}
    source_identity = hashlib.sha256(json.dumps(target_inputs, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
    docker_config = checked(base / 'docker-public-config')
    docker_config.mkdir(exist_ok=True)
    buildx = shutil.which('docker-buildx')
    assert buildx, 'Use the existing official Docker Buildx plugin'
    (docker_config / 'config.json').write_text(json.dumps({'cliPluginsExtraDirs': [str(Path(buildx).parent)]}))
    docker = ['docker', '--config', str(docker_config)]
    report = {**target_inputs, 'sourceIdentity': source_identity, 'frontend': 'official Memoh Vue',
              'version': lock['version'], 'upstreamCommit': lock['commit'],
              'upstreamRecipe': lock['buildRecipe'], 'hostPatches': patches,
              'checks': [], 'appInstalled': False, 'UIExecuted': False,
              'resources': previous_resources}

    def run(args, name, stdin=None):
        with (base / (name + '.log')).open('wb') as log:
            result = subprocess.run(docker + args, cwd=ROOT, stdin=stdin, stdout=log, stderr=subprocess.STDOUT, env={**os.environ, 'DOCKER_BUILDKIT': '1'})
        report['checks'].append({'name': name, 'exitCode': result.returncode})
        (base / 'build.json').write_text(json.dumps(report, indent=2) + '\n')
        assert result.returncode == 0, 'Official frontend build failed; inspect ' + name + '.log'

    tag = 'memoh-web-' + args.platform + ':build'
    with context.open('rb') as stream:
        run(['buildx', 'build', '--load', '--target', lock['buildTarget'], '-f', lock['buildRecipe'],
             '--build-arg', 'VITE_API_URL=' + api_url, '-t', tag, '-'], 'official-vue-build', stream)
    if dist.exists():
        actual = {p.relative_to(dist).as_posix(): sha(p) for p in dist.rglob('*') if p.is_file()}
        assert previous.get('frontend') == 'official Memoh Vue' and actual == previous_resources, 'Unowned frontend output'
        assert dist.resolve().is_relative_to(base.resolve())
        for path in sorted(dist.rglob('*'), key=lambda p: len(p.parts), reverse=True):
            checked(path)
            if path.is_file(): path.unlink()
            elif path.is_dir(): path.rmdir()
        dist.rmdir()
    container = 'memoh-web-output-' + uuid.uuid4().hex[:12]
    run(['create', '--name', container, tag], 'output-container-create')
    try:
        run(['cp', container + ':/build/apps/web/dist', str(dist)], 'official-dist-copy')
    finally:
        run(['rm', container], 'output-container-remove')
    assert before == inputs(), 'Frontend host inputs changed during build'
    verify_modules(modules)
    with tarfile.open(ROOT / lock['archive'], 'r:gz') as source:
        package_notices(dist, source.extractfile(lock['archivePrefix'] + 'LICENSE').read())
    image = subprocess.run(docker + ['image', 'inspect', tag, '--format', '{{.Id}}'], capture_output=True, text=True, check=True)
    report['builderImageID'] = image.stdout.strip()
    resources = {p.relative_to(dist).as_posix(): sha(p) for p in dist.rglob('*') if p.is_file()}
    assert 'index.html' in resources
    report.update(exitCode=0, resources=resources)
    (base / 'build.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'officialVueBuild': True, 'platform': args.platform,
                      'sourceIdentity': source_identity, 'version': lock['version'], 'resources': len(resources),
                      'hostPatchedUpstreamFiles': len(patches), 'installedOrUIExecuted': False}))


if __name__ == '__main__':
    main()
