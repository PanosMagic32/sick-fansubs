const ALLOWED_PROTOCOLS = ['http:', 'https:', 'magnet:'];

/**
 * Safely opens a download URL via an anchor click.
 *
 * - `magnet:` links bypass URL parsing (the constructor rejects them in some
 *   environments) and trigger the system protocol handler directly.
 * - `http:` / `https:` links are validated and trigger a file download.
 * - Other protocols and malformed URLs are silently rejected (or reported via
 *   `onError` if provided).
 */
export function openSafeUrl(url: string | undefined, onError?: () => void): void {
  if (!url) return;

  // Magnet links bypass URL parsing — the URL constructor rejects them in some
  // environments because they lack a host. Use an anchor click to trigger the
  // system's magnet: protocol handler instead.
  if (/^magnet:/i.test(url)) {
    const a = document.createElement('a');
    a.href = url;
    a.click();
    return;
  }

  try {
    const parsed = new URL(url);
    if (!ALLOWED_PROTOCOLS.includes(parsed.protocol)) {
      onError?.();
      return;
    }
    const a = document.createElement('a');
    a.href = parsed.href;
    a.download = parsed.pathname.split('/').pop() || '';
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.click();
  } catch {
    onError?.();
  }
}
