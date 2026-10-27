import { expect, it } from 'vitest'
import { imageTimingHandle } from './harmony-image-timing'

it('imports the default-off collector outside a browser without adding a window or collecting', () => {
  expect(typeof window).toBe('undefined')
  expect(imageTimingHandle({})).toBeUndefined()
  expect(Object.hasOwn(globalThis, '__garnetImageTiming')).toBe(false)
})
