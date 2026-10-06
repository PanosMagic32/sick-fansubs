import { afterEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  restoreMockFetch,
  stripCssComments,
} from "@shared/api/test-utils.js";
import {
  BUY_ME_A_COFFEE_URL,
  DISCORD_URL,
  FACEBOOK_URL,
  GITHUB_URL,
  TRACKER_URL,
} from "@shared/config/links.js";

// Side-effect import registers the custom element; the type import is
// erased at compile time (bun tree-shakes type-only named imports — a
// type-only import of the class would silently skip registration).
import "./about-page.js";
import type { AboutPage } from "./about-page.js";

// Both sheets are read from disk with comments stripped, so prose can never
// satisfy a pin (testing rule 7).
const pageStyles = stripCssComments(
  await Bun.file(new URL("./about-page.css", import.meta.url)).text(),
);
const shellStyles = stripCssComments(
  await Bun.file(
    new URL("../../core/shell/app-shell.css", import.meta.url),
  ).text(),
);
const darkTokens = shellStyles.match(/:host \{([^}]*)\}/s)?.[1] ?? "";
const lightTokens =
  shellStyles.match(/:host\(\.light\) \{([^}]*)\}/s)?.[1] ?? "";

/** The five anchors' labels, in render order. */
const LABELS = [
  el.about.facebookLabel,
  el.about.discordLabel,
  el.about.githubLabel,
  el.about.trackerLabel,
  el.about.donateLabel,
];

async function renderPage(): Promise<AboutPage> {
  const page = document.createElement("about-page");
  document.body.appendChild(page);
  await page.updateComplete;
  return page;
}

function root(page: AboutPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

function text(root: HTMLElement, selector: string): string {
  return (root.querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

afterEach(() => {
  document.body.innerHTML = "";
  restoreMockFetch();
});

describe("about-page", () => {
  test("renders the team logo on top (presentational, same asset as the header)", async () => {
    const page = await renderPage();

    const logo = root(page).querySelector("img.about-logo");
    expect(logo?.getAttribute("src")).toBe("/logo/logo.png");
    // Presentational — the adjacent h1 conveys the name (the same pair as
    // the header brand logo).
    expect(logo?.getAttribute("alt")).toBe("");
    expect(logo?.getAttribute("role")).toBe("presentation");
    // The logo sits ABOVE the headline.
    expect(
      root(page).querySelector(".about")?.firstElementChild?.className,
    ).toContain("about-logo");

    // CSS contract: a centered circular crop with a hairline border.
    const logoBlock = pageStyles.match(/\.about-logo \{([^}]*)\}/)?.[1] ?? "";
    expect(logoBlock).toContain("border-radius: 50%;");
    expect(logoBlock).toContain("width: 10rem;");
  });

  test("renders the headline, intro, and members line from the catalog", async () => {
    const page = await renderPage();

    expect(text(root(page), ".about-title")).toBe(el.about.pageTitle);
    expect(text(root(page), ".about-subtitle")).toBe(el.about.pageSubtitle);
    expect(text(root(page), ".about-intro")).toBe(el.about.intro);
    expect(text(root(page), ".about-members")).toBe(
      `${el.about.membersLabel} ${el.about.members}`,
    );
  });

  test("the members line lists the catalog roster verbatim (public names only)", async () => {
    const page = await renderPage();

    expect(text(root(page), ".members-names")).toBe(el.about.members);
  });

  test("renders the five community links with secure external attributes", async () => {
    const page = await renderPage();

    const anchors = [
      ...root(page).querySelectorAll<HTMLAnchorElement>(".about-links a"),
    ];
    expect(anchors.length).toBe(5);
    // `list-style: none` needs the explicit role for list semantics to
    // survive in some browsers.
    expect(
      root(page).querySelector("ul.about-links")?.getAttribute("role"),
    ).toBe("list");

    expect(anchors.map((a) => a.getAttribute("href"))).toEqual([
      FACEBOOK_URL,
      DISCORD_URL,
      GITHUB_URL,
      TRACKER_URL,
      BUY_ME_A_COFFEE_URL,
    ]);

    for (const [i, anchor] of anchors.entries()) {
      expect(anchor.textContent?.trim()).toBe(LABELS[i]!);
      expect(anchor.getAttribute("target")).toBe("_blank");
      expect(anchor.getAttribute("rel")).toBe("noopener noreferrer");
      // Every anchor sits INSIDE its Greek sentence, not beside it.
      expect(anchor.parentElement?.classList.contains("link-desc")).toBe(true);
    }
  });

  test("scopes lang=en to the brand names; the tracker loanword stays plain", async () => {
    const page = await renderPage();

    const anchors = [
      ...root(page).querySelectorAll<HTMLAnchorElement>(".about-links a"),
    ];
    const scoped = anchors.map(
      (a) => a.querySelector("span[lang='en']")?.textContent?.trim() ?? null,
    );
    expect(scoped).toEqual([
      el.about.facebookLabel,
      el.about.discordLabel,
      el.about.githubLabel,
      null,
      el.about.donateLabel,
    ]);
  });

  test("keeps the brand anchor inside its Greek sentence", async () => {
    const page = await renderPage();

    const desc = root(page).querySelector(".link-desc")!;
    const anchor = desc.querySelector("a")!;
    expect(anchor.textContent?.trim()).toBe(el.about.facebookLabel);
    // The prefix and suffix are part of the same sentence, so the anchor
    // cannot split off into a label + detached link.
    const sentence = desc.textContent ?? "";
    const labelAt = sentence.indexOf(el.about.facebookLabel);
    expect(sentence.slice(0, labelAt)).toBe(el.about.facebookPrefix);
    expect(sentence.slice(labelAt + el.about.facebookLabel.length)).toBe(
      el.about.facebookSuffix,
    );
    expect(text(root(page), ".link-row")).toBe(
      `${el.about.facebookPrefix}${el.about.facebookLabel}${el.about.facebookSuffix}`,
    );
  });

  test("tints each service row with its brand tone", async () => {
    const page = await renderPage();

    const tones = [...root(page).querySelectorAll(".link-row")].map((row) =>
      [...row.classList].find((c) => c.startsWith("tone-")),
    );
    expect(tones).toEqual([
      "tone-facebook",
      "tone-discord",
      "tone-github",
      "tone-tracker",
      "tone-coffee",
    ]);

    // CSS contract: the tint comes from a per-row --row-accent custom
    // property consumed by color-mix (nearly-transparent bg + border), and
    // each tone reads a brand token pair declared in the shell.
    expect(pageStyles).toContain("color-mix(in srgb, var(--row-accent) 10%");
    expect(pageStyles).toContain("color-mix(in srgb, var(--row-accent) 45%");
    for (const tone of ["facebook", "discord", "github", "tracker", "coffee"]) {
      expect(pageStyles).toMatch(
        new RegExp(
          `\\.tone-${tone} \\{[^}]*--row-accent: var\\(--sf-brand-${tone}\\)`,
        ),
      );
      expect(darkTokens).toContain(`--sf-brand-${tone}:`);
      expect(lightTokens).toContain(`--sf-brand-${tone}:`);
    }
  });

  test("link text rides the tint-safe token pair and keeps its underline", async () => {
    // happy-dom resolves no tokens and computes no text decoration: the
    // rule and both theme declarations are pinned against the stripped
    // sources. The exact values live in the shell's own suite.
    const linkBlock = pageStyles.match(/\.link-desc a \{([^}]*)\}/)?.[1] ?? "";
    expect(linkBlock).toContain("color: var(--sf-link-on-tint);");
    expect(linkBlock).toContain("text-decoration: underline;");
    expect(darkTokens).toContain("--sf-link-on-tint:");
    expect(lightTokens).toContain("--sf-link-on-tint:");
  });

  test("renders synchronously without fetching (static page)", async () => {
    let called = false;
    mockFetchImpl(() => {
      called = true;
      return Promise.reject(new Error("unexpected fetch"));
    });

    const page = await renderPage();
    expect(text(root(page), ".about-title")).toBe(el.about.pageTitle);
    expect(called).toBe(false);
  });
});
