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
from client_config import read_config

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'tools'))
from artifact_paths import checked
BASE = ROOT / 'mobile/.build/memoh-web'
DIST = BASE / 'official-dist'
PIN = ROOT / 'config/memoh_web.lock.json'


def sha(path):
    return hashlib.sha256(checked(path).read_bytes()).hexdigest()


def inputs():
    files = [ROOT / name for name in (
        'mobile/build_memoh_web.py', 'mobile/memoh/harmony.ts', 'mobile/memoh/harmony-files.ts',
        'mobile/memoh/harmony-voice.ts', 'mobile/memoh/harmony-voice-geometry.ts',
        'mobile/memoh/HarmonyVoiceInput.vue', 'mobile/memoh/HarmonyVoiceFan.vue', 'mobile/memoh/HarmonyVoicePlayer.vue',
        'mobile/memoh/voiceFan.svg', 'mobile/memoh/voiceFan.css', 'contracts/ui_theme.json',
        'mobile/memoh/licenses/lucide.txt', 'mobile/memoh/licenses/wavesurfer.txt',
        'config/memoh_web.lock.json', 'config/memoh_client.json',
        'mobile/client_config.py', 'mobile/prepare_memoh_source.py')]
    return {p.relative_to(ROOT).as_posix(): sha(p) for p in files}


def verify_modules(modules):
    lock = json.loads(PIN.read_text())
    assert modules == {lock['archive']: lock['archiveSHA256']}, 'Use the complete official release source'
    assert all(sha(ROOT / name) == digest for name, digest in modules.items()), 'Upstream archive drift'


def context_archive(lock, base=BASE):
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
    warm_css = '\n:root.warm {\n  color-scheme: light;\n' + ''.join(
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
        'apps/web/tsconfig.app.json': [
            ('"rootDir": "."', '"rootDir": "../.."', 1),
            ('"@/*": ["./src/*"]', '"@/*": ["./src/*"],\n      "#/*": ["../../packages/ui/src/*"]', 1),
            ('"#/*": ["../../packages/ui/src/*"]', '"#/*": ["../../packages/ui/src/*"],\n      "vue": ["./node_modules/vue"]', 1),
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
        'apps/web/src/pages/home/components/chat-pane.vue': [(':open-index="galleryOpenIndex"', ':open-index="galleryOpenIndex"\n        :origin="galleryOrigin"', 1), (':open-index="composerPreviewIndex"', ':open-index="composerPreviewIndex"\n        :origin="composerPreviewOrigin"', 1), ('  openBySrc: galleryOpenBySrc,', '  openBySrc: galleryOpenBySrc,\n  origin: galleryOrigin,', 1), ('  composerPreviewIndex,', '  composerPreviewIndex,\n  composerPreviewOrigin,', 1), ('const paneView = computed(() => chatStore.chatView(paneTarget.value))', 'const paneView = computed(() => chatStore.chatView(paneTarget.value))\nconst attachmentPreparing = computed(() => !!paneView.value.attachmentPreparation.value)', 1), (':disabled="!currentBotId || activeChatReadOnly || loadingMessages || firstSendAwaiting || attachmentUploading"', ':disabled="!currentBotId || activeChatReadOnly || loadingMessages || firstSendAwaiting || attachmentUploading || attachmentPreparing"', 1), ('    && isActive.value && isVisible.value,\n  onPaste: handlePaste,', '    && isActive.value && isVisible.value && !attachmentPreparing.value,\n  onPaste: handlePaste,', 1), ('function handleSendButton() {', 'function handleSendButton() {\n  if (paneView.value.attachmentPreparation.value && !showSend.value) {\n    paneView.value.attachmentPreparation.value.abort()\n    return\n  }', 1), ('            <input\n              ref="fileInput"', '            <input ref="harmonyPhotoInput" type="file" accept="image/*" multiple class="hidden" @change="handleFileInputChange">\n            <input\n              ref="fileInput"', 1), ('                    <DropdownMenuItem\n                      :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages"\n                      @select="fileInput?.click()"', '                    <DropdownMenuItem\n                      :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages"\n                      @select="harmonyPhotoInput?.click()"\n                    >\n                      <ImagePlus />\n                      <span class="min-w-0 flex-1 truncate">{{ $t(\'chat.attachPhotos\') }}</span>\n                    </DropdownMenuItem>\n                    <DropdownMenuItem\n                      :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages"\n                      @select="fileInput?.click()"', 1), ('const { t, te, locale } = useI18n()', 'const harmonyPhotoInput = ref<HTMLInputElement>()\nconst { t, te, locale } = useI18n()', 1), (', type ChatAttachment, type CommandActionListItem', ', type CommandActionListItem', 1), ('import { ATTACHMENT_ANIM_MS,', "import { prepareSubmission } from '@/lib/harmony-files'\nimport { nativeVoiceAvailable, type VoiceSubmission } from '@/lib/harmony-voice'\nimport HarmonyVoiceInput from '@/lib/HarmonyVoiceInput.vue'\nimport HarmonyVoicePlayer from '@/lib/HarmonyVoicePlayer.vue'\nimport { ATTACHMENT_ANIM_MS,", 1), ('                        <ChatAttachmentCard\n                          v-for="preview in pendingPreviews"\n                          :key="preview.key"', '                        <template v-for="preview in pendingPreviews" :key="preview.key">\n                        <HarmonyVoicePlayer v-if="preview.file.type.startsWith(\'audio/\')" :file="preview.file" removable @remove="removeAttachment(preview.i)" />\n                        <ChatAttachmentCard\n                          v-else', 1), ('                          @preview="preview.isPasted ? (pastedViewerText = preview.pastedText) : openComposerPreview(preview.url)"\n                        />', '                          @preview="preview.isPasted ? (pastedViewerText = preview.pastedText) : openComposerPreview(preview.url)"\n                        />\n                        </template>', 1), (':clickable="preview.isPasted || (preview.isMedia && !!preview.url)"', ':clickable="preview.isPasted || preview.isText || (preview.isMedia && !!preview.url)"', 1), ('openComposerPreview(preview.url)"', 'openComposerPreview(preview.url, $event, preview.file).catch(error => { composerError = error instanceof Error ? error.message : String(error) })"', 1), ("{{ $t('chat.pastedViewerTitle') }}", "{{ $t('common.preview') }}", 1), ('async function handleSend() {', 'function harmonyVoiceEditorTarget() {\n  const resolve = () => {\n    const node = textareaEl.value\n    if (!node) return\n    const rect = node.getBoundingClientRect()\n    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height, radius: Number.parseFloat(getComputedStyle(node).borderRadius) || 0 }\n  }\n  const bounds = resolve()\n  return bounds ? { ...bounds, resolve } : undefined\n}\nfunction insertHarmonyVoiceText(text: string) {\n  const field = textareaEl.value\n  if (!field || !text.trim()) return\n  const start = field.selectionStart, end = field.selectionEnd\n  inputText.value = inputText.value.slice(0, start) + text + inputText.value.slice(end)\n  void nextTick(() => field.setSelectionRange(start + text.length, start + text.length))\n}\nasync function handleSend() { return sendComposer() }\nasync function sendComposer(voice?: VoiceSubmission) {\n  if (paneView.value.attachmentPreparation.value) return', 1), ('(!text && !files.length && !skills.length)', '(!text && !files.length && !skills.length && !voice)', 1), ("  let attachments: ChatAttachment[] | undefined\n  try {\n    if (files.length) {\n      attachments = await uploadAttachments(sentContext.target.botId, files)\n    }\n  } catch (error) {\n    pairSend.releaseReads()\n    draftSendStarting.value = false\n    if (!matchesChatPaneSendContext(\n      sentContext,\n      paneTarget.value,\n      inputDraftKey.value || 'chat',\n    )) return\n    // The draft remains visible throughout upload; cancellation must not\n    // resurrect a removed file or overwrite a changed composer.\n    if (error instanceof DOMException && error.name === 'AbortError') return\n    composerError.value = error instanceof AttachmentUploadLimitError\n      ? t('chat.attachmentUploadTooLarge', { name: error.fileName, limit: error.maxBytes.toLocaleString() })\n      : resolveApiErrorMessage(error, t('chat.sendFailed'))\n    return\n  }\n", '  preparingView = paneView.value\n  const nativeSubmission = prepareSubmission(sentContext.target.botId, files, voice)\n  const attachments = nativeSubmission.attachments\n', 1), ('const result = await chatStore.sendMessage(sendText, attachments, {', 'const result = await chatStore.sendMessage(sendText, attachments, {\n    prepareAttachments: nativeSubmission.prepareAttachments,', 1), ("    pendingFiles.value = files\n    requestedSkills.value = skills\n    if (commandPanelEvent.value?.type !== 'command_error')", "    pendingFiles.value = nativeSubmission.files\n    requestedSkills.value = skills\n    if (commandPanelEvent.value?.type !== 'command_error')", 1), ('                  <!-- While voice owns the composer', '                  <HarmonyVoiceInput\n                    v-if="nativeVoiceAvailable"\n                    :key="inputDraftKey"\n                    :disabled="!currentBotId || activeChatReadOnly || streaming || loadingMessages || composerConfigPending || composerHasNoModel || goalSubmissionBlocked || !!runtimeModeUnavailableReason"\n                    :editor-target="harmonyVoiceEditorTarget"\n                    @send="sendComposer"\n                    @transcribe="insertHarmonyVoiceText"\n                    @error="composerError = $event instanceof Error ? $event.message : $t(\'chat.sendFailed\')"\n                  />\n                  <!-- While voice owns the composer', 1), ('                    v-else\n                    class="relative size-8 max-md:size-11 shrink-0"', '                    v-else\n                    v-show="!nativeVoiceAvailable || !inactiveSlotVisible"\n                    class="relative size-8 max-md:size-11 shrink-0"', 1), ('<script setup lang="ts">', '<script setup lang="ts">\nimport type { ComponentPublicInstance } from \'vue\'', 1), ('ref="composerEl"', ':ref="setComposerElement"', 1), ('\n\n</script>', '\n\nfunction setComposerElement(element: Element | ComponentPublicInstance | null) {\n  composerEl.value = element instanceof HTMLElement ? element : null\n}\n</script>', 1), ('                        <Spinner\n                          v-if="voiceInputState === \'transcribing\'"\n                          class="size-4 max-md:size-5"\n                        />\n                        <svg\n                          v-else', '                        <svg', 1), ('                          :class="voiceInputState === \'recording\' ? \'motion-safe:animate-pulse\' : undefined"\n', '', 1), ('for (let i = 0; i < dataArray.length; i++) sum += dataArray[i] ** 2', 'for (const sample of dataArray) sum += sample ** 2', 1), ('  const pairSend = composerPair.captureSend()\n  const sentModelId', '  const sentInput = inputText.value\n  const sentField = textareaEl.value\n  const sentSelection = sentField ? {\n    start: sentField.selectionStart,\n    end: sentField.selectionEnd,\n    direction: sentField.selectionDirection,\n  } : null\n  const pairSend = composerPair.captureSend()\n  const sentModelId', 1), ('    inputText.value = restore.input\n    saveInputDraft(sentDraftKey, restore.input)', '    inputText.value = sentInput\n    saveInputDraft(sentDraftKey, sentInput)', 1), ('      composerError.value = restore.error\n    }\n    return', "      composerError.value = restore.error\n    }\n    await nextTick()\n    if (sentSelection && inputText.value === sentInput\n      && matchesChatPaneSendContext(sentContext, paneTarget.value, inputDraftKey.value || 'chat')) {\n      textareaEl.value?.setSelectionRange(sentSelection.start, sentSelection.end, sentSelection.direction)\n    }\n    return", 1), ("import { AttachmentUploadLimitError, useComposerUpload } from '../composables/useComposerUpload'", '', 1), ('const {\n  uploading: attachmentUploading,\n  upload: uploadAttachments,\n  cancel: cancelAttachmentUpload,\n} = useComposerUpload()', 'let preparingView: ReturnType<typeof chatStore.chatView> | undefined\nconst attachmentUploading = computed(() => !!preparingView?.attachmentPreparation.value)\nfunction cancelAttachmentUpload() { preparingView?.attachmentPreparation.value?.abort() }\nonBeforeUnmount(cancelAttachmentUpload)', 1), ('onDeactivated(() => {\n  onDeactivatedResetScroll()', 'onDeactivated(() => {\n  cancelAttachmentUpload()\n  onDeactivatedResetScroll()', 1), ('const attachmentUploading = computed(() => !!preparingView?.attachmentPreparation.value)', 'const attachmentUploading = computed(() => !!paneView.value.attachmentPreparation.value)', 1)],
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
        'apps/web/src/store/chat/send.ts': [("import type { Ref } from 'vue'", "import { attachmentForWire, verifyAttachmentReferences } from '@/lib/harmony-files'\nimport type { Ref } from 'vue'", 1), ('    const inband = wasDraft && !isExternalAgent', '    const preparationController = options.prepareAttachments || attachments?.length ? new AbortController() : null\n    if (preparationController) initialView.attachmentPreparation.value = preparationController\n    const inband = wasDraft && !isExternalAgent', 1), ('    try {\n      options.onBeforeMessageSend?.()', "    try {\n      if (preparationController) {\n        try {\n          if (options.prepareAttachments) attachments = await options.prepareAttachments(preparationController.signal)\n          else await verifyAttachmentReferences(viewTarget.botId, attachments ?? [], preparationController.signal)\n          preparationController.signal.throwIfAborted()\n        } catch (error) {\n          if (error instanceof Error && error.name === 'AbortError') throw new Error('附件传输已取消', { cause: error })\n          throw error\n        }\n      }\n      options.onBeforeMessageSend?.()", 1), ('      const appendTurns = () => {', '      // The exact batch is ready; draft clearing must not cancel its own send.\n      if (initialView.attachmentPreparation.value === preparationController) initialView.attachmentPreparation.value = null\n      const appendTurns = () => {', 1), ('        attachments,\n        requested_skills:', '        attachments: attachments?.map(attachmentForWire),\n        requested_skills:', 1), ('    } finally {\n      stopConfirmationWatch()', '    } finally {\n      if (preparationController && initialView.attachmentPreparation.value === preparationController) {\n        preparationController.abort()\n        initialView.attachmentPreparation.value = null\n      }\n      stopConfirmationWatch()', 1)],
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
            ('function getUrl(att: AttachmentItem): string {\n  return resolveUrl(att)\n}', "// This component only exposes complete, verified NAS image bytes.\nconst imageUrls = reactive<Record<string, string>>({})\n\nfunction imageReferenceKey(att: AttachmentItem): string {\n  return JSON.stringify([att.content_hash, att.bot_id ?? att.metadata?.bot_id,\n    att.storage_key ?? att.metadata?.storage_key, att.size, att.mime])\n}\n\nwatch(() => JSON.stringify([...new Set(props.block.attachments.filter(isImage).map(imageReferenceKey))].sort()),\n  (_keys, _previous, onCleanup) => {\n    const controller = new AbortController()\n    const urls: string[] = []\n    const seen = new Set<string>()\n    onCleanup(() => {\n      controller.abort()\n      for (const url of urls) URL.revokeObjectURL(url)\n      for (const key of Object.keys(imageUrls)) delete imageUrls[key]\n    })\n    for (const att of props.block.attachments.filter(isImage)) {\n      const key = imageReferenceKey(att)\n      if (seen.has(key)) continue\n      seen.add(key)\n      void originalBlob(att, controller.signal).then((blob) => {\n        if (controller.signal.aborted) return\n        const url = URL.createObjectURL(blob)\n        urls.push(url)\n        imageUrls[key] = url\n      }).catch(() => { /* Existing non-accessible file card stays visible. */ })\n    }\n  }, { immediate: true })\n\nfunction getUrl(att: AttachmentItem): string {\n  return isImage(att) ? imageUrls[imageReferenceKey(att)] || '' : resolveUrl(att)\n}", 1),
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
            ('treePollTimer: ReturnType<typeof setInterval>', 'treePollTimer: number', 1),
        ],
        'apps/web/src/pages/home/composables/useFileManagerProvider.ts': [
            ('  contentHash?: string', '  contentHash?: string\n  storageKey?: string\n  size?: number\n  mime?: string', 1),
        ],
        'apps/web/src/pages/home/components/dockview/panel-asset.vue': [
            ('import { sdkApiUrl, sdkAuthQuery }', "import { saveOriginal } from '@/lib/harmony-files'\nimport { sdkApiUrl, sdkAuthQuery }", 1),
            ('contentHash?: string, src?: string', 'contentHash?: string, storageKey?: string, size?: number, mime?: string, src?: string', 1),
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
      size: original.size, mime: original.mime })
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
            ("import type { ChatMessage } from '@/store/chat-list'", "import type { ChatMessage } from '@/store/chat-list'\nimport type { UIAttachment } from '@/composables/api/useChat'", 1),
            ('att: Record<string, unknown>', 'att: UIAttachment', 4),
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

watch(currentItem, (item, _previous, onCleanup) => {
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
            ('function close() {', "type ImagePoint = { x: number; y: number }\ntype ImagePose = ImagePoint & { scale: number }\ntype ImageBounds = { width: number; height: number; center: ImagePoint; viewportWidth: number; viewportHeight: number }\n\nconst imagePointers = new Map<number, ImagePoint>()\nlet interactionImage: HTMLImageElement | null = null\nlet imagePose: ImagePose = { scale: 1, x: 0, y: 0 }\nlet imageBounds: ImageBounds | null = null\nlet imageAnchor: { points: ImagePoint[]; pose: ImagePose } | null = null\nlet imageClickConsumed = false\nlet imageMotionActive = false\n\nfunction boundImagePose(pose: ImagePose, bounds: ImageBounds): ImagePose {\n  const scale = Math.max(1, pose.scale)\n  const xLimit = Math.max(0, (bounds.width * scale - bounds.viewportWidth) / 2)\n  const yLimit = Math.max(0, (bounds.height * scale - bounds.viewportHeight) / 2)\n  return { scale, x: Math.max(-xLimit, Math.min(xLimit, pose.x)), y: Math.max(-yLimit, Math.min(yLimit, pose.y)) }\n}\n\nfunction projectImagePose(points: ImagePoint[], anchor: { points: ImagePoint[]; pose: ImagePose }, bounds: ImageBounds): ImagePose {\n  const first = points[0]\n  const start = anchor.points[0]\n  if (!first || !start) return anchor.pose\n  if (points[1] && anchor.points[1]) {\n    const second = points[1]\n    const startSecond = anchor.points[1]\n    const distance = Math.hypot(second.x - first.x, second.y - first.y)\n    const startDistance = Math.hypot(startSecond.x - start.x, startSecond.y - start.y)\n    if (!startDistance) return anchor.pose\n    const scale = Math.max(1, anchor.pose.scale * distance / startDistance)\n    const startX = (start.x + startSecond.x) / 2 - bounds.center.x\n    const startY = (start.y + startSecond.y) / 2 - bounds.center.y\n    return boundImagePose({ scale,\n      x: (first.x + second.x) / 2 - bounds.center.x - (startX - anchor.pose.x) * scale / anchor.pose.scale,\n      y: (first.y + second.y) / 2 - bounds.center.y - (startY - anchor.pose.y) * scale / anchor.pose.scale,\n    }, bounds)\n  }\n  return boundImagePose({ scale: anchor.pose.scale,\n    x: anchor.pose.x + first.x - start.x, y: anchor.pose.y + first.y - start.y,\n  }, bounds)\n}\n\nfunction clearImagePointers() {\n  const ids = [...imagePointers.keys()]\n  imagePointers.clear()\n  imageAnchor = null\n  for (const id of ids) {\n    if (interactionImage?.hasPointerCapture(id)) interactionImage.releasePointerCapture(id)\n  }\n}\n\nfunction resetImageInteraction() {\n  clearImagePointers()\n  interactionImage?.style.removeProperty('transform')\n  interactionImage?.style.removeProperty('transition')\n  interactionImage = null\n  imageBounds = null\n  imagePose = { scale: 1, x: 0, y: 0 }\n  imageClickConsumed = false\n  imageMotionActive = false\n}\n\nfunction rebaseImageGesture() {\n  const image = interactionImage\n  const dialog = dialogRef.value\n  if (!image || !dialog || imageMotionActive || !imagePointers.size) return\n  const rect = image.getBoundingClientRect()\n  const viewport = dialog.getBoundingClientRect()\n  if (!rect.width || !rect.height || !viewport.width || !viewport.height) return\n  imageBounds = { width: rect.width / imagePose.scale, height: rect.height / imagePose.scale,\n    center: { x: rect.left + rect.width / 2 - imagePose.x, y: rect.top + rect.height / 2 - imagePose.y },\n    viewportWidth: viewport.width, viewportHeight: viewport.height }\n  imageAnchor = { points: [...imagePointers.values()].slice(0, 2), pose: { ...imagePose } }\n  // Only the actual image transform changes during direct manipulation.\n  image.style.transition = 'none'\n}\n\nfunction imagePointerDown(event: PointerEvent) {\n  if (event.pointerType !== 'touch' || !(event.currentTarget instanceof HTMLImageElement)) return\n  if (!imagePointers.size) imageClickConsumed = false\n  interactionImage = event.currentTarget\n  imagePointers.set(event.pointerId, { x: event.clientX, y: event.clientY })\n  interactionImage.setPointerCapture(event.pointerId)\n  if (imagePointers.size > 1) imageClickConsumed = true\n  rebaseImageGesture()\n}\n\nfunction imagePointerMove(event: PointerEvent) {\n  if (!imagePointers.has(event.pointerId)) return\n  imagePointers.set(event.pointerId, { x: event.clientX, y: event.clientY })\n  if (imageMotionActive) return\n  if (!imageAnchor || !imageBounds) { rebaseImageGesture(); return }\n  // Coincident fingers have no scale baseline; establish one without a jump.\n  const first = imageAnchor.points[0]\n  const second = imageAnchor.points[1]\n  if (first && second && first.x === second.x && first.y === second.y) {\n    rebaseImageGesture()\n    return\n  }\n  const next = projectImagePose([...imagePointers.values()].slice(0, 2), imageAnchor, imageBounds)\n  if (next.scale === imagePose.scale && next.x === imagePose.x && next.y === imagePose.y) return\n  imageClickConsumed = true\n  imagePose = next\n  interactionImage!.style.transform = `translate(${imagePose.x}px, ${imagePose.y}px) scale(${imagePose.scale})`\n}\n\nfunction imagePointerUp(event: PointerEvent) {\n  if (!imagePointers.delete(event.pointerId)) return\n  if (interactionImage?.hasPointerCapture(event.pointerId)) interactionImage.releasePointerCapture(event.pointerId)\n  imageAnchor = null\n  rebaseImageGesture()\n}\n\nfunction imagePointerCancelled(event: PointerEvent) {\n  if (!imagePointers.has(event.pointerId)) return\n  imageClickConsumed = true\n  imagePointerUp(event)\n}\n\nfunction imageContextMenu() {\n  // Observe only. ArkWeb's existing authenticated system sheet remains in charge.\n  imageClickConsumed = true\n  clearImagePointers()\n}\n\nfunction imageClick(event: MouseEvent) {\n  if (imageClickConsumed) { event.preventDefault(); event.stopPropagation(); return }\n  close()\n}\n\nfunction imageEntered() {\n  imageMotionActive = false\n  // Touches begun during the thumbnail transition start from its settled pose.\n  rebaseImageGesture()\n}\n\nfunction beginImageLeave(element: Element) {\n  imageMotionActive = true\n  const dialog = element as HTMLElement\n  const image = element.querySelector<HTMLImageElement>('.lightbox-media img')\n  if (image) {\n    // Vue's leave-from class keeps this exact pose until the return transition.\n    dialog.style.setProperty('--lightbox-close-from-transform', getComputedStyle(image).transform)\n    image.style.removeProperty('transform')\n    image.style.removeProperty('transition')\n  }\n  clearImagePointers()\n}\n\nwatch(() => [props.openIndex, currentItem.value?.src], () => {\n  if (!isOpen.value) return // Keep the closing pose until Vue's after-leave.\n  const hadPointers = imagePointers.size > 0\n  resetImageInteraction()\n  imageClickConsumed = hadPointers\n})\nonBeforeUnmount(resetImageInteraction)\n\nfunction close() {", 1),
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
            ("type AnswersNode = { kind: 'answers'; key: string; block: ContentBlock; index: number }", "type AnswersNode = { kind: 'answers'; key: string; block: ToolCallBlockType; index: number }", 1),
            ('if (isCompletedAskUser(block)) {', "if (block.type === 'tool' && isCompletedAskUser(block)) {", 1),
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
            ('externalPollTimer: ReturnType<typeof setInterval>', 'externalPollTimer: number', 1),
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
        'apps/web/src/pages/home/components/dockview/panel-preview.vue': [
            ('previewPollTimer: ReturnType<typeof setInterval>', 'previewPollTimer: number', 1),
        ],
        'apps/web/src/components/bot-select/index.vue': [
            ('@update:model-value="$emit(\'update:modelValue\', $event)"', '@update:model-value="typeof $event === \'string\' && $emit(\'update:modelValue\', $event)"', 1),
        ],
        'apps/web/src/components/config-schema-form/index.vue': [
            ('function updateNumber(path: string, value: string)', 'function updateNumber(path: string, value: string | number)', 1),
            ('(val: string) => updateValue(field.key, val)', '(val: unknown) => updateValue(field.key, val)', 4),
            ('(val: string) => updateNumber(field.key, val)', '(val: string | number) => updateNumber(field.key, val)', 1),
        ],
        'apps/web/src/components/create-model/index.vue': [
            ('(val: boolean) => toggleCompat(opt.value, val)', '(val) => toggleCompat(opt.value, val === true)', 1),
            ('contextWindow: form.values.context_window ?? (isEdit ? fallback!.config?.context_window : undefined),', 'contextWindow: Number(form.values.context_window ?? (isEdit ? fallback!.config?.context_window : undefined)),', 1),
            ('        return updateModelByLegacyModelID({ modelId: fallback!.model_id, data })', "        const originalModelId = fallback?.model_id\n        if (!originalModelId) throw new Error(t('models.modelIdRequired'))\n        return updateModelByLegacyModelID({ modelId: originalModelId, data })", 1),
        ],
        'apps/web/src/components/file-manager/file-icon.ts': [
            ('EXTENSION_LANGUAGE[parts[parts.length - 1]]', "EXTENSION_LANGUAGE[parts[parts.length - 1] ?? '']", 1),
            ('seti.iconDefinitions[key] ?? seti.iconDefinitions[seti.file]', 'seti.iconDefinitions[key] ?? theme.iconDefinitions._default', 1),
        ],
        'apps/web/src/components/key-value-editor/index.vue': [
            ('  list[index] = { ...list[index], [field]: val }', '  const row = list[index]\n  if (!row) return\n  list[index] = { ...row, [field]: val }', 1),
        ],
        'apps/web/src/components/sidebar/bot-switcher.vue': [
            ('          next.splice(newIndex, 0, moved)', '          if (!moved) return\n          next.splice(newIndex, 0, moved)', 1),
        ],
        'apps/web/src/components/sidebar/folder-create-dialog.vue': [
            ('computed<WorkspaceWorkspaceTarget[]>(() => (', 'computed<Array<WorkspaceWorkspaceTarget & { target_id: string }>>(() => (', 1),
            (".filter(target => (target.target_id ?? '').trim())", ".filter((target): target is WorkspaceWorkspaceTarget & { target_id: string } => typeof target.target_id === 'string' && target.target_id.trim().length > 0)", 1),
        ],
        'apps/web/src/composables/useListboxKeyboard.ts': [
            ('function activate(rowIndex: number) {', 'function activate(rowIndex: number | undefined) {\n    if (rowIndex === undefined) return', 1),
        ],
        'apps/web/src/constants/color-schemes.ts': [
            ('colorSchemes: ColorSchemeOption[]', 'colorSchemes: [ColorSchemeOption, ...ColorSchemeOption[]]', 1),
        ],
        'apps/web/src/lib/api-client.test.ts': [
            ('const fetchMock = vi.fn(async () =>', 'const fetchMock = vi.fn(async (_request: Request) =>', 11),
        ],
        'apps/web/src/lib/query-cache-persistence.test.ts': [
            ('  return {\n    data: new Map<string, string>(Object.entries(initial ?? {})),', '  const data = new Map<string, string>(Object.entries(initial ?? {}))\n  return {\n    data,', 1),
            ('this.data.', 'data.', 3),
        ],
        'apps/web/src/lib/upload-with-progress.ts': [
            ('reject(new Error(detail))', 'reject(new Error(String(detail)))', 1),
        ],
        'apps/web/src/pages/bots/components/bot-desktop.vue': [
            ('SettingsSettings', 'SettingsUpsertRequest', 2),
            ('body: Partial<SettingsUpsertRequest>', 'body: SettingsUpsertRequest', 1),
        ],
        'apps/web/src/pages/bots/components/bot-network.vue': [
            ('SettingsSettings', 'SettingsUpsertRequest', 2),
            ('body: Partial<SettingsUpsertRequest>', 'body: SettingsUpsertRequest', 1),
            ('(val: string) => updateValue(field.key, val)', '(val: unknown) => updateValue(field.key, val)', 8),
            ('  isFetching: isNetworkStatusFetching,', '  asyncStatus: networkStatusAsyncStatus,', 1),
            ('const {\n  data: nodeListData,', "const isNetworkStatusFetching = computed(() => networkStatusAsyncStatus.value === 'loading')\n\nconst {\n  data: nodeListData,", 1),
        ],
        'apps/web/src/pages/bots/components/use-memory-filter.ts': [
            ('interface UseMemoryFilterResult {', 'interface UseMemoryFilterResult<T extends AdaptersMemoryItem = AdaptersMemoryItem> {', 1),
            ('filtered: ComputedRef<AdaptersMemoryItem[]>', 'filtered: ComputedRef<T[]>', 1),
            ('export function useMemoryFilter(memories: ComputedRef<AdaptersMemoryItem[]>): UseMemoryFilterResult {', 'export function useMemoryFilter<T extends AdaptersMemoryItem>(memories: ComputedRef<T[]>): UseMemoryFilterResult<T> {', 1),
            ('const filtered = computed<AdaptersMemoryItem[]>', 'const filtered = computed<T[]>', 1),
        ],
        'apps/web/src/pages/bots/components/use-memory-groups.ts': [
            ('interface MemoryGroup {', 'interface MemoryGroup<T extends AdaptersMemoryItem = AdaptersMemoryItem> {', 1),
            ('  items: AdaptersMemoryItem[]', '  items: T[]', 1),
            ('interface UseMemoryGroupsResult {', 'interface UseMemoryGroupsResult<T extends AdaptersMemoryItem = AdaptersMemoryItem> {', 1),
            ('groups: ComputedRef<MemoryGroup[]>', 'groups: ComputedRef<MemoryGroup<T>[]>', 1),
            ('function useMemoryGroups(', 'function useMemoryGroups<T extends AdaptersMemoryItem>(', 1),
            ('memories: ComputedRef<AdaptersMemoryItem[]> | ComputedRef<Array<AdaptersMemoryItem & { id: string; memory: string }>>', 'memories: ComputedRef<T[]>', 1),
            ('): UseMemoryGroupsResult {', '): UseMemoryGroupsResult<T> {', 1),
            ('computed<MemoryGroup[]>', 'computed<MemoryGroup<T>[]>', 1),
            ('new Map<string, AdaptersMemoryItem[]>()', 'new Map<string, T[]>()', 1),
        ],
        'apps/web/src/pages/bots/components/bot-memory.vue': [
            ("import type { DateRange } from 'reka-ui'\n", '', 1),
            ('interface MemoryItem {', "type DateRange = NonNullable<InstanceType<typeof RangeCalendar>['$props']['modelValue']>\n\ninterface MemoryItem {", 1),
            ('computed(() => memories.value as unknown as AdaptersMemoryItem[])', 'computed(() => memories.value)', 1),
            ('computed(() => filtered.value as unknown as MemoryItem[])', 'computed(() => filtered.value)', 1),
            ("import { computed, ref, watch } from 'vue'", "import { computed, ref, shallowRef, watch } from 'vue'", 1),
            ('const compactDecayRange = ref<DateRange', 'const compactDecayRange = shallowRef<DateRange', 1),
        ],
        'apps/web/src/pages/bots/components/bot-access.vue': [
            ("    .filter(i => i.id && !present.has(i.id) && !aclExcludedChannelTypes.value.has(i.channel ?? ''))\n    .map(i => ({", "    .filter(i => i.id && !present.has(i.id) && !aclExcludedChannelTypes.value.has(i.channel ?? ''))\n    .map<MemberCandidateOption>(i => ({", 1),
            ('    .flatMap((g) => {\n      const key = groupRowKey', '    .flatMap<MemberCandidateOption>((g) => {\n      const key = groupRowKey', 1),
        ],
        'apps/web/src/pages/bots/components/bot-card.vue': [
            (':name="bot.display_name || bot.id"', ':name="bot.display_name || bot.id || \'\'"', 1),
        ],
        'apps/web/src/pages/bots/components/bot-hooks.vue': [
            ('  command?: string\n  tool?: string', "  command?: string\n  tool?: string\n  on_error?: 'ignore' | 'fail' | 'block'", 1),
            ('const eventItems = computed<HandlersHookEventInfo[]>(() => eventsData.value?.events ?? [])', "const eventItems = computed(() => (eventsData.value?.events ?? []).filter(\n  (event): event is HandlersHookEventInfo & { name: string } => typeof event.name === 'string' && event.name.length > 0,\n))", 1),
            ('  if (!items.length) return', '  const first = items[0]\n  if (!first) return', 1),
            ('testEvent.value = items[0].name', 'testEvent.value = first.name', 1),
            ('  event?: string\n  enabled?: boolean', '  event?: string\n  matcher?: string\n  priority?: number\n  enabled?: boolean', 1),
        ],
        'apps/web/src/pages/bots/components/channel-field.vue': [
            ("(v: string) => emit('update:modelValue', v)", "(v: unknown) => emit('update:modelValue', v)", 3),
        ],
        'apps/web/src/pages/bots/components/schedule-editor.vue': [
            ("{ required_error: t('bots.schedule.form.invalidMaxRunMinutes'), invalid_type_error: t('bots.schedule.form.invalidMaxRunMinutes') }", "{ error: t('bots.schedule.form.invalidMaxRunMinutes') }", 1),
        ],
        'apps/web/src/pages/bots/components/bot-overview.vue': [
            ('const [r, g, b, a] = colorCanvas.getImageData(0, 0, 1, 1).data', 'const [r = 0, g = 0, b = 0, a = 0] = colorCanvas.getImageData(0, 0, 1, 1).data', 1),
            ('const query = { ...route.query, tab }', 'const query: typeof route.query = { ...route.query, tab }', 1),
        ],
        'apps/web/src/pages/bots/components/channel-settings-panel.vue': [
            ('v-for="key in requiredFieldsKeys"', 'v-for="[key, field] in requiredFields"', 1),
            ('v-for="key in optionalFieldsKeys"', 'v-for="[key, field] in optionalFields"', 1),
            (':field="orderedFields[key]"', ':field="field"', 2),
            ('const requiredFieldsKeys = computed(() => Object.keys(orderedFields.value).filter(k => orderedFields.value[k].required))', 'const requiredFields = computed(() => Object.entries(orderedFields.value).filter(([, field]) => field.required))', 1),
            ('const optionalFieldsKeys = computed(() => Object.keys(orderedFields.value).filter(k => !orderedFields.value[k].required))', 'const optionalFields = computed(() => Object.entries(orderedFields.value).filter(([, field]) => !field.required))', 1),
            ('for (const key of requiredFieldsKeys.value)', 'for (const [key, field] of requiredFields.value)', 1),
            ('orderedFields.value[key].title || key', 'field.title || key', 1),
            ('requiredFieldsKeys', 'requiredFields', 1),
            ('optionalFieldsKeys', 'optionalFields', 2),
        ],
        'apps/web/src/pages/bots/index.vue': [
            ('const { data: botData, status } = useQuery(getBotsQuery())', 'const { data: botData, isLoading } = useQuery(getBotsQuery())', 1),
            ("const isLoading = computed(() => status.value === 'loading')\n", '', 1),
            ('  return Array.from({ length: n }, (_, i) => {', '  const first = real[0]\n  if (!first) return []\n  return Array.from({ length: n }, (_, i) => {', 1),
            ('const base = real[i % real.length]', 'const base = real[i % real.length] ?? first', 1),
        ],
        'apps/web/src/lib/keyboard-bindings.test.ts': [],
        'apps/web/src/components/sidebar/recents.vue': [
            ('<script setup lang="ts">', '<script setup lang="ts">\nimport type { ComponentPublicInstance } from \'vue\'', 1),
            ('ref="loadMoreSentinel"', ':ref="setLoadMoreSentinel"', 1),
            ('}\n</script>', '}\nfunction setLoadMoreSentinel(element: Element | ComponentPublicInstance | null) {\n  loadMoreSentinel.value = element instanceof HTMLElement ? element : null\n}\n</script>', 1),
        ],
        'apps/web/src/pages/home/components/chat-workspace.test.ts': [
            ("    // The repository's test tsconfig does not load Vue's ambient module shim.\n    // @ts-expect-error Vitest's Vue plugin resolves this SFC at runtime.\n", '', 1),
        ],
        'apps/web/src/pages/home/components/dockview/panel-breadcrumb.vue': [
            ("return segs.length ? segs[segs.length - 1] : ''", "return segs[segs.length - 1] ?? ''", 1),
        ],
        'apps/web/src/pages/home/composables/useQueueTurnAnchors.ts': [
            ('message.runtimeSteer ?', 'isRuntimeSteerUserTurn(message) ?', 1),
        ],
        'apps/web/src/pages/login/components/dot-matrix-bg.vue': [
            ('  if (!frames.length) return 0', '  const first = frames[0]\n  if (!first) return 0', 1),
            ('frames[i + 1].t <= frame', '(frames[i + 1]?.t ?? Infinity) <= frame', 1),
            ('const a = frames[i]', 'const a = frames[i] ?? first', 1),
            ('const b = frames[Math.min(i + 1, frames.length - 1)]', 'const b = frames[Math.min(i + 1, frames.length - 1)] ?? a', 1),
            ('fieldData[offset + 3] / 255', '(fieldData[offset + 3] ?? 0) / 255', 1),
        ],
        'apps/web/src/pages/onboarding/index.vue': [
            ('stepComponents[currentStep].', 'stepComponents[currentStep]?.', 2),
        ],
        'apps/web/src/utils/timezones.ts': [
            ('for (; i < end; i++) getUtcOffsetLabel(timezones[i])', 'for (; i < end; i++) {\n      const timezone = timezones[i]\n      if (timezone !== undefined) getUtcOffsetLabel(timezone)\n    }', 1),
        ],
        'apps/web/src/utils/webhook-public-base.ts': [
            ("host.split(':', 1)[0].trim()", "(host.split(':', 1)[0] ?? '').trim()", 1),
            ("  return raw.trim()\n    .replace(/^[a-z][a-z0-9+.-]*:\\/\\//i, '')\n    .split(/[/?#]/, 1)[0]", "  return raw.trim()\n    .replace(/^[a-z][a-z0-9+.-]*:\\/\\//i, '')\n    .split(/[/?#]/, 1)[0] ?? ''", 1),
            ('  const [a, b, c] = parts', '  const [a, b, c] = parts\n  if (a === undefined || b === undefined || c === undefined) return false', 1),
        ],
        'apps/web/src/store/chat/transcript.test.ts': [
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-assistant-1',\n      status: 'waiting_decision',\n      operation: null,\n      turns: [pendingProjection],\n      streaming: true,\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-assistant-1',\n      status: 'waiting_decision',\n      operation: null,\n      turns: [pendingProjection],\n      streaming: true,\n    })", 2),
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'running',\n      operation: null,\n      streaming: true,\n      turns: [\n        rawAssistant('runtime-assistant', []),\n      ],\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'running',\n      operation: null,\n      streaming: true,\n      turns: [\n        rawAssistant('runtime-assistant', []),\n      ],\n    })", 1),
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'completed',\n      operation: null,\n      streaming: false,\n      turns: [\n        rawUser('runtime-user', 'hello first'),\n        rawAssistant('runtime-assistant', []),\n      ],\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'completed',\n      operation: null,\n      streaming: false,\n      turns: [\n        rawUser('runtime-user', 'hello first'),\n        rawAssistant('runtime-assistant', []),\n      ],\n    })", 1),
            ("transcript.applyRuntimeTranscript({\n        runId: 'run-1',\n        turnId: 'turn-1',\n        status: 'running',\n        operation: null,\n        streaming: true,\n        turns: [\n          { ...rawUser('runtime-user'), turn_id: 'turn-1' },\n          {\n            ...rawAssistant('runtime-assistant', [{ id: 0, type: 'text', content: 'streaming' }]),\n            turn_id: 'turn-1',\n          },\n        ],\n      })", "transcript.applyRuntimeTranscript({\n        invocationId: '',\n        runId: 'run-1',\n        turnId: 'turn-1',\n        status: 'running',\n        operation: null,\n        streaming: true,\n        turns: [\n          { ...rawUser('runtime-user'), turn_id: 'turn-1' },\n          {\n            ...rawAssistant('runtime-assistant', [{ id: 0, type: 'text', content: 'streaming' }]),\n            turn_id: 'turn-1',\n          },\n        ],\n      })", 1),
            ("transcript.applyRuntimeTranscript({\n        runId: 'run-edit',\n        turnId: 'turn-edit',\n        status: 'running',\n        operation: {\n          kind: 'edit',\n          replace_from_message_id: 'server-user',\n        },\n        streaming: true,\n        turns: [\n          rawUser('runtime-user', 'edited'),\n          rawAssistant('runtime-assistant'),\n        ],\n      })", "transcript.applyRuntimeTranscript({\n        invocationId: '',\n        runId: 'run-edit',\n        turnId: 'turn-edit',\n        status: 'running',\n        operation: {\n          kind: 'edit',\n          replace_from_message_id: 'server-user',\n        },\n        streaming: true,\n        turns: [\n          rawUser('runtime-user', 'edited'),\n          rawAssistant('runtime-assistant'),\n        ],\n      })", 2),
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'admitting',\n      operation: null,\n      streaming: true,\n      turns: [rawAssistant('runtime-assistant')],\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'admitting',\n      operation: null,\n      streaming: true,\n      turns: [rawAssistant('runtime-assistant')],\n    })", 1),
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'running',\n      operation: {\n        kind: 'retry',\n        replace_from_message_id: 'assistant-old',\n      },\n      streaming: true,\n      turns: [rawAssistant('runtime-assistant', [\n        { id: 0, type: 'text', content: 'new answer' },\n      ])],\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'running',\n      operation: {\n        kind: 'retry',\n        replace_from_message_id: 'assistant-old',\n      },\n      streaming: true,\n      turns: [rawAssistant('runtime-assistant', [\n        { id: 0, type: 'text', content: 'new answer' },\n      ])],\n    })", 1),
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'running',\n      operation: {\n        kind: 'edit',\n        replace_from_message_id: 'missing-user',\n      },\n      streaming: true,\n      turns: [\n        rawUser('runtime-user', 'edited'),\n        rawAssistant('runtime-assistant'),\n      ],\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-1',\n      status: 'running',\n      operation: {\n        kind: 'edit',\n        replace_from_message_id: 'missing-user',\n      },\n      streaming: true,\n      turns: [\n        rawUser('runtime-user', 'edited'),\n        rawAssistant('runtime-assistant'),\n      ],\n    })", 1),
            ("transcript.applyRuntimeTranscript({\n      runId: 'run-1',\n      turnId: 'turn-user-1',\n      status: 'completed',\n      operation: null,\n      turns: [{\n        id: 'runtime:turn-user-1:user',\n        turn_id: 'turn-user-1',\n        role: 'user',\n        text: 'hello',\n        timestamp: '2026-01-01T00:00:00.000Z',\n      }],\n      streaming: false,\n    })", "transcript.applyRuntimeTranscript({\n      invocationId: '',\n      runId: 'run-1',\n      turnId: 'turn-user-1',\n      status: 'completed',\n      operation: null,\n      turns: [{\n        id: 'runtime:turn-user-1:user',\n        turn_id: 'turn-user-1',\n        role: 'user',\n        text: 'hello',\n        timestamp: '2026-01-01T00:00:00.000Z',\n      }],\n      streaming: false,\n    })", 1),
        ],
        'apps/web/src/pages/bots/components/settings-interaction-card.test.ts': [("import { createApp, nextTick, reactive } from 'vue'", "import { createApp, nextTick, reactive } from 'vue'\nimport type { BotagentsBotAgent } from '@memohai/sdk'", 1), ('const botAgents = [', 'const botAgents: [BotagentsBotAgent, BotagentsBotAgent, BotagentsBotAgent] = [', 1)],
        'apps/web/src/pages/bots/new-progress.test.ts': [
            ('function translate(key: string, params?: Record<string, string>) {\n  return params?.name ? `${key}:${params.name}` : key\n}', "function translate(key: unknown, params?: unknown) {\n  const name = params && typeof params === 'object' && 'name' in params ? params.name : undefined\n  return name ? `${String(key)}:${String(name)}` : String(key)\n}", 1),
        ],
        'apps/web/src/pages/providers/model-setting.test.ts': [
            ("import type { Slots } from 'vue'", "import type { Slots } from 'vue'\nimport type { ProvidersGetResponse } from '@memohai/sdk'", 1),
            ('const provider = ref({', 'const provider = ref<ProvidersGetResponse | undefined>({', 1),
            ('provider.value.enable', 'provider.value?.enable', 1),
            ('    SettingsShell: Passthrough,', '    SettingsShell: Passthrough,\n    SurfaceCard: Passthrough,', 1),
        ],
        'apps/web/src/store/workspace-tabs.test.ts': [
            ('const primaryGroupId = parentPanel.group.id', "if (!parentPanel.group) throw new Error('Parent fixture has no group')\n    const primaryGroupId = parentPanel.group.id", 1),
            ('expect(first.group.id).not.toBe(primaryGroupId)', "if (!first.group) throw new Error('First subagent fixture has no group')\n    expect(first.group.id).not.toBe(primaryGroupId)", 1),
            ('expect(second.group.id).toBe(first.group.id)', "if (!second.group) throw new Error('Second subagent fixture has no group')\n    expect(second.group.id).toBe(first.group.id)", 1),
        ],
        'apps/web/src/pages/dev/components/lib/variant-specs.ts': [
            ('variants: string[]', 'variants: readonly string[]', 1),
            ('sizes?: string[]', 'sizes?: readonly string[]', 1),
            ('} satisfies Record<string, VariantSpec>', '} as const satisfies Record<string, VariantSpec>', 1),
        ],
        'apps/web/src/pages/dev/components/components/VariantMatrix.vue': [
            ('<script setup lang="ts">', '<script setup lang="ts" generic="TVariant extends string, TSize extends string = string">', 1),
            ('variants: string[]', 'variants: readonly TVariant[]', 1),
            ('sizes?: string[]', 'sizes?: readonly TSize[]', 1),
            ('excludeSizes?: Record<string, string[]>', 'excludeSizes?: Record<string, readonly string[]>', 1),
            ('function cellsFor(variant: string): (string | undefined)[]', 'function cellsFor(variant: TVariant): (TSize | undefined)[]', 1),
        ],
        'apps/web/src/pages/dev/components/sections/SectionType.vue': [
            ("import { onMounted, ref } from 'vue'", "import { onMounted, ref, type ComponentPublicInstance } from 'vue'", 1),
            ('return (el: Element | null) => {\n    sampleRefs.value[name] = el as HTMLElement | null', 'return (el: Element | ComponentPublicInstance | null) => {\n    sampleRefs.value[name] = el instanceof HTMLElement ? el : null', 1),
        ],
        'apps/web/src/pages/dev/components/sections/SectionInputsForms.vue': [
            ('v-for="(slot, i) in slots.slice(', 'v-for="(_, i) in slots.slice(', 2),
        ],
        'apps/web/src/pages/bots/detail.vue': [
            ('                        <span\n                          v-if="bot.type"\n                          class="text-muted-foreground/60"\n                        >· {{ botTypeLabel }}</span>\n', '', 1),
            (", 'bot-type': bot.value?.type", '', 2),
            ("const botTypeLabel = computed(() => {\n  const type = bot.value?.type\n  if (type === 'personal' || type === 'public') return t('bots.types.' + type)\n  return type ?? ''\n})\n\n", '', 1),
        ],
        'apps/web/src/pages/bots/components/memory-graph/types.ts': [
            ('ChartNodeData extends GraphNode', "ChartNodeData extends Omit<GraphNode, 'label'>", 1),
        ],
        'apps/web/src/pages/bots/components/memory-graph/useMemoryGraph.ts': [
            ("import { computed, onBeforeUnmount, ref, toRef, watch, type Ref } from 'vue'", "import { computed, onBeforeUnmount, ref, toRef, useTemplateRef, watch, type Ref } from 'vue'", 1),
            ('const chartRef = ref<{ chart?: EChartsType } | null>(null)', "const chartRef = useTemplateRef<{ chart?: EChartsType }>('chartRef')", 1),
        ],
        'apps/web/src/pages/bots/components/memory-graph.vue': [
            ('  chartRef,\n', '', 1),
        ],
        'packages/ui/src/components/label-swap/LabelSwap.vue': [
            ("import type { ComponentPublicInstance } from 'vue'\n", '', 1),
            ('el: Element | ComponentPublicInstance | null', 'el: unknown', 1),
        ],
        'apps/web/src/pages/bots/components/memory-graph/build-chart-option.ts': [
            ("export function readSeriesZoom(chart: EChartsType): number {\n  try {\n    const seriesModel = chart.getModel().getSeries().find(series => (\n      series.id === MEMORY_GRAPH_SERIES_ID || series.subType === 'graph'\n    )) ?? chart.getModel().getSeriesByIndex(0)\n    const coord = seriesModel?.coordinateSystem as { getZoom?: () => number } | undefined\n    const fromCoord = coord?.getZoom?.()\n    if (typeof fromCoord === 'number' && Number.isFinite(fromCoord) && fromCoord > 0) return fromCoord\n    const fromOption = seriesModel?.get('zoom')\n    if (typeof fromOption === 'number' && Number.isFinite(fromOption) && fromOption > 0) return fromOption\n  }\n  catch {\n    // fall through\n  }\n  return 1\n}\n\nexport function readSeriesCenter(chart: EChartsType): [number, number] | null {\n  try {\n    const seriesModel = chart.getModel().getSeries().find(series => (\n      series.id === MEMORY_GRAPH_SERIES_ID || series.subType === 'graph'\n    )) ?? chart.getModel().getSeriesByIndex(0)\n    const center = seriesModel?.get('center')\n    if (Array.isArray(center) && center.length >= 2) {\n      const x = Number(center[0])\n      const y = Number(center[1])\n      if (Number.isFinite(x) && Number.isFinite(y)) return [x, y]\n    }\n  }\n  catch {\n    // fall through\n  }\n  return null\n}\n\nexport function nodeDataIndex(chart: EChartsType, nodeId: string): number | null {\n  try {\n    const seriesModel = chart.getModel().getSeries().find(series => (\n      series.id === MEMORY_GRAPH_SERIES_ID || series.subType === 'graph'\n    )) ?? chart.getModel().getSeriesByIndex(0)\n    const data = seriesModel?.getData()\n    if (!data) return null\n    let found: number | null = null\n    data.each((idx: number) => {\n      if (found != null) return\n      const raw = data.getRawDataItem(idx) as { id?: string, name?: string }\n      if (String(raw?.id ?? raw?.name ?? '') === nodeId) found = idx\n    })\n    return found\n  }\n  catch {\n    return null\n  }\n}\n\n", "function isOptionRecord(value: unknown): value is Record<string, unknown> {\n  return typeof value === 'object' && value !== null && !Array.isArray(value)\n}\n\nfunction readGraphSeries(chart: Pick<EChartsType, 'getOption'>): Record<string, unknown> | undefined {\n  const optionSeries = chart.getOption().series\n  const series: unknown[] = Array.isArray(optionSeries) ? optionSeries : optionSeries ? [optionSeries] : []\n  return series.find((item): item is Record<string, unknown> => (\n    isOptionRecord(item) && (item.id === MEMORY_GRAPH_SERIES_ID || item.type === 'graph')\n  )) ?? (isOptionRecord(series[0]) ? series[0] : undefined)\n}\n\nexport function readSeriesZoom(chart: Pick<EChartsType, 'getOption'>): number {\n  try {\n    const zoom = readGraphSeries(chart)?.zoom\n    if (typeof zoom === 'number' && Number.isFinite(zoom) && zoom > 0) return zoom\n  }\n  catch {\n    // fall through\n  }\n  return 1\n}\n\nexport function readSeriesCenter(chart: Pick<EChartsType, 'getOption'>): [number, number] | null {\n  try {\n    const center = readGraphSeries(chart)?.center\n    if (Array.isArray(center) && center.length >= 2) {\n      const x = Number(center[0])\n      const y = Number(center[1])\n      if (Number.isFinite(x) && Number.isFinite(y)) return [x, y]\n    }\n  }\n  catch {\n    // fall through\n  }\n  return null\n}\n\nexport function nodeDataIndex(chart: Pick<EChartsType, 'getOption'>, nodeId: string): number | null {\n  try {\n    const data = readGraphSeries(chart)?.data\n    if (!Array.isArray(data)) return null\n    const index = data.findIndex(raw => (\n      isOptionRecord(raw) && String(raw.id ?? raw.name ?? '') === nodeId\n    ))\n    return index < 0 ? null : index\n  }\n  catch {\n    return null\n  }\n}\n\n", 1),
            ("import type { EChartsType } from 'echarts/core'", "import { registerPostUpdate, type EChartsType } from 'echarts/core'", 1),
            ('export interface MemoryGraphOptionInput {', "// The public post-update callback supplies ECharts' current layout model.\n// Keep that SDK-owned model keyed weakly by its public renderer identity;\n// no private chart method or layout/render mutation is needed.\ntype ChartModel = Parameters<Parameters<typeof registerPostUpdate>[0]>[0]\nconst chartModels = new WeakMap<ReturnType<EChartsType['getZr']>, ChartModel>()\nregisterPostUpdate((model, api) => {\n  chartModels.set(api.getZr(), model)\n})\n\nexport function readChartModel(chart: EChartsType) {\n  return chart.isDisposed() ? undefined : chartModels.get(chart.getZr())\n}\n\nexport interface MemoryGraphOptionInput {", 1),
            ('chart.getModel().getSeries()', 'readChartModel(chart)?.getSeries()', 1),
            ('chart.getModel().getSeriesByIndex(params.seriesIndex ?? 0)', 'readChartModel(chart)?.getSeriesByIndex(params.seriesIndex ?? 0)', 1),
        ],
        'packages/sdk/src/types.gen.ts': [
            ("export type GetBotsByBotIdMcpData = {\n    body?: never;\n    path?: never;\n    query?: never;\n    url: '/bots/{bot_id}/mcp';\n}", "export type GetBotsByBotIdMcpData = {\n    body?: never;\n    path: { bot_id: string };\n    query?: never;\n    url: '/bots/{bot_id}/mcp';\n}", 1),
            ("export type PostBotsByBotIdMcpData = {\n    /**\n     * MCP payload\n     */\n    body: McpUpsertRequest;\n    path?: never;\n    query?: never;\n    url: '/bots/{bot_id}/mcp';\n}", "export type PostBotsByBotIdMcpData = {\n    /**\n     * MCP payload\n     */\n    body: McpUpsertRequest;\n    path: { bot_id: string };\n    query?: never;\n    url: '/bots/{bot_id}/mcp';\n}", 1),
            ("export type PutBotsByBotIdMcpOpsImportData = {\n    /**\n     * mcpServers dict\n     */\n    body: McpImportRequest;\n    path?: never;\n    query?: never;\n    url: '/bots/{bot_id}/mcp-ops/import';\n}", "export type PutBotsByBotIdMcpOpsImportData = {\n    /**\n     * mcpServers dict\n     */\n    body: McpImportRequest;\n    path: { bot_id: string };\n    query?: never;\n    url: '/bots/{bot_id}/mcp-ops/import';\n}", 1),
            ("export type PostBotsByBotIdMcpByIdOauthExchangeData = {\n    /**\n     * Authorization code and state\n     */\n    body: HandlersOauthExchangeRequest;\n    path?: never;\n    query?: never;\n    url: '/bots/{bot_id}/mcp/{id}/oauth/exchange';\n}", "export type PostBotsByBotIdMcpByIdOauthExchangeData = {\n    /**\n     * Authorization code and state\n     */\n    body: HandlersOauthExchangeRequest;\n    path: { bot_id: string; id: string };\n    query?: never;\n    url: '/bots/{bot_id}/mcp/{id}/oauth/exchange';\n}", 1),
        ],
        'apps/web/src/pages/bots/components/bot-mcp.vue': [
            ('getBotsByBotIdMcp({ path: { bot_id: props.botId } as unknown as { bot_id: string }', 'getBotsByBotIdMcp({ path: { bot_id: props.botId }', 1),
            ('putBotsByBotIdMcpOpsImport({ path: { bot_id: props.botId } as unknown as { bot_id: string }', 'putBotsByBotIdMcpOpsImport({ path: { bot_id: props.botId }', 1),
        ],
        'apps/web/src/pages/bots/components/mcp-server-detail.vue': [
            ('postBotsByBotIdMcp({ path: { bot_id: props.botId } as unknown as { bot_id: string }', 'postBotsByBotIdMcp({ path: { bot_id: props.botId }', 1),
        ],
        'packages/sdk/src/@pinia/colada.gen.ts': [
            ('export const getBotsByBotIdMcpQuery = defineQueryOptions<Options<GetBotsByBotIdMcpData>, GetBotsByBotIdMcpResponse, GetBotsByBotIdMcpError>((options?: Options<GetBotsByBotIdMcpData>)', 'export const getBotsByBotIdMcpQuery = defineQueryOptions<Options<GetBotsByBotIdMcpData>, GetBotsByBotIdMcpResponse, GetBotsByBotIdMcpError>((options: Options<GetBotsByBotIdMcpData>)', 1),
        ],
        'apps/web/src/pages/bots/components/memory-graph/useMemoryGraphLayout.ts': [
            ("import { readSeriesCenter, readSeriesZoom } from './build-chart-option'", "import { readChartModel, readSeriesCenter, readSeriesZoom } from './build-chart-option'", 1),
            ('chart.getModel().getSeriesByIndex(0)', 'readChartModel(chart)?.getSeriesByIndex(0)', 1),
        ],
    }
    changes['apps/web/src/pages/home/composables/useComposerAttachments.ts'].extend([
        ("import { computed,", "import { selectedAttachmentFile } from '@/lib/harmony-files'\nimport { computed,", 1),
        ('const uploaded = uploadedAttachmentFile(attachment)',
         'const uploaded = selectedAttachmentFile(attachment) ?? uploadedAttachmentFile(attachment)', 1),
    ])
    # Current upstream type errors are adapted only in this private client context.
    changes.update({'apps/web/src/pages/home/components/composer-dock.vue': [(' && !props.compacting', '', 1)], 'apps/web/src/pages/web-search/components/searxng-settings.vue': [('  localConfig.headers[index] = { ...localConfig.headers[index], [field]: value }', '  const header = localConfig.headers[index]\n  if (header) localConfig.headers[index] = { ...header, [field]: value }', 1)], 'apps/web/src/utils/markdown-dollar.ts': [("import type MarkdownIt from 'markdown-it'", "import type { NodeRendererProps } from 'markstream-vue'\ntype MarkdownIt = Parameters<NonNullable<NodeRendererProps['customMarkdownIt']>>[0]\ntype MarkdownToken = ReturnType<MarkdownIt['parse']>[number]\ntype MarkdownRule = Parameters<MarkdownIt['core']['ruler']['before']>[2]", 1), ('MarkdownIt.RuleCore', 'MarkdownRule', 2), ('MarkdownIt.Token', 'MarkdownToken', 4), ('const ch = content[i]', 'const ch = content[i]!', 1)]})
    changes['apps/web/src/pages/home/components/chat-pane.vue'].extend([('let preparingView: ReturnType<typeof chatStore.chatView> | undefined', 'let preparingView: ReturnType<typeof chatStore.chatView> | undefined\nlet attachmentPreparationCancelled = false', 1), ('function cancelAttachmentUpload() { preparingView?.attachmentPreparation.value?.abort() }', 'function cancelAttachmentUpload() {\n  const controller = preparingView?.attachmentPreparation.value\n  if (controller) { attachmentPreparationCancelled = true; controller.abort() }\n}', 1), ('  preparingView = paneView.value', '  attachmentPreparationCancelled = false\n  preparingView = paneView.value', 1), ('  if (!holdsInput) clearSentInput()\n', '', 1), ('      if (holdsInput) {\n        clearSentInput(onSentPane)', '      clearSentInput(onSentPane)\n      if (holdsInput) {', 1), ('  if (restore) {\n    if (!matchesChatPaneSendContext(', '  if (restore) {\n    // Selection/lifecycle cancellation leaves the live composer untouched.\n    if (attachmentPreparationCancelled) return\n    if (!matchesChatPaneSendContext(', 1)])
    changes['apps/web/src/utils/markdown-dollar.ts'].append((
        "type MarkdownToken = ReturnType<MarkdownIt['parse']>[number]",
        "type MarkdownToken = ReturnType<MarkdownIt['parse']>[number] & { raw?: string }", 1))
    changes['apps/web/src/lib/api-client.ts'] = [
        ('function createApiFetch(', 'export function createApiFetch(', 1),
    ]
    patches = {}
    context = base / 'official-context.tar'
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
            target.addfile(entry, io.BytesIO(data) if data is not None else None)
        assert patches.keys() == changes.keys() | {'apps/web/package.json', 'pnpm-lock.yaml'}, 'Upstream host seam missing'
        host_sources = {name: ROOT / 'mobile/memoh' / name for name in (
            'harmony.ts', 'harmony-files.ts', 'harmony-voice.ts', 'harmony-voice-geometry.ts',
            'HarmonyVoiceInput.vue', 'HarmonyVoiceFan.vue', 'HarmonyVoicePlayer.vue', 'voiceFan.svg', 'voiceFan.css')}
        host_sources['harmony-theme.json'] = ROOT / 'contracts/ui_theme.json'
        for name, path in host_sources.items():
            data = path.read_bytes()
            entry = tarfile.TarInfo('apps/web/src/lib/' + name)
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
    licenses = dist / 'licenses'
    licenses.mkdir(exist_ok=True)
    (licenses / 'memoh.txt').write_bytes((checkout / 'LICENSE').read_bytes())
    for name, source in [('lucide-voice.txt', 'lucide.txt'), ('wavesurfer.txt', 'wavesurfer.txt')]:
        (licenses / name).write_bytes((ROOT / 'mobile/memoh/licenses' / source).read_bytes())
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
    context, patches = context_archive(lock, base)
    config = read_config(require_api=args.platform == 'harmony')
    api_url = config['apiOrigin'] if args.platform == 'harmony' else '/api'
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
        license_path = checked(dist / 'licenses/memoh.txt')
        license_path.parent.mkdir(exist_ok=True)
        license_path.write_bytes(source.extractfile(lock['archivePrefix'] + 'LICENSE').read())
    (dist / 'licenses/lucide-voice.txt').write_bytes((ROOT / 'mobile/memoh/licenses/lucide.txt').read_bytes())
    (dist / 'licenses/wavesurfer.txt').write_bytes((ROOT / 'mobile/memoh/licenses/wavesurfer.txt').read_bytes())
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
