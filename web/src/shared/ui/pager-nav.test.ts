import { describe, expect, test } from "bun:test";

import {
  stripCssComments,
  stripSourceComments,
} from "@shared/api/test-utils.js";
import { el, pagerPageOf } from "@shared/catalog/el.js";

// Side-effect import registers the custom element.
import "./pager-nav.js";
import type { PagerNav } from "./pager-nav.js";

const pagerStyles = stripCssComments(
  await Bun.file(new URL("./pager-nav.css", import.meta.url)).text(),
);

function root(el: HTMLElement): HTMLElement {
  return el.shadowRoot! as unknown as HTMLElement;
}

function text(el: HTMLElement, selector: string): string {
  return (el.querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

async function render(props: Partial<PagerNav>): Promise<PagerNav> {
  const el = document.createElement("pager-nav");
  Object.assign(el, props);
  document.body.appendChild(el);
  await el.updateComplete;
  return el;
}

function buttons(pager: PagerNav): HTMLButtonElement[] {
  return [...root(pager).querySelectorAll("button")] as HTMLButtonElement[];
}

describe("pager-nav", () => {
  test("renders both buttons always — disabled at the boundaries", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: true,
      limits: [10, 20, 50],
      limit: 10,
    });

    const [prev, next] = buttons(pager);
    // First page: Previous disabled (no marker in this session), Next live.
    expect(prev?.disabled).toBe(true);
    expect(next?.disabled).toBe(false);
    // Arrow-only chips: the visible row carries the
    // glyph, the accessible name carries the catalog words.
    expect(text(root(pager), "button:nth-of-type(1)")).toBe("←");
    expect(text(root(pager), "button:nth-of-type(2)")).toBe("→");
    expect(prev?.getAttribute("aria-label")).toBe(el.ui.prevPage);
    expect(next?.getAttribute("aria-label")).toBe(el.ui.nextPage);
    // Stable geometry: both buttons exist in every state.
    expect(buttons(pager).length).toBe(2);
    pager.remove();
  });

  test("the page-turn chips are compact and never show the words", async () => {
    const pager = await render({
      hasPrevious: true,
      hasNext: true,
      limits: [10, 20, 50],
      limit: 10,
    });

    const [prev, next] = buttons(pager);
    for (const button of [prev!, next!]) {
      expect(button.classList.contains("chip-arrow")).toBe(true);
      // The catalog words never reach the visible row — a reword that
      // re-adds them would double the bar's width and fail here.
      expect(button.textContent).not.toContain(el.ui.prevPage);
      expect(button.textContent).not.toContain(el.ui.nextPage);
      // …and the glyph is decorative: the label is the whole name.
      expect(button.querySelector("span")?.getAttribute("aria-hidden")).toBe(
        "true",
      );
    }
    pager.remove();
  });

  test("last page disables Next; a paged session enables Previous", async () => {
    const pager = await render({
      hasPrevious: true,
      hasNext: false,
      limits: [10, 20, 50],
      limit: 10,
    });

    const [prev, next] = buttons(pager);
    expect(prev?.disabled).toBe(false);
    expect(next?.disabled).toBe(true);
    pager.remove();
  });

  test("clicking a live button dispatches the matching event", async () => {
    const pager = await render({
      hasPrevious: true,
      hasNext: true,
      limits: [10, 20, 50],
      limit: 10,
    });

    const events: string[] = [];
    pager.addEventListener("sf-pager-prev", () => events.push("prev"));
    pager.addEventListener("sf-pager-next", () => events.push("next"));
    const [prev, next] = buttons(pager);
    prev!.dispatchEvent(new Event("click", { bubbles: true }));
    next!.dispatchEvent(new Event("click", { bubbles: true }));
    expect(events).toEqual(["prev", "next"]);
    pager.remove();
  });

  test("the size chip renders the catalog label, the visible value, and every offered size", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: false,
      limits: [10, 20, 50],
      limit: 20,
    });

    const label = root(pager).querySelector(".size-chip");
    expect(label).not.toBeNull();
    expect(text(root(pager), ".size-chip")).toContain(el.ui.pageSizeLabel);
    // The visible value mirrors the active size (the select itself is
    // invisible — see the CSS contract below).
    expect(text(root(pager), ".size-value")).toBe("20");

    const options = [...root(pager).querySelectorAll("option")];
    expect(options.map((o) => o.value)).toEqual(["10", "20", "50"]);
    // The active size is selected.
    expect(
      (root(pager).querySelector("select") as HTMLSelectElement).value,
    ).toBe("20");
    pager.remove();
  });

  test("the compact geometry is pinned in the stylesheet", async () => {
    // happy-dom cannot measure layout, so the compaction is pinned against
    // the CSS source: the tighter row gap, the size chip's own dashed base,
    // and the arrows as the shared secondary icon control.
    // [^}]* keeps each match inside its OWN block: a [\s\S]*? scan would
    // happily reach into a later rule (the size chip also carries gap: 8px),
    // so a reverted .pager gap would still satisfy the pin.
    expect(pagerStyles).toMatch(/\.pager \{[^}]*gap: 8px;/);
    expect(pagerStyles).toMatch(/\.chip-arrow \{[^}]*justify-content: center;/);
    expect(pagerStyles).toMatch(/\.size-chip \{[^}]*display: inline-flex;/);
    expect(pagerStyles).toMatch(/\.size-chip \{[^}]*border-style: dashed;/);
    const source = stripSourceComments(
      await Bun.file(new URL("./pager-nav.ts", import.meta.url)).text(),
    );
    expect(source).toContain(
      'class="button button--secondary button--icon chip-arrow"',
    );
  });

  test("the pager row never squeezes its size chip", async () => {
    // happy-dom cannot measure layout: pinned against the CSS source. The row
    // wraps instead of squeezing the chip, the chip's own label never breaks
    // inside its border (the wrapped label is what made the pager look
    // broken), and at phone width the label steps back to the accessible name
    // (the visually-hidden recipe — `clip`, never `display: none`, so the
    // select keeps its name and the value+caret stay visible).
    expect(pagerStyles).toMatch(/\.pager \{[^}]*flex-wrap: wrap;/);
    expect(pagerStyles).toMatch(/\.size-chip \{[^}]*white-space: nowrap;/);
    const narrow =
      pagerStyles.match(/@media \(max-width: 480px\) \{(.*?)\n\}/s)?.[1] ?? "";
    expect(narrow).toMatch(/\.size-label \{[^}]*clip: rect\(0, 0, 0, 0\);/);
    expect(narrow).not.toContain("display: none");

    // The component owns the class that query targets, and the label stays in
    // the DOM (the select's accessible name comes from it).
    const pager = await render({
      hasPrevious: false,
      hasNext: true,
      limits: [10, 20, 50],
      limit: 10,
    });
    const label = root(pager).querySelector(".size-label");
    expect(label?.textContent?.trim()).toBe(el.ui.pageSizeLabel);
    pager.remove();
  });

  test("the whole chip is the select hit target (CSS contract)", () => {
    // happy-dom cannot simulate the native dropdown opening — pin the
    // source: the select stretches invisibly over the chip, so any click
    // anywhere on the chip lands on the select.
    expect(pagerStyles).toMatch(/\.size-chip \{[^}]*position: relative;/);
    expect(pagerStyles).toMatch(
      /\.size-select \{[^}]*position: absolute;[^}]*inset: 0;/,
    );
    expect(pagerStyles).toMatch(/\.size-select \{[^}]*opacity: 0;/);
  });

  test("changing the size dispatches sf-limit-change with the number", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: false,
      limits: [10, 20, 50],
      limit: 10,
    });

    const sizes: number[] = [];
    pager.addEventListener("sf-limit-change", (e) =>
      sizes.push((e as CustomEvent<number>).detail),
    );

    const select = root(pager).querySelector("select") as HTMLSelectElement;
    select.value = "50";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    expect(sizes).toEqual([50]);
    pager.remove();
  });

  test("a valid-but-unoffered limit renders as an extra option", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: false,
      limits: [10, 20, 50],
      limit: 7,
    });

    const options = [...root(pager).querySelectorAll("option")];
    expect(options.map((o) => o.value)).toEqual(["10", "20", "50", "7"]);
    expect(
      (root(pager).querySelector("select") as HTMLSelectElement).value,
    ).toBe("7");
    pager.remove();
  });

  test("the nav landmark carries the catalog aria-label", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: false,
      limits: [10, 20, 50],
      limit: 10,
    });
    expect(root(pager).querySelector("nav")?.getAttribute("aria-label")).toBe(
      el.ui.pagerLabel,
    );
    pager.remove();
  });

  test("renders the position line from page, total, and limit", async () => {
    const pager = await render({
      hasPrevious: true,
      hasNext: true,
      limits: [10, 20, 50],
      limit: 10,
      page: 2,
      total: 23,
    });
    // 23 rows at 10 per page = 3 pages; the URL states which one is shown.
    expect(text(root(pager), ".page-status")).toBe(pagerPageOf(2, 3));
    pager.remove();
  });

  test("the page count floors at one when total is zero", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: false,
      limits: [10, 20, 50],
      limit: 10,
      page: 1,
      total: 0,
    });
    expect(text(root(pager), ".page-status")).toBe(pagerPageOf(1, 1));
    pager.remove();
  });

  test("a valid-but-unoffered limit still derives the page count", async () => {
    const pager = await render({
      hasPrevious: false,
      hasNext: true,
      limits: [10, 20, 50],
      limit: 7,
      page: 2,
      total: 15,
    });
    // 15 rows at the hand-edited size of 7 = 3 pages (the select renders 7
    // as an extra option — see the dedicated test below).
    expect(text(root(pager), ".page-status")).toBe(pagerPageOf(2, 3));
    pager.remove();
  });

  test("the position line sits BELOW the control row, not on it", async () => {
    const pager = await render({
      limits: [10, 20, 50],
      limit: 10,
      page: 1,
      total: 3,
    });
    const rootEl = root(pager);
    const nav = rootEl.querySelector("nav.pager");
    const status = rootEl.querySelector(".page-status");
    expect(status).not.toBeNull();
    // A caption, not a row child.
    expect(nav?.contains(status as Node)).toBe(false);
    expect(
      nav!.compareDocumentPosition(status as Node) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    pager.remove();
  });

  test("the position line stays muted and unbreakable (CSS contract)", () => {
    // happy-dom cannot measure text wrapping: pinned against the CSS source.
    expect(pagerStyles).toMatch(
      /\.page-status \{[^}]*color: var\(--sf-text-muted\);/,
    );
    expect(pagerStyles).toMatch(/\.page-status \{[^}]*white-space: nowrap;/);
    // The status is a caption under the row: the wrapper stacks it.
    expect(pagerStyles).toMatch(/\.pager-wrap \{[^}]*flex-direction: column;/);
  });
});
