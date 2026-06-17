/**
 * Escapes special regex characters in a string so it can be safely used
 * in MongoDB `$regex` queries without risk of ReDoS or unintended patterns.
 */
export function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
