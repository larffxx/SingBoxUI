/**
 * Tag allocation for tagged sing-box resources.
 *
 * Tags are the only stable identity sing-box offers inside outbounds, inbounds
 * and endpoints, so every insert must produce a tag that is not already in use.
 * The function never mutates its input and never returns an empty tag.
 */
export function uniqueTag(taken: Iterable<string>, base: string): string {
  const used = new Set(taken)
  const clean = base.trim() === '' ? 'untitled' : base.trim()
  if (!used.has(clean)) return clean
  let index = 2
  while (used.has(`${clean}-${index}`)) index += 1
  return `${clean}-${index}`
}
