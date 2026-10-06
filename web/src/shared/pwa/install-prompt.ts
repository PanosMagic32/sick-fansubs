/**
 * Deferred install prompt state.
 *
 * The browser owns the install prompt: it fires `beforeinstallprompt` when
 * the app is installable, and the page may stash the event and replay it
 * from its own control (`./install-control.ts` — used by the home donation
 * strip and the account page's profile panel). The stash lives here at
 * module scope, wired once by `core/index.ts`, because the event can fire
 * before either host renders: the strip is home-only, and the account page
 * mounts whenever the user opens it.
 *
 * `prompt()` is single-use: consuming the deferred event clears it, and the
 * browser fires a fresh event only if the user is still installable.
 */

/** The Chromium event (lib.dom has no type for it). */
interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>;
}

let deferred: BeforeInstallPromptEvent | null = null;
let wired = false;
const subscribers = new Set<() => void>();

function notify(): void {
  for (const subscriber of subscribers) subscriber();
}

/** Wires the capture listeners. Called once, from `core/index.ts`. */
export function watchInstallPrompt(win: Window): void {
  // A second call is a no-op: two listeners would notify twice for one
  // event (and the capture is a page-level concern, not per-consumer).
  if (wired) return;
  wired = true;

  win.addEventListener("beforeinstallprompt", (event) => {
    // Suppress the browser's own mini-infobar: the app offers its own
    // control.
    event.preventDefault();
    deferred = event as BeforeInstallPromptEvent;
    notify();
  });
  win.addEventListener("appinstalled", () => {
    deferred = null;
    notify();
  });
}

/** True while the browser is offering installation for this page. */
export function installPromptAvailable(): boolean {
  return deferred !== null;
}

/** Subscribes to availability changes; returns the unsubscribe function. */
export function subscribeInstallPrompt(callback: () => void): () => void {
  subscribers.add(callback);
  return () => {
    subscribers.delete(callback);
  };
}

/** Replays the deferred prompt (single-use) and clears the stash. */
export function consumeInstallPrompt(): Promise<void> {
  const event = deferred;
  if (!event) return Promise.resolve();
  deferred = null;
  notify();
  return event.prompt();
}

/**
 * This browser already allowed notifications. Push subscriptions are
 * per-device, so a server-side "has push" flag would hide the install
 * control on a desktop because a phone is subscribed; the permission is
 * the honest per-browser signal. Granting and later unsubscribing hides
 * the control conservatively (accepted).
 */
export function notificationsGranted(): boolean {
  return (
    typeof Notification !== "undefined" && Notification.permission === "granted"
  );
}

/** Clears the stash — tests only, so one case cannot leak into the next. */
export function resetInstallPrompt(): void {
  deferred = null;
  notify();
}
