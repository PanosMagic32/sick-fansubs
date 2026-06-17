const ALLOWED_PROTOCOLS = ['http:', 'https:', 'magnet:'];

/**
 * Safely opens a URL in a new window, rejecting potentially dangerous protocols
 * such as javascript: or data:.
 */
export function openSafeUrl(url: string | undefined, target = '_blank'): void {
  if (!url) return;

  try {
    const parsed = new URL(url);
    if (!ALLOWED_PROTOCOLS.includes(parsed.protocol)) return;
    window.open(parsed.href, target);
  } catch {
    // Silently ignore malformed URLs
  }
}
