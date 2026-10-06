/**
 * Baseline coverage test — pins that every component includes the shared
 * in-shadow baseline stylesheet (core/styles/baseline.css).
 *
 * Rationale: box-sizing/sr-only/focus/reduced-motion live in the shared
 * sheet, and a component that drops the import fails here — happy-dom has
 * no layout, so only a source-level pin can catch a mis-rendered sr-only
 * suffix. The test inspects each component's `static styles` for the
 * baseline's signature rules.
 */

import { describe, expect, test } from "bun:test";
import { stripCssComments } from "@shared/api/test-utils.js";
import type { CSSResult, LitElement } from "lit";

import "../shell/app-shell.js";
import "../shell/app-header.js";
import "../shell/app-footer.js";
import "../shell/donation-banner.js";
import "../shell/offline-banner.js";
import "@features/auth/sign-in-page.js";
import "@features/auth/register-page.js";
import "@features/auth/account-page.js";
import "@features/notifications/ui/push-settings.js";
import "@shared/pwa/install-control.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/content-card.js";

const COMPONENTS = [
  "app-shell",
  "app-header",
  "app-footer",
  "donation-banner",
  "offline-banner",
  "sign-in-page",
  "register-page",
  "account-page",
  "push-settings",
  "install-control",
  "loading-spinner",
  "error-banner",
  "content-card",
] as const;

function styleText(tag: string): string {
  const ctor = customElements.get(tag) as typeof LitElement;
  const styles = (ctor as unknown as { styles?: CSSResult | CSSResult[] })
    .styles;
  const list = Array.isArray(styles) ? styles : [styles];
  return list.map((s) => s?.cssText ?? "").join("\n");
}

describe("in-shadow baseline coverage", () => {
  for (const tag of COMPONENTS) {
    test(`${tag} imports the shared baseline and control sheets`, () => {
      expect(customElements.get(tag)).toBeDefined();
      const css = styleText(tag);
      // bunfig.toml maps `.css` to bun's text loader, so `?inline`
      // imports carry the REAL CSS under bun test — Bun's own resolution
      // yields an empty object, and the loader keeps the rules
      // inspectable. The signature is the
      // baseline's own top-level `.sr-only` rule: component sheets reuse
      // the pattern only as a COMMA-JOINED selector (`.sr-only,`), so
      // this exact form can only come from baseline.css. A component
      // that drops the import fails here; the real Vite build carries
      // the same rules into the same slot.
      expect(css).toContain(".sr-only {");
      // The control vocabulary (controls.css) rides the same slot — the
      // `.button` base rule is its signature. architecture.test.ts pins
      // the import for every styled component at the source level; this
      // is the runtime half.
      expect(css).toContain(".button {");
    });
  }
});

describe("reserved state block", () => {
  test("the shared baseline reserves a footprint for loading/empty/error states", async () => {
    const css = stripCssComments(
      await Bun.file(new URL("./baseline.css", import.meta.url)).text(),
    );
    const rule = css.match(/(?:^|\n)\.state-block \{([^}]*)\}/s)?.[1] ?? "";
    expect(rule).toContain("min-height: 14rem");
    // The message centers inside the reserved box: a reserved block with
    // top-hugging text would read as a layout bug.
    expect(rule).toContain("justify-content: center");

    // …and every component that consumes it must actually carry the rule:
    // bunfig maps `.css` to bun's text loader, so the inline sheet is
    // inspectable (the file above and the component's slot can only diverge
    // if a component stops importing the baseline — which the .sr-only
    // signature test above already catches).
    expect(styleText("app-shell")).toContain(".state-block {");
  });
});
