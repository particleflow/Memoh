import test from 'node:test'
import assert from 'node:assert/strict'
import { rejectedControls } from './ui-rejected-controls.mjs'

test('latest rejected controls cannot return under an old locale key or CSS hiding', () => {
  for (const key of ['chat.tasks.newest', 'chat.readAloud.stop', 'chat.readAloud.enable', 'chat.readAloud.resume', 'chat.contact.startNew', 'chat.actions.createFork', 'chat.newSession']) {
    assert.deepEqual(rejectedControls(`<Button class="hidden">{{ t('${key}') }}</Button>`), [key])
  }
})
test('icon-only replacement controls remain rejected', () => {
  assert.ok(rejectedControls('<TextButton @click="readAloud.stop()"><X /></TextButton>').length)
  assert.ok(rejectedControls('<DropdownMenuItem @select="store.openDraftChat()"><Plus /></DropdownMenuItem>').length)
  assert.ok(rejectedControls('<ChatForkDialog />').length)
})
test('original interruption, replay gestures, historical navigation and actual errors remain allowed', () => {
  assert.deepEqual(rejectedControls('<div @click.capture="readAloud.stop()"><MessageItem @replay-speech="readAloud.replay(msg)" /></div><textarea @input="readAloud.stop()" /><ForkSourceDivider @navigate="openExisting" /><CalloutBanner>{{ error }}</CalloutBanner>'), [])
})
