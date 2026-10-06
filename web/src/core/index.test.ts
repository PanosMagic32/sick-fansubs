/**
 * Entry-point registration contract.
 *
 * core/index.ts is the ONLY module that registers routed pages for the real
 * application. A route whose element is never imported renders as an
 * HTMLUnknownElement — no shadow root, no content, silently blank. The
 * detail-page component tests could not catch this: they import the page
 * directly. This test imports the real entry point and pins that the shell
 * and the primary routed pages are registered.
 */

import { afterEach, describe, expect, test } from "bun:test";

import {
  stripCssComments,
  stripSourceComments,
} from "@shared/api/test-utils.js";
import {
  installPromptAvailable,
  resetInstallPrompt,
} from "@shared/pwa/install-prompt.js";

// Side-effect import: the real application bootstrap.
import "./index.js";

// The stash is module state on a window shared by every file in this bun
// worker: an assertion failure above the reset would leak a stashed prompt
// into unrelated suites.
afterEach(() => {
  resetInstallPrompt();
});

describe("core/index registrations", () => {
  test("registers the shell and the primary routed pages", () => {
    const routed = [
      "app-shell",
      "blog-list-page",
      "blog-detail-page",
      "projects-list-page",
      "project-detail-page",
      "sign-in-page",
      "register-page",
      "account-page",
    ];
    for (const tag of routed) {
      expect(customElements.get(tag), tag).toBeDefined();
    }
  });

  test("wires the install prompt capture", async () => {
    // Source-level pin first: the behavioural case below cannot fail on its
    // own, because every pwa suite in this worker calls watchInstallPrompt
    // and the `wired` guard plus the shared window make the later call a
    // no-op. Only the reading of this entry point proves the REAL app wires
    // the capture. Comments are stripped so prose can never satisfy the pin.
    const source = stripSourceComments(
      await Bun.file(new URL("./index.ts", import.meta.url)).text(),
    );
    expect(source).toMatch(/watchInstallPrompt\(window\)/);
  });

  test("every @font-face is swap-displayed", async () => {
    // Typography has one home (core/index.css): the five vendored IBM Plex
    // faces, each with font-display: swap so a slow webfont never blocks
    // the Greek UI's first paint. happy-dom fetches no fonts, so the
    // contract is pinned against the source.
    const css = stripCssComments(
      await Bun.file(new URL("./index.css", import.meta.url)).text(),
    );
    const faces = css.match(/@font-face \{[^}]*\}/g) ?? [];
    expect(faces).toHaveLength(5);
    for (const face of faces) {
      expect(face).toContain("font-display: swap");
    }
  });

  test("captures the install prompt from the first frame", () => {
    // The wiring lives in this entry point only; the two hosts (donation
    // strip, account page) replay the stash. Without this import the
    // install control would never light up in the real application.
    const event = new Event("beforeinstallprompt", { cancelable: true });
    Object.assign(event, { prompt: () => Promise.resolve() });

    window.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(installPromptAvailable()).toBe(true);
  });
});
