// Latest explicit user rejections take precedence over older source provenance.
const rejectedKeys = /chat\.(?:tasks\.newest|readAloud\.(?:stop|enable|resume)|contact\.startNew|actions\.createFork|slash\.newDescription|newSession)\b/g
export function rejectedControls(template) {
  const violations = [...template.matchAll(rejectedKeys)].map(match => match[0])
  if (/<ChatForkDialog\b/.test(template)) violations.push('ChatForkDialog')
  if (/<(?:Button|TextButton|ContextMenuItem|DropdownMenuItem)\b[^>]*@(?:click|select)[^>]*readAloud\.(?:stop|resume)\(/s.test(template)) violations.push('extra playback control')
  if (/<(?:Button|TextButton|SidebarNavButton|DropdownMenuItem)\b[^>]*@(?:click|select)[^>]*openDraftChat\(/s.test(template)) violations.push('new main-chat control')
  return [...new Set(violations)]
}
