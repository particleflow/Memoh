/** Conservative streaming boundaries, using native sentence segmentation plus
 * the abbreviation/initial/decimal protection used by LiveKit's basic sentence
 * tokenizer. Pipecat's SimpleTextAggregator likewise retains ambiguous periods
 * for textual lookahead rather than committing punctuation on the token alone.
 * Sources and deliberately excluded length/delay heuristics: continuity DESIGN. */
export function completedSpeechPrefix(text: string): number {
  const protect = (value: string) => value.replace(/\./gu, '\u2060')
  const masked = text
    .replace(/\b(?:Mr|Mrs|Ms|Dr|St|Prof|Capt|Cpt|Lt|Inc|Ltd|Jr|Sr|Co|Ph\.D)\./gu, protect)
    .replace(/\b(?:[A-Za-z]\.){2,}/gu, protect)
    .replace(/\b[A-Z]\.(?=\s+[A-Z][\p{L}])/gu, protect)
    .replace(/[\p{L}\p{N}]\.(?=[\p{L}\p{N}])/gu, protect)
    .replace(/(?:^|\s)\d+\.(?=\s|$)/gu, protect)
    .replace(/\.{2,}/gu, protect)
  // Masks have exactly the same UTF-16 length, so offsets still name the
  // untouched visible text. No synthetic overlap is sent to TTS or read twice.
  const sentences = typeof Intl.Segmenter === 'function'
    ? Array.from(new Intl.Segmenter(undefined, { granularity: 'sentence' }).segment(masked), item => item.segment)
    : masked.match(/[^。！？.!?\n]*[。！？.!?\n]+(?:["'”’」』)）]*\s*)?|.+$/gu) ?? []
  let end = 0
  for (const sentence of sentences) {
    if (!/[\p{Sentence_Terminal}\n]["'”’」』)）]*\s*$/u.test(sentence)) break
    const next = end + sentence.length
    // As in Pipecat, punctuation alone is not enough: a following token or
    // response end resolves decimals, initials and Markdown closing delimiters.
    // The latter can still change how the renderer projects a CJK sentence.
    if (!masked.slice(next).trim()) break
    end = next
  }
  return end
}
