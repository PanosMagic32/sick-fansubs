import { afterEach, describe, expect, test } from "bun:test";
import { html } from "lit";

import { stripCssComments } from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";
import type { AvatarUserRef } from "./avatar.js";

// Side-effect import registers the custom element.
import "./content-card.js";
import type { ContentCard } from "./content-card.js";

const cardStyles = stripCssComments(
  await Bun.file(new URL("./content-card.css", import.meta.url)).text(),
);

function root(card: ContentCard): HTMLElement {
  return card.shadowRoot! as unknown as HTMLElement;
}

function text(card: ContentCard, selector: string): string {
  return (root(card).querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

/** Render a card with the given inputs — every shape goes through here. */
async function render(props: Partial<ContentCard>): Promise<ContentCard> {
  const card = document.createElement("content-card");
  Object.assign(card, props);
  document.body.appendChild(card);
  await card.updateComplete;
  return card;
}

const creator: AvatarUserRef = { username: "creator", avatarUrl: null };

afterEach(() => {
  document.body.innerHTML = "";
});

describe("content-card", () => {
  test("the grid shape reads every input it consumes", async () => {
    const card = await render({
      shape: "grid",
      href: "/blog/p1",
      title: "Τίτλος",
      subtitle: "Υπότιτλος",
      description: "Περιγραφή",
      thumbnailUrl: "https://example.com/t.jpg",
      creator,
      publishedAt: "2026-08-15T18:30:00.000Z",
      srUsername: true,
      commentCount: 3,
      favoriteCount: 0,
    });

    const link = root(card).querySelector("a.link") as HTMLAnchorElement;
    expect(link.getAttribute("href")).toBe("/blog/p1");
    // The anchor wraps the whole card: one click target, no per-element
    // links inside the frame.
    expect(link.querySelector("article.frame")).not.toBeNull();
    expect(root(card).querySelector("img.thumb")?.getAttribute("src")).toBe(
      "https://example.com/t.jpg",
    );
    expect(text(card, "h2.title")).toBe("Τίτλος");
    expect(text(card, ".subtitle")).toBe("Υπότιτλος");
    expect(text(card, ".description")).toBe("❝ Περιγραφή ❞");
    // The byline: the avatar chip and the numeric Greek date.
    expect(root(card).querySelector(".meta .avatar")).not.toBeNull();
    expect(text(card, ".meta .sr-only")).toBe("creator");
    expect(text(card, ".meta .date")).toBe("15/08/2026, 18:30");
    // Both indicators, including the zero favorite count.
    expect(root(card).querySelectorAll(".content-stats .stat").length).toBe(2);
    const numbers = [...root(card).querySelectorAll(".stat-num")].map((n) =>
      n.textContent?.trim(),
    );
    expect(numbers).toEqual(["3", "0"]);
    expect(root(card).querySelector(".more")?.getAttribute("aria-label")).toBe(
      el.ui.details,
    );

    card.remove();
  });

  test("a null thumbnail keeps the grid cell; an absent description and subtitle render nothing", async () => {
    const card = await render({
      shape: "grid",
      href: "/projects/p1",
      title: "Έργο",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
    });

    expect(root(card).querySelector("img.thumb")).toBeNull();
    // The empty cell keeps the 16/9 rhythm (the card's own class).
    expect(root(card).querySelector(".thumb-empty")).not.toBeNull();
    expect(root(card).querySelector(".subtitle")).toBeNull();
    expect(root(card).querySelector(".description")).toBeNull();
    // No counts means no indicator row at all (a half-set pair is a
    // programming error and also renders nothing).
    expect(root(card).querySelector(".content-stats")).toBeNull();
    expect(text(card, ".date")).toBe("15/08/2026, 18:30");

    card.remove();
  });

  test('the list cards read "Title – Subtitle" inline behind a decorative separator', async () => {
    for (const shape of ["grid", "hero", "row"] as const) {
      const card = await render({
        shape,
        href: "/blog/p1",
        title: "One Piece",
        subtitle: "Επεισόδιο 1.038",
      });

      const line = root(card).querySelector(".title-line") as HTMLElement;
      expect(line).not.toBeNull();
      const title = line.querySelector(".title") as HTMLElement;
      const sep = line.querySelector(".subtitle-sep") as HTMLElement;
      const subtitle = line.querySelector(".subtitle") as HTMLElement;
      expect(title.textContent).toBe("One Piece");
      expect(sep.textContent?.trim()).toBe("–");
      expect(sep.getAttribute("aria-hidden")).toBe("true");
      expect(subtitle.textContent).toBe("Επεισόδιο 1.038");
      // Document order: title, then separator, then subtitle.
      expect(
        title.compareDocumentPosition(sep) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
      expect(
        sep.compareDocumentPosition(subtitle) &
          Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();

      card.remove();
    }
  });

  test("a row without a subtitle keeps its plain title", async () => {
    const card = await render({
      shape: "row",
      dense: true,
      title: "Τίτλος",
      secondary: "slug-one",
    });
    expect(root(card).querySelector<HTMLElement>(".title")?.textContent).toBe(
      "Τίτλος",
    );
    expect(root(card).querySelector(".title-line")).toBeNull();
    card.remove();
  });

  test("the title-line sheet keeps the inline baseline row (raw-sheet pin)", () => {
    // happy-dom computes no layout, so the inline arrangement is pinned
    // against the raw sheet: a block slide-back would silently restore the
    // stacked subtitle. The sheet is comment-stripped at load — a prose line
    // naming a retired declaration must never satisfy a pin.
    // The title-line is a base rule: every shape that renders one shares
    // it, and the per-shape deltas only set the gap.
    const rule = cardStyles.match(/\n\.title-line \{([^}]*)\}/)?.[1] ?? "";
    expect(rule).toContain("display: flex");
    expect(rule).toContain("align-items: baseline");
    expect(rule).toContain("flex-wrap: wrap");
    // The search row's delta keeps the inline gap and the plain title's
    // block spacing (the subtitle must not collapse onto the description).
    const rowDelta =
      cardStyles.match(
        /\n:host\(\[shape="row"\]\) \.title-line \{([^}]*)\}/,
      )?.[1] ?? "";
    expect(rowDelta).toContain("column-gap: 0.4rem");
    expect(rowDelta).toMatch(/margin:\s*0 0 6px/);
    // The inner title drops its plain block margin so the line owns the
    // spacing; losing this reset would double it.
    const rowTitleReset =
      cardStyles.match(
        /\n:host\(\[shape="row"\]\) \.title-line \.title \{([^}]*)\}/,
      )?.[1] ?? "";
    expect(rowTitleReset).toMatch(/margin:\s*0;/);
    // The dense delta drops the block spacing entirely: the body gap owns
    // the rhythm, so a dense row with and without a subtitle sits the same
    // way above its description.
    const denseDelta =
      cardStyles.match(
        /\n:host\(\[shape="row"\]\[dense\]\) \.title-line \{([^}]*)\}/,
      )?.[1] ?? "";
    expect(denseDelta).toMatch(/margin:\s*0;/);
  });

  test("the stats chip renders only when BOTH counts are set", async () => {
    const partial = await render({
      shape: "grid",
      title: "X",
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 4,
    });
    expect(root(partial).querySelector(".content-stats")).toBeNull();
    partial.remove();

    const zeroes = await render({
      shape: "grid",
      title: "X",
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    });
    expect(
      [...root(zeroes).querySelectorAll(".stat-num")].map((n) =>
        n.textContent?.trim(),
      ),
    ).toEqual(["0", "0"]);
    expect(
      root(zeroes).querySelector(".stat")?.getAttribute("aria-label"),
    ).toContain("0");
    zeroes.remove();
  });

  test("headingLevel picks the heading tag (2 is the default; 0 is text)", async () => {
    for (const [level, tag] of [
      [0, "span"],
      [1, "h1"],
      [2, "h2"],
      [3, "h3"],
    ] as const) {
      const card = await render({
        shape: "grid",
        href: "/blog/p1",
        title: "Τίτλος",
        headingLevel: level,
        publishedAt: "2026-08-15T18:30:00.000Z",
      });
      expect(root(card).querySelector(".title")?.localName).toBe(tag);
      card.remove();
    }
  });

  test("the hero shape puts the title beside the badge and keeps the frame's lead treatment", async () => {
    const card = await render({
      shape: "hero",
      href: "/blog/p1",
      title: "Τίτλος",
      badge: el.blog.latestBadge,
      badgeTone: "latest",
      thumbnailUrl: "https://example.com/t.jpg",
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 1,
      favoriteCount: 2,
    });

    const topline = root(card).querySelector(".topline") as HTMLElement;
    expect(topline.querySelector(".title")?.textContent).toBe("Τίτλος");
    const badge = topline.querySelector(".badge");
    expect(badge?.textContent?.trim()).toBe(el.blog.latestBadge);
    expect(badge?.getAttribute("data-tone")).toBe("latest");
    // The hero reads the same footer inputs as the grid shape.
    expect(root(card).querySelector(".footer .content-stats")).not.toBeNull();
    expect(root(card).querySelector(".footer .more")).not.toBeNull();

    card.remove();
  });

  test("an empty badge renders no chip at all", async () => {
    const card = await render({
      shape: "hero",
      href: "/blog/p1",
      title: "Τίτλος",
      thumbnailUrl: "https://example.com/t.jpg",
      publishedAt: "2026-08-15T18:30:00.000Z",
    });
    expect(root(card).querySelector(".badge")).toBeNull();
    card.remove();
  });

  test("a hero without a thumbnail keeps the image cell as a seam", async () => {
    const card = await render({
      shape: "hero",
      href: "/blog/p1",
      title: "Τίτλος",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
    });
    expect(root(card).querySelector("img.thumb")).toBeNull();
    // The body keeps its own column: the empty cell holds the first track.
    expect(root(card).querySelector(".thumb-empty")).not.toBeNull();
    card.remove();
  });

  test("a badge may carry markup, so an anglicism keeps its lang wrapper", async () => {
    const card = await render({
      shape: "row",
      title: "Τίτλος",
      badge: html`<span lang="en">Projects</span>`,
      badgeTone: "project",
    });
    const badge = root(card).querySelector(".badge") as HTMLElement;
    expect(badge.textContent?.trim()).toBe("Projects");
    expect(badge.querySelector('[lang="en"]')).not.toBeNull();
    card.remove();
  });

  test("a linked row anchors its frame; a static row keeps the actions slot", async () => {
    const linked = await render({
      shape: "row",
      href: "/blog/p1",
      title: "Τίτλος",
      badge: el.search.typePosts,
      badgeTone: "accent",
      description: "Κείμενο",
      metaInstant: "2026-08-15T18:30:00.000Z",
      thumbnailUrl: "https://example.com/t.jpg",
      commentCount: 1,
      favoriteCount: 2,
    });
    const anchor = root(linked).querySelector("a.frame") as HTMLAnchorElement;
    expect(anchor.getAttribute("href")).toBe("/blog/p1");
    expect(anchor.querySelector(".body")).not.toBeNull();
    expect(root(linked).querySelector("div.frame")).toBeNull();
    // The row's description is plain text — the quote treatment belongs to
    // the cards (asserted with the quote pin below).
    expect(text(linked, ".description")).toBe("Κείμενο");
    // The overlay slot is the linked row's action door; it is a sibling of
    // the frame, never inside the anchor.
    expect(anchor.querySelector("slot")).toBeNull();
    linked.remove();

    const plain = await render({
      shape: "row",
      title: "Τίτλος",
      metaInstant: "2026-08-15T18:30:00.000Z",
    });
    const frame = root(plain).querySelector("div.frame") as HTMLElement;
    expect(frame.querySelector('slot[name="actions"]')).not.toBeNull();
    plain.remove();
  });

  test("the row meta line renders the label, the middle dot and a time element", async () => {
    const card = await render({
      shape: "row",
      dense: true,
      title: "Τίτλος",
      badge: el.admin.statusDraft,
      badgeTone: "draft",
      metaLabel: el.blog.updated,
      metaInstant: "2026-08-15T18:30:00.000Z",
    });

    expect(card.hasAttribute("dense")).toBe(true);
    const meta = root(card).querySelector(".meta") as HTMLElement;
    expect(meta.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `${el.blog.updated} · 15/08/2026, 18:30`,
    );
    // The dot is decorative, like the detail byline's separator: hidden from
    // assistive tech so the label and the date stay separate values.
    expect(meta.querySelector('[aria-hidden="true"]')?.textContent).toBe(" · ");
    const time = meta.querySelector("time") as HTMLTimeElement;
    expect(time.getAttribute("datetime")).toBe("2026-08-15T18:30:00.000Z");

    card.remove();
  });

  test("the row meta renders without a label when none is set", async () => {
    const card = await render({
      shape: "row",
      title: "Τίτλος",
      metaInstant: "2026-08-15T18:30:00.000Z",
    });
    expect(text(card, ".meta")).toBe("15/08/2026, 18:30");
    expect(root(card).querySelector(".meta-label")).toBeNull();
    expect(root(card).querySelector('.meta [aria-hidden="true"]')).toBeNull();
    card.remove();
  });

  test("the dense row renders its secondary line", async () => {
    const card = await render({
      shape: "row",
      dense: true,
      title: "Τίτλος",
      secondary: "project-one",
    });
    expect(text(card, ".secondary")).toBe("project-one");
    card.remove();
  });

  test("an assigned overlay control marks the host so the row reserves its corner", async () => {
    const card = await render({ shape: "row", dense: true, title: "Τίτλος" });
    expect(card.hasAttribute("data-overlay")).toBe(false);

    const button = document.createElement("button");
    button.setAttribute("slot", "overlay");
    button.textContent = "×";
    card.appendChild(button);
    await card.updateComplete;

    expect(card.hasAttribute("data-overlay")).toBe(true);
    expect(root(card).querySelector(".body")).not.toBeNull();

    button.remove();
    await card.updateComplete;
    expect(card.hasAttribute("data-overlay")).toBe(false);

    card.remove();
  });

  test("the detail shape renders the lead block and both byline rules", async () => {
    const card = await render({
      shape: "detail",
      headingLevel: 1,
      title: "Τίτλος",
      subtitle: "Υπότιτλος",
      description: "Γραμμή μία\nΓραμμή δύο",
      thumbnailUrl: "https://example.com/t.jpg",
      creator,
      publishedAt: "2026-08-15T18:30:00.000Z",
      updater: { username: "editor", avatarUrl: null },
      updatedAt: "2026-08-16T10:00:00.000Z",
      commentCount: 2,
      favoriteCount: 1,
    });

    expect(root(card).querySelector(".thumb")).not.toBeNull();
    expect(root(card).querySelector("h1.title")).not.toBeNull();
    expect(text(card, ".subtitle")).toBe("Υπότιτλος");
    expect(
      root(card).querySelector(".detail-stats .content-stats"),
    ).not.toBeNull();
    // The raw description (line breaks included) reaches the text span; the
    // pre-line rule that renders those breaks is pinned at the source.
    expect(
      root(card).querySelector(".description .description-text")?.textContent,
    ).toBe("Γραμμή μία\nΓραμμή δύο");
    const lines = root(card).querySelectorAll(".detail-meta .meta-line");
    expect(lines.length).toBe(2);
    expect(lines[0]?.querySelector(".meta-label")?.textContent?.trim()).toBe(
      el.blog.published,
    );
    expect(lines[0]?.querySelector(".meta-byline")?.textContent?.trim()).toBe(
      "creator",
    );
    expect(lines[1]?.querySelector(".meta-label")?.textContent?.trim()).toBe(
      el.blog.updated,
    );
    expect(lines[1]?.querySelector(".meta-byline")?.textContent?.trim()).toBe(
      "editor",
    );

    card.remove();
  });

  test("the detail byline omits the update line when the content was never edited", async () => {
    const neverEdited = await render({
      shape: "detail",
      headingLevel: 1,
      title: "Τίτλος",
      publishedAt: "2026-08-15T18:30:00.000Z",
      updatedAt: "2026-08-15T18:30:00.000Z",
    });
    expect(
      root(neverEdited).querySelectorAll(".detail-meta .meta-line").length,
    ).toBe(1);
    neverEdited.remove();

    // The same instant in another valid form still counts as never edited:
    // the comparison is chronological, not text order.
    const offsetForm = await render({
      shape: "detail",
      headingLevel: 1,
      title: "Τίτλος",
      publishedAt: "2026-08-15T18:30:00.000Z",
      updatedAt: "2026-08-15T20:30:00+02:00",
    });
    expect(
      root(offsetForm).querySelectorAll(".detail-meta .meta-line").length,
    ).toBe(1);
    offsetForm.remove();

    // An anonymous creator: no avatar, no separator, no byline name — the
    // published line alone.
    const anonymous = await render({
      shape: "detail",
      headingLevel: 1,
      title: "Τίτλος",
      publishedAt: "2026-08-15T18:30:00.000Z",
    });
    const line = root(anonymous).querySelector(".meta-line") as HTMLElement;
    expect(line.querySelector(".meta-byline")).toBeNull();
    expect(line.querySelector(".meta-separator")).toBeNull();
    expect(line.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `${el.blog.published} 15/08/2026, 18:30`,
    );
    anonymous.remove();
  });

  test("the detail actions slot participates in the title row's layout", async () => {
    const card = await render({
      shape: "detail",
      headingLevel: 1,
      title: "Τίτλος",
      publishedAt: "2026-08-15T18:30:00.000Z",
    });
    const control = document.createElement("button");
    control.setAttribute("slot", "actions");
    control.textContent = "♥";
    card.appendChild(control);
    await card.updateComplete;

    // Slotted content is assigned to the shadow root's actions slot and
    // stays OUTSIDE the title (the page's own element).
    const slot = root(card).querySelector(
      'slot[name="actions"]',
    ) as HTMLSlotElement;
    expect(slot.assignedElements().length).toBe(1);
    expect(root(card).querySelector(".title-row")?.contains(slot)).toBe(true);

    card.remove();
  });

  test("every shape's geometry is pinned at its source (happy-dom renders no layout)", () => {
    // The shape hooks the sheet keys on.
    for (const shape of ["grid", "hero", "row", "detail"]) {
      expect(cardStyles).toContain(`:host([shape="${shape}"])`);
    }
    // The details marker's own metrics — a missing rule would leave the
    // ❯❯ at inherited size and color.
    expect(cardStyles).toMatch(
      /\.more \{\s*flex-shrink: 0;\s*display: inline-flex;/,
    );
    // The row's two geometries and the narrow bands. Each media block is
    // extracted whole, then matched with a bounded `[^}]*` inside one rule:
    // an unbounded scan could reach into a later rule and pass on it.
    expect(cardStyles).toMatch(
      /:host\(\[shape="row"\]\) \.frame \{\s*display: flex;/,
    );
    expect(cardStyles).toContain(':host([shape="row"][dense]) .frame');
    const compactBand =
      cardStyles.match(/@media \(max-width: 700px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(compactBand).toMatch(
      /:host\(\[shape="row"\]\[dense\]\) \.thumb \{[^}]*\}/,
    );
    const phoneBand =
      cardStyles.match(/@media \(max-width: 480px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(phoneBand).toMatch(
      /:host\(\[shape="row"\]\) \.frame \{[^}]*flex-direction: column;[^}]*\}/,
    );
    // The hero stacks at the tablet threshold.
    const tabletBand =
      cardStyles.match(/@media \(max-width: 1150px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(tabletBand).toMatch(
      /:host\(\[shape="hero"\]\) \.frame \{[^}]*grid-template-columns: 1fr;/,
    );
    // The grid fills its cell (equal-height cards, bottom-pinned footers).
    expect(cardStyles).toMatch(/:host\(\[shape="grid"\]\) \{\s*height: 100%;/);
    // The description's quote treatment is the cards': only grid and hero
    // italicise it.
    expect(cardStyles).toMatch(
      /:host\(\[shape="grid"\]\) \.description,\s*:host\(\[shape="hero"\]\) \.description \{\s*font-style: italic;/,
    );
    // The detail description preserves the data's line breaks in its text
    // span (happy-dom renders no layout, so the rule is pinned here).
    expect(cardStyles).toMatch(
      /:host\(\[shape="detail"\]\) \.description \.description-text \{\s*white-space: pre-line;/,
    );
    // The overlay slot owns the frame's corner; the actions slot is a
    // pass-through so its controls join the title row's flex line.
    expect(cardStyles).toContain(
      ':host([shape="row"]) slot[name="overlay"]::slotted(*)',
    );
    expect(cardStyles).toMatch(
      /:host\(\[shape="detail"\]\) slot\[name="actions"\] \{\s*display: contents;/,
    );
    // The corner reservation follows the overlay's presence, and both
    // action doors are pass-throughs so their controls join the layout.
    expect(cardStyles).toContain(':host([shape="row"][data-overlay]) .body');
    expect(cardStyles).toMatch(
      /:host\(\[shape="row"\]\) slot\[name="actions"\] \{\s*display: contents;/,
    );
  });
});
