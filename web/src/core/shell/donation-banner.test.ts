import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { stripCssComments } from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";
import { BUY_ME_A_COFFEE_URL } from "@shared/config/links.js";
import {
  resetInstallPrompt,
  watchInstallPrompt,
} from "@shared/pwa/install-prompt.js";

import "./donation-banner.js";
import type { DonationBanner } from "./donation-banner.js";

async function renderBanner(): Promise<DonationBanner> {
  const banner = document.createElement("donation-banner") as DonationBanner;
  document.body.appendChild(banner);
  await banner.updateComplete;
  return banner;
}

function root(banner: DonationBanner): HTMLElement {
  return banner.shadowRoot! as unknown as HTMLElement;
}

beforeEach(() => {
  // The strip hosts the install control; a prompt stashed by another file
  // in the same happy-dom worker must not decide this file's rendering.
  resetInstallPrompt();
  watchInstallPrompt(window);
});

afterEach(() => {
  document.body.innerHTML = "";
  resetInstallPrompt();
});

describe("donation-banner", () => {
  test("renders the Greek sentence and the external link", async () => {
    const banner = await renderBanner();

    const text = root(banner)
      .querySelector("p")
      ?.textContent?.replace(/\s+/g, " ")
      .trim();
    expect(text).toBe(el.donation.bannerText);

    const link = root(banner).querySelector("a");
    expect(link?.getAttribute("href")).toBe(BUY_ME_A_COFFEE_URL);
    expect(link?.getAttribute("target")).toBe("_blank");
    expect(link?.getAttribute("rel")).toBe("noopener noreferrer");
    // Brand label is an anglicism — inside lang="en".
    expect(link?.querySelector('span[lang="en"]')?.textContent?.trim()).toBe(
      el.about.donateLabel,
    );
  });

  test("hosts the shared install control on the right", async () => {
    const banner = await renderBanner();

    const control = root(banner).querySelector("install-control");
    expect(control).not.toBeNull();
    // The strip owns layout only — the control's four states are pinned in
    // shared/pwa/install-control.test.ts.
    expect(control?.parentElement?.classList.contains("banner")).toBe(true);
  });

  test("pins the slim, main-column-width CSS contract", async () => {
    const styles = stripCssComments(
      await Bun.file(new URL("./donation-banner.css", import.meta.url)).text(),
    );
    // Same width as the main content column (the 80rem wide-route cap).
    // Anchored on the :host rule — a bare `.banner` alternate would pass on
    // any sheet that renders one.
    const host = styles.match(/:host \{([^}]*)\}/)?.[1] ?? "";
    expect(host).toContain("max-width: 80rem");
    // Slim: the banner padding stays under the header's presence.
    expect(styles).toMatch(/padding: 0\.45rem 1rem/);
    // The sentence and the donation link form the left group, the install
    // control sits on the right. Its auto left margin is the whole
    // mechanism, so a refactor must not drop it.
    const control =
      styles.match(/\.banner install-control \{([^}]*)\}/)?.[1] ?? "";
    expect(control).toContain("margin-left: auto");
    // The pill clears the WCAG 2.5.8 24px target minimum.
    const pillRule = styles.match(/\.banner a \{([^}]*)\}/)?.[1] ?? "";
    expect(pillRule).toContain("min-height: 1.5rem");
  });
});
