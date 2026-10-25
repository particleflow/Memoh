"""Adapt upstream DOM fixtures to the existing Harmony verified-file boundary.

Run against a freshly extracted Harmony context, never the upstream checkout.
These mocks check component routing only; native integrity tests run separately.
Every replacement is exact-count guarded so source drift fails visibly.
"""
import sys
from pathlib import Path

root = Path(sys.argv[1])
components = root / 'apps/web/src/pages/home/components'
path = components / 'session-task-results.test.ts'
text = path.read_text()


def replace(old, new, count=1):
    global text
    assert text.count(old) == count, f'Fixture drift: {old[:80]!r}'
    text = text.replace(old, new)


replace("let app: ReturnType<typeof createApp>", """const phone = vi.hoisted(() => ({ original: vi.fn(), save: vi.fn() }))
vi.mock('@/lib/harmony-files', () => ({ originalBlob: phone.original, saveOriginal: phone.save }))
let app: ReturnType<typeof createApp>""")
replace('  vi.useFakeTimers()', """  vi.useFakeTimers()
  vi.clearAllMocks()
  phone.original.mockResolvedValue(new Blob(['synthetic image'], { type: 'image/png' }))
  phone.save.mockResolvedValue(undefined)
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL() { return 'blob:verified-phone-fixture' }
    static revokeObjectURL() {}
  })
  vi.stubGlobal('Image', class { src = ''; decode() { return Promise.resolve() } })""")
start = text.index("    const link = root.querySelector('a')!")
end = text.index("    root.querySelector('button[title=", start)
text = text[:start] + """    expect(root.querySelectorAll('a')).toHaveLength(0)
    const download = [...root.querySelectorAll('button')].find(button => button.textContent?.trim() === en.bots.files.download)!
    download.click()
    await nextTick()
    expect(phone.save).toHaveBeenCalledOnce()
    expect(phone.save).toHaveBeenCalledWith(expect.objectContaining({ bot_id: 'specialist', content_hash: hash, name: 'output', size: 42 }))
    phone.save.mockClear()
""" + text[end:]
replace('expect(download).not.toHaveBeenCalled()', 'expect(phone.save).not.toHaveBeenCalled()')
replace('} else expect(download).toHaveBeenCalledOnce()', """} else {
      expect(phone.save).toHaveBeenCalledOnce()
      expect(phone.save).toHaveBeenCalledWith(expect.objectContaining({ bot_id: 'specialist', content_hash: hash }))
    }""")
replace("    expect(root.querySelectorAll('a')).toHaveLength(0)\n    deliveries.value", """    expect(root.querySelectorAll('a')).toHaveLength(0)
    expect([...root.querySelectorAll('button')].some(button => button.textContent?.trim() === en.bots.files.download)).toBe(false)
    deliveries.value""")
replace("    root.querySelector('img')!.closest('button')!.click(); await nextTick()", """    await vi.advanceTimersByTimeAsync(1); await nextTick()
    expect(phone.original).toHaveBeenCalledWith(expect.objectContaining({ bot_id: 'specialist', content_hash: hash }), expect.any(AbortSignal))
    expect(root.querySelector('img')?.getAttribute('src')).toBe('blob:verified-phone-fixture')
    root.querySelector('img')!.closest('button')!.click()
    await vi.advanceTimersByTimeAsync(1); await nextTick()""")
replace(".toBe(`/api/bots/specialist/media/${hash}?token=synthetic-token`)", ".toBe('blob:verified-phone-fixture')")
replace("    deliveries.value = { ...deliveries.value, files: [...deliveries.value.files] }", """    const originalReads = phone.original.mock.calls.length
    deliveries.value = { ...deliveries.value, files: [...deliveries.value.files] }""")
replace("    expect(root.querySelector('[data-keyboard-scope=\"mediaLightbox\"]')).not.toBeNull()", """    expect(phone.original.mock.calls.length).toBe(originalReads)
    expect(root.querySelector('[data-keyboard-scope="mediaLightbox"]')).not.toBeNull()""")
path.write_text(text)

path = root / 'apps/web/src/pages/home/composables/useMediaGallery.test.ts'
text = path.read_text()
replace('        name: undefined,', '        name: undefined,\n        original: expect.any(Function),')
path.write_text(text)

path = components / 'attachment-block.test.ts'
text = path.read_text()
replace("vi.mock('@felinic/ui', () => ({ Skeleton: 'span' }))", """vi.mock('@felinic/ui', () => ({ Skeleton: 'span', toast: { error: vi.fn() } }))
const save = vi.hoisted(() => vi.fn())
vi.mock('@/lib/harmony-files', () => ({ originalBlob: vi.fn(), saveOriginal: save }))""")
replace('  vi.clearAllMocks()', '  vi.clearAllMocks()\n  save.mockResolvedValue(undefined)')
start = text.index("    const download = vi.spyOn(HTMLAnchorElement.prototype, 'click')")
end = text.index("    expect(fetchMock).not.toHaveBeenCalled()", start)
text = text[:start] + """    host.querySelector('button')!.click()
    expect(save).toHaveBeenCalledOnce()
    expect(save).toHaveBeenCalledWith(expect.objectContaining({ name, size: 201 * 1024 * 1024 }))
""" + text[end:]
path.write_text(text)

path = components / 'session-tasks.test.ts'
text = path.read_text()
replace("const api = vi.hoisted", """const phoneSave = vi.hoisted(() => vi.fn())
vi.mock('@/lib/harmony-files', () => ({ originalBlob: vi.fn(), saveOriginal: phoneSave }))
const api = vi.hoisted""")
replace('    vi.useFakeTimers()', '    vi.useFakeTimers()\n    phoneSave.mockReset().mockResolvedValue(undefined)')
replace("""    expect(links).toHaveLength(1)
    expect(links[0]!.getAttribute('href')).toBe(`/api/bots/bot/media/${'a'.repeat(64)}?token=synthetic-identity`)
    expect(links[0]!.getAttribute('download')).toBe('ready-result')""", """    expect(links).toHaveLength(0)
    button(en.bots.files.download).click()
    await settled()
    expect(phoneSave).toHaveBeenCalledOnce()
    expect(phoneSave).toHaveBeenCalledWith(expect.objectContaining({ bot_id: 'bot', content_hash: 'a'.repeat(64), name: 'ready-result', size: 42 }))""")
path.write_text(text)
print('Adapted 4 upstream attachment test files; login tests retain the upstream real generation counter.')
