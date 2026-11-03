'use strict'
// TF-17 quote-reply composition gate: asserts the composed client (the real
// official-context.tar after every overlay) carries the two entry points, the
// composer chip, the reply_to wire field, and no quote-specific visible copy
// beyond the approved menu label. Reads the tar directly; no build required.
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert')

const root = path.resolve(__dirname, '../..')
const context = path.join(root, 'mobile/.build/memoh-web/official-context.tar')
const overlayPath = path.join(root, 'mobile/memoh/shared-quote-reply-changes.json')

function readTar(file) {
  const fd = fs.openSync(file, 'r')
  try {
    const out = {}
    const header = Buffer.alloc(512)
    while (fs.readSync(fd, header, 0, 512, null) === 512) {
      if (!header.subarray(0).some(byte => byte !== 0)) break
      const typeflag = String.fromCharCode(header[156])
      const size = parseInt(header.toString('utf8', 124, 136).replace(/\0.*$/, '').trim() || '0', 8)
      let name = header.toString('utf8', 0, 100).replace(/\0.*$/, '')
      const prefix = header.toString('utf8', 345, 500).replace(/\0.*$/, '')
      if (prefix) name = prefix + '/' + name
      const data = Buffer.alloc(size)
      if (size > 0) fs.readSync(fd, data, 0, size, null)
      const padded = (512 - (size % 512)) % 512
      if (padded > 0) fs.readSync(fd, Buffer.alloc(padded), 0, padded, null)
      // Skip pax extended headers; member data still follows the real entry.
      if (typeflag === 'x' || typeflag === 'g') continue
      if (typeflag === '0' || typeflag === '\0') out[name] = data.toString('utf8')
    }
    return out
  } finally {
    fs.closeSync(fd)
  }
}

const files = readTar(context)
const pane = files['apps/web/src/pages/home/components/chat-pane.vue']
const actions = files['apps/web/src/pages/home/components/message-actions.vue']
const item = files['apps/web/src/pages/home/components/message-item.vue']
const send = files['apps/web/src/store/chat/send.ts']
const wsTypes = files['apps/web/src/composables/api/useChat.ws.ts']
const zh = JSON.parse(files['apps/web/src/i18n/locales/zh.json'])
const en = JSON.parse(files['apps/web/src/i18n/locales/en.json'])
const ja = JSON.parse(files['apps/web/src/i18n/locales/ja.json'])
assert.ok(pane && actions && item && send && wsTypes, 'composed sources present')

// 1. Long-press menu: read-aloud stays the first item; quote is second.
const menuStart = pane.indexOf('<ContextMenuContent v-if="readAloud.menuAvailable(msg) || quoteMenuAvailable(msg)">')
assert.ok(menuStart > 0, 'menu opens for quotable rows too')
const menuEnd = pane.indexOf('</ContextMenuContent>', menuStart)
const menuBlock = pane.slice(menuStart, menuEnd)
const replayItem = menuBlock.indexOf(':disabled="!readAloud.canReplay(msg)"')
const quoteItem = menuBlock.indexOf("@select=\"startQuoteReply(msg)\"")
assert.ok(replayItem > 0 && quoteItem > replayItem, 'read-aloud item remains first, quote second')
assert.ok(menuBlock.includes("$t('chat.quoteReply.menu')"), 'quote menu label')
assert.ok(pane.includes(':disabled="!readAloud.menuAvailable(msg) && !quoteMenuAvailable(msg)"'), 'trigger disabled relaxed')

// 2. Action row: one icon-only quote button at the end of the row.
const quoteButton = actions.indexOf("<Quote class=\"size-4\" />")
assert.ok(quoteButton > 0, 'quote icon present in the action row')
const buttonStart = actions.lastIndexOf('<Button', quoteButton)
const buttonEnd = actions.indexOf('</Button>', quoteButton)
const buttonBlock = actions.slice(buttonStart, buttonEnd)
assert.ok(buttonBlock.includes("aria-label=\"t('chat.quoteReply.action')\""), 'icon carries only an aria label')
assert.ok(!buttonBlock.includes('TooltipContent'), 'no tooltip copy on the quote icon')
assert.ok(!/\{\{\s*t\(/.test(buttonBlock), 'no visible interpolated copy inside the quote button')
const providerEnd = actions.indexOf('</TooltipProvider>')
assert.ok(buttonEnd < providerEnd, 'quote button sits inside the row')
const lastIconBefore = Math.max(
  actions.lastIndexOf('<PencilLine class="size-4" />'),
  actions.lastIndexOf('<RotateCcw'),
  actions.lastIndexOf('<Volume2 class="size-4" />'),
  actions.lastIndexOf('</template>', buttonStart),
)
assert.ok(quoteButton > lastIconBefore, 'quote icon is the last control of the row')
assert.ok(item.includes(':on-quote="onQuote"'), 'message card forwards the quote handler')

// 3. Composer chip: sender + truncated preview + icon-only cancel.
assert.ok(pane.includes('v-if="quoteReplyTarget"'), 'chip renders only with a pending quote')
assert.ok(pane.includes('{{ quoteReplyTarget.sender }}') && pane.includes('{{ quoteReplyTarget.preview }}'), 'chip shows sender and preview')
assert.ok(pane.includes('@click="cancelQuoteReply"'), 'cancel control')
const cancelStart = pane.lastIndexOf('<Button', pane.indexOf('@click="cancelQuoteReply"'))
const cancelBlock = pane.slice(cancelStart, pane.indexOf('</Button>', cancelStart))
assert.ok(cancelBlock.includes("aria-label=\"$t('chat.quoteReply.cancel')\""), 'cancel is icon-only with an aria label')
assert.ok(!/\{\{\s*\$t\(/.test(cancelBlock), 'no visible copy in the cancel control')

// 4. Send plumbing: option -> payload, cleared on send and on session switch.
assert.ok(send.includes('reply_to: options.replyTo?.trim() || undefined,'), 'WS payload carries reply_to')
const payloadAt = send.indexOf('reply_to: options.replyTo?.trim() || undefined,')
const sessionAt = send.indexOf('session_id: targetSessionId || undefined,')
assert.ok(payloadAt > sessionAt && payloadAt - sessionAt < 200, 'reply_to rides the message payload')
assert.ok(send.indexOf('reply_to', payloadAt + 10) === -1, 'retry and edit payloads stay untouched')
assert.ok(pane.includes('replyTo: sentReplyTo,'), 'composer passes the quote to the send options')
assert.ok(pane.includes('if (onSentPane) quoteReplyTarget.value = null'), 'quote clears when its turn is appended')
assert.ok(pane.includes('watch(() => [paneTarget.value.botId, paneTarget.value.sessionId], () => {\n  quoteReplyTarget.value = null\n})'), 'quote clears on conversation switch')
assert.ok(wsTypes.includes('reply_to?: string'), 'WS client type carries reply_to')

// 5. Locales: the approved menu label and nothing else visible.
assert.strictEqual(zh.chat.quoteReply.menu, '引用')
assert.strictEqual(en.chat.quoteReply.menu, 'Quote')
assert.strictEqual(ja.chat.quoteReply.menu, '引用')
for (const locale of [zh, en, ja]) {
  assert.deepStrictEqual(Object.keys(locale.chat.quoteReply).sort(), ['action', 'cancel', 'menu'])
}
const overlay = JSON.parse(fs.readFileSync(overlayPath, 'utf8'))
assert.deepStrictEqual(Object.keys(overlay.existingFiles).sort(), [
  'apps/web/src/composables/api/useChat.ws.ts',
  'apps/web/src/pages/home/components/chat-pane.vue',
  'apps/web/src/pages/home/components/message-actions.vue',
  'apps/web/src/pages/home/components/message-item.vue',
  'apps/web/src/store/chat/send.ts',
  'apps/web/src/store/chat/types.ts',
])
assert.ok(!overlay.addedFiles || Object.keys(overlay.addedFiles).length === 0, 'quote overlay edits in place, adds no sources')

console.log(JSON.stringify({
  composedTree: true,
  menuOrderReadAloudFirst: true,
  actionRowTrailingIconOnly: true,
  composerChipWithIconCancel: true,
  sendPayloadReplyTo: true,
  clearedOnSendAndSwitch: true,
  localesApprovedLabelOnly: true,
  passed: true,
}))
