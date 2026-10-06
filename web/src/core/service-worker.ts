/**
 * Service-worker registration.
 *
 * Production only. In Vite dev a worker would cache dev assets and shadow
 * live changes; bun tests run without import.meta.env.PROD set, so the
 * guard is falsy there too. The hourly update() call keeps long-lived tabs
 * from pinning an old worker forever — the worker itself still activates
 * conservatively (no skipWaiting, next visit wins).
 */

export function registerServiceWorker(
  isProd: boolean,
  navigatorRef: Navigator,
  updateIntervalMs = 60 * 60 * 1000,
): void {
  if (!isProd || !("serviceWorker" in navigatorRef)) {
    return;
  }
  navigatorRef.serviceWorker
    .register("/sw.js")
    .then((registration) => {
      // The worker activates conservatively (no skipWaiting) — update()
      // just makes long-lived tabs install newer workers in the
      // background; its own rejection is noise, never an error state.
      window.setInterval(
        () => registration.update().catch(() => {}),
        updateIntervalMs,
      );
    })
    .catch(() => {
      // Offline support is optional, never an error state: private-mode
      // browsers and some embedded webviews refuse registration, and the
      // app works fully without a worker.
    });
}
