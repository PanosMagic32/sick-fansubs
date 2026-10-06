/**
 * Platform capability helpers.
 *
 * `isStandalone` is the installed-app check the install control and the
 * push settings share: the `display-mode` media query covers Android and
 * desktop installs, `navigator.standalone` covers iOS.
 */

/** iOS/iPadOS detection, including the iPadOS 13+ desktop-mode UA. */
export function isIOS(nav: Navigator = navigator): boolean {
  const ua = nav.userAgent;
  return (
    /iphone|ipad|ipod/i.test(ua) ||
    (/macintosh/i.test(ua) && nav.maxTouchPoints > 1)
  );
}

/** True while the app runs as an installed PWA (its own window). */
export function isStandalone(win: Window = window): boolean {
  const nav = win.navigator as Navigator & { standalone?: boolean };
  return (
    win.matchMedia?.("(display-mode: standalone)").matches === true ||
    nav.standalone === true
  );
}
