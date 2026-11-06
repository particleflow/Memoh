import { rejectedControls } from './ui-rejected-controls.mjs'
import fs from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'
import { createHash } from 'node:crypto'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(path.join(root, 'apps/web/package.json'))
const { parse } = require('vue/compiler-sfc')
const baselinePath = path.join(root, 'docs/testing/unauthorized-ui-copy/reviewed-surfaces.json')
const locales = ['en', 'zh', 'ja'].map(language => JSON.parse(fs.readFileSync(path.join(root, `apps/web/src/i18n/locales/${language}.json`), 'utf8')))
const normalize = text => text.replace(/\s+/g, ' ').trim()
const translations = text => [...new Set([...text.matchAll(/(?:\$t|\bt)\(['"]([^'"]+)['"]/g)].map(match => match[1]))].map(key => ({ key, values: locales.map(locale => key.split('.').reduce((value, part) => value?.[part], locale) ?? null) }))
const localeDigests = locales.map(locale => createHash('sha256').update(JSON.stringify(locale)).digest('hex'))
const rows = []
function scan(file) {
  const source = fs.readFileSync(file, 'utf8')
  const descriptor = parse(source).descriptor
  const scriptTranslations = translations((descriptor.scriptSetup?.content ?? '') + (descriptor.script?.content ?? ''))
  if (scriptTranslations.length) {
    const signature = { file: path.relative(root, file), tag: 'script-i18n', translations: scriptTranslations }
    rows.push({ ...signature, fingerprint: createHash('sha256').update(JSON.stringify(signature)).digest('hex') })
  }
  const template = descriptor.template
  if (!template) return
  const rejected = rejectedControls(template.content, file)
  if (rejected.length) throw new Error(`${path.relative(root, file)} restores rejected controls: ${rejected.join(', ')}`)
  function walk(node, inaccessible = false) {
    if (node.type !== 1) { node.children?.forEach(child => walk(child, inaccessible)); return }
    const hidden = inaccessible || node.props.some(prop => prop.name === 'class' && /sr-only/.test(prop.value?.content ?? ''))
    const direct = node.children.filter(child => child.type === 2 || child.type === 5).map(child => child.loc.source).join('')
    const props = node.props.filter(prop => /^(description|subtitle|hint|enter-hint|help|note|caption)$/.test(prop.arg?.content ?? prop.name ?? '')).map(prop => normalize(prop.loc.source))
    const control = /^(Button|TextButton|ContextMenuItem|DropdownMenuItem)$/.test(node.tag)
    const supplemental = props.length || /Description|Hint|Subtitle/.test(node.tag) || direct.trim()
    if (!hidden && (control || supplemental)) {
      const text = normalize(control ? node.loc.source.replace(/<[^>]+>/g, ' ') : direct)
      const conditions = control ? node.props.filter(prop => ['if', 'else-if', 'else', 'on'].includes(prop.name)).map(prop => normalize(prop.loc.source)) : []
      const signature = { file: path.relative(root, file), tag: node.tag, text, props, conditions, translations: translations([text, ...props].join(' ')) }
      rows.push({ ...signature, fingerprint: createHash('sha256').update(JSON.stringify(signature)).digest('hex') })
    }
    node.children.forEach(child => walk(child, hidden))
  }
  walk(template.ast)
}
function visit(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name)
    if (entry.isDirectory()) { if (file !== path.join(root, 'apps/web/src/pages/dev')) visit(file) }
    else if (file.endsWith('.vue')) scan(file)
  }
}
visit(path.join(root, 'apps/web/src'))
if (process.argv.includes('--write-baseline')) {
  fs.writeFileSync(baselinePath, '{\n  \"policy\": ' + JSON.stringify('User requested no unapproved explanatory copy or additional controls. Preserve original body/data, necessary action confirmations, real state and accessibility; review changes against the actual user request.') + ',\n  \"localeDigests\": ' + JSON.stringify(localeDigests) + ',\n  \"surfaces\": [\n' + rows.map(row => '    ' + JSON.stringify(row)).join(',\n') + '\n  ]\n}\n')
  console.log(`Recorded ${rows.length} reviewed copy/control surfaces`)
} else {
  const baseline = JSON.parse(fs.readFileSync(baselinePath, 'utf8'))
  const approved = baseline.surfaces
  const localeChanged = JSON.stringify(baseline.localeDigests) !== JSON.stringify(localeDigests)
  const counts = new Map()
  for (const row of approved) counts.set(row.fingerprint, (counts.get(row.fingerprint) ?? 0) + 1)
  const unexpected = []
  for (const row of rows) {
    const count = counts.get(row.fingerprint) ?? 0
    if (count) counts.set(row.fingerprint, count - 1)
    else unexpected.push(`${row.file} <${row.tag}> ${row.text || (row.props ?? []).join(' ') || row.translations.map(item => item.key).join(', ')}`)
  }
  if (localeChanged || unexpected.length) {
    console.error('Unreviewed UI copy/control changes require an explicit user request and a reviewed baseline update:\n' + (localeChanged ? 'Locale content changed.\n' : '') + unexpected.join('\n'))
    process.exitCode = 1
  } else console.log(`UI copy policy OK (${rows.length} reviewed surfaces; no unreviewed additions)`)
}
