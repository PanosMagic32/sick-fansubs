/**
 * Likers modal tests — the shared who-liked dialog: page 1 on open, cursor
 * append, empty/error/retry states, and the close contract. The loader is
 * the host's stub function (the element never touches the wire itself).
 */

import { afterEach, describe, expect, test } from "bun:test";

import { readSheet } from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

// Side-effect import registers the custom element.
import "./likers-modal.js";
import type {
  LikerPage,
  LikerPageLoader,
  LikersModal,
} from "./likers-modal.js";

// The stylesheet is read from disk rather than through the `?inline`
// import (the shared CSS-pin mechanism).
const modalStyles = await readSheet(
  new URL("./likers-modal.css", import.meta.url),
);

function root(modal: LikersModal): HTMLElement {
  return modal.shadowRoot! as unknown as HTMLElement;
}

function page(
  items: Array<{ id: string; username: string; avatarUrl: string | null }>,
  hasNextPage = false,
  endCursor: string | null = null,
): LikerPage {
  return { items, pageInfo: { hasNextPage, endCursor } };
}

async function render(): Promise<LikersModal> {
  const modal = document.createElement("likers-modal") as LikersModal;
  modal.heading = el.likers.title;
  document.body.appendChild(modal);
  await modal.updateComplete;
  return modal;
}

/** Flush the loader microtask chain and every dependent render. */
async function settle(modal: LikersModal): Promise<void> {
  for (let i = 0; i < 4; i += 1) {
    await new Promise((r) => setTimeout(r, 0));
    await modal.updateComplete;
  }
}

function dialog(modal: LikersModal): HTMLDialogElement {
  return root(modal).querySelector("dialog") as HTMLDialogElement;
}

function names(modal: LikersModal): string[] {
  return [...root(modal).querySelectorAll(".likers-name")].map(
    (n) => n.textContent?.trim() ?? "",
  );
}

/** The nearest common inclusive ancestor — where a real click lands after a
 * mousedown and mouseup at the two ends. */
function commonAncestor(a: Element, b: Element): Element {
  let node: Element | null = a;
  while (node && !node.contains(b)) node = node.parentElement;
  return node ?? document.documentElement;
}

/** One pointer press: mousedown and mouseup at the given elements, then the
 * click at its real target (the two ends' common ancestor). */
async function press(
  modal: LikersModal,
  down: Element,
  up: Element = down,
): Promise<void> {
  down.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
  up.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
  commonAncestor(down, up).dispatchEvent(
    new MouseEvent("click", { bubbles: true }),
  );
  await settle(modal);
}

/** Render the modal, wire the loader and a dismissal collector, and open it
 * — the shared arrange for the open-driven tests. */
async function openModal(loader?: LikerPageLoader): Promise<{
  modal: LikersModal;
  closes: string[];
}> {
  const modal = await render();
  modal.loader = loader;
  const closes: string[] = [];
  modal.addEventListener("sf-likers-close", () => closes.push("close"));
  modal.open = true;
  await settle(modal);
  return { modal, closes };
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("likers-modal", () => {
  test("the dialog panel is opaque", () => {
    // happy-dom resolves no custom properties and does no layout, so the
    // fill is pinned against the CSS source.
    const panel = modalStyles.match(/\.likers-dialog \{([^}]*)\}/s)?.[1] ?? "";
    expect(panel).toContain("background: var(--sf-bg)");
    expect(panel).not.toContain("var(--sf-surface)");
    expect(panel).not.toContain("backdrop-filter");
  });

  test("a closed dialog stays hidden", () => {
    // The panel's `display: flex` beats the UA sheet's closed-dialog
    // hiding, so the sheet must restore it; happy-dom applies no UA sheet
    // and does no layout, so this is a CSS-source pin.
    const closed = modalStyles.match(
      /\.likers-dialog:not\(\[open\]\) \{([^}]*)\}/s,
    )?.[1];
    expect(closed).toContain("display: none");
  });

  test("loads page 1 on open and renders the users", async () => {
    const calls: Array<string | null> = [];
    const { modal } = await openModal(async (after) => {
      calls.push(after);
      return page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    });

    expect(calls).toEqual([null]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
    expect(names(modal)).toEqual(["Perospero"]);
    expect(
      root(modal).querySelector(".likers-title")?.textContent?.trim(),
    ).toBe(el.likers.title);
  });

  test("load more appends the next page through the end cursor", async () => {
    const calls: Array<string | null> = [];
    const { modal } = await openModal(async (after) => {
      calls.push(after);
      if (after === null) {
        return page(
          [{ id: "u2", username: "A", avatarUrl: null }],
          true,
          "cur1",
        );
      }
      return page([{ id: "u3", username: "B", avatarUrl: null }]);
    });

    const more = root(modal).querySelector(".likers-more") as HTMLButtonElement;
    expect(more).not.toBeNull();
    more.click();
    await settle(modal);

    expect(calls).toEqual([null, "cur1"]);
    expect(names(modal)).toEqual(["A", "B"]);
    expect(root(modal).querySelector(".likers-more")).toBeNull();
  });

  test("an empty list renders the empty state and no more button", async () => {
    const { modal } = await openModal(async () => page([]));

    expect(
      root(modal).querySelector(".likers-empty")?.textContent?.trim(),
    ).toBe(el.likers.empty);
    expect(root(modal).querySelector(".likers-more")).toBeNull();
  });

  test("a failed load renders the error banner and retry reloads", async () => {
    let fail = true;
    const { modal } = await openModal(async () => {
      if (fail) throw new Error("boom");
      return page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    });

    expect(root(modal).querySelector(".likers-retry")).not.toBeNull();

    fail = false;
    (root(modal).querySelector(".likers-retry") as HTMLButtonElement).click();
    await settle(modal);
    expect(names(modal)).toEqual(["Perospero"]);
  });

  test("closing informs the host, and open=false closes the dialog", async () => {
    const { modal, closes } = await openModal(async () =>
      page([{ id: "u2", username: "Perospero", avatarUrl: null }]),
    );
    // The host flips `open` down on the dismissal, as a real host does.
    modal.addEventListener("sf-likers-close", () => {
      modal.open = false;
    });

    expect(dialog(modal).hasAttribute("open")).toBe(true);

    (root(modal).querySelector(".likers-close") as HTMLButtonElement).click();
    await settle(modal);

    expect(closes).toEqual(["close"]);
    expect(dialog(modal).hasAttribute("open")).toBe(false);
  });

  test("a re-open refetches page 1 (likes are live data)", async () => {
    const calls: Array<string | null> = [];
    const { modal } = await openModal(async (after) => {
      calls.push(after);
      return page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    });

    modal.open = false;
    await settle(modal);
    modal.open = true;
    await settle(modal);

    expect(calls).toEqual([null, null]);
  });

  test("closing while a page is in flight drops the stale response", async () => {
    let release!: (p: LikerPage) => void;
    const modal = await render();
    modal.loader = () =>
      new Promise<LikerPage>((resolve) => {
        release = resolve;
      });

    modal.open = true;
    await modal.updateComplete;
    modal.open = false;
    await settle(modal);
    release(page([{ id: "u2", username: "Perospero", avatarUrl: null }]));
    await settle(modal);

    expect(names(modal)).toEqual([]);
    expect(root(modal).querySelector(".likers-empty")).toBeNull();
  });

  test("a failed load-more keeps the list and shows the inline error", async () => {
    const { modal } = await openModal(async (after) => {
      if (after === null) {
        return page(
          [{ id: "u2", username: "A", avatarUrl: null }],
          true,
          "cur1",
        );
      }
      throw new Error("boom");
    });

    (root(modal).querySelector(".likers-more") as HTMLButtonElement).click();
    await settle(modal);

    expect(names(modal)).toEqual(["A"]);
    expect(root(modal).querySelector(".likers-more-error")).not.toBeNull();
    expect(root(modal).querySelector(".likers-more")).not.toBeNull();
  });

  test("the native close informs the host (the Escape path)", async () => {
    const { modal, closes } = await openModal(async () => page([]));

    dialog(modal).dispatchEvent(new Event("close"));

    expect(closes).toEqual(["close"]);
  });

  test("a backdrop click closes the dialog", async () => {
    const { modal, closes } = await openModal(async () => page([]));
    // The host flips `open` down on the dismissal, as a real host does.
    modal.addEventListener("sf-likers-close", () => {
      modal.open = false;
    });

    // The dialog element itself is the backdrop: a full press on it closes.
    await press(modal, dialog(modal));

    expect(closes).toEqual(["close"]);
    expect(dialog(modal).hasAttribute("open")).toBe(false);
  });

  test("a click inside the panel does not close", async () => {
    const { modal, closes } = await openModal(async () =>
      page([{ id: "u2", username: "Perospero", avatarUrl: null }]),
    );

    await press(
      modal,
      root(modal).querySelector(".likers-head") as HTMLElement,
    );

    expect(closes).toEqual([]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
  });

  test("a drag from the panel onto the backdrop does not close", async () => {
    const { modal, closes } = await openModal(async () =>
      page([{ id: "u2", username: "Perospero", avatarUrl: null }]),
    );

    // Selection drag that ends outside the panel: the click targets the
    // dialog (the two ends' common ancestor) but the press began inside.
    await press(
      modal,
      root(modal).querySelector(".likers-name") as HTMLElement,
      dialog(modal),
    );

    expect(closes).toEqual([]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
  });

  test("a drag from the backdrop onto the panel does not close", async () => {
    const { modal, closes } = await openModal(async () =>
      page([{ id: "u2", username: "Perospero", avatarUrl: null }]),
    );

    // The mirror drag: press on the backdrop, release on the text.
    await press(
      modal,
      dialog(modal),
      root(modal).querySelector(".likers-name") as HTMLElement,
    );

    expect(closes).toEqual([]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
  });

  test("a non-primary backdrop press does not close", async () => {
    const { modal, closes } = await openModal(async () => page([]));

    // A context-menu press: only a mousedown on the PRIMARY button arms the
    // dismissal, so the button-2 sequence never counts.
    const dlg = dialog(modal);
    dlg.dispatchEvent(
      new MouseEvent("mousedown", { bubbles: true, button: 2 }),
    );
    dlg.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, button: 2 }));
    dlg.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await settle(modal);

    expect(closes).toEqual([]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
  });

  test("a synthetic click on panel content never counts as the backdrop", async () => {
    const { modal, closes } = await openModal(async () => page([]));

    // Synthetic-only shape: no real pointer sequence targets a child after
    // both press ends landed on the backdrop. The guard covers dispatchers
    // and assistive-tech clicks that retarget.
    const dlg = dialog(modal);
    dlg.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    dlg.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
    (root(modal).querySelector(".likers-head") as HTMLElement).dispatchEvent(
      new MouseEvent("click", { bubbles: true }),
    );
    await settle(modal);

    expect(closes).toEqual([]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
  });

  test("a press that never completes does not linger into the next open", async () => {
    const { modal, closes } = await openModal(async () => page([]));

    // A backdrop press whose release never arrived (the pointer left the
    // window), then the host closes and reopens the modal.
    dialog(modal).dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    modal.open = false;
    await settle(modal);
    modal.open = true;
    await settle(modal);

    // A COMPLETE backdrop gesture on the fresh modal must not inherit the
    // stale press: the transient state drops on close/open.
    const dlg = dialog(modal);
    dlg.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
    dlg.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await settle(modal);

    expect(closes).toEqual([]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
  });

  test("a missing loader surfaces as the error state, not an eternal spinner", async () => {
    const { modal } = await openModal();

    expect(root(modal).querySelector(".likers-retry")).not.toBeNull();
    expect(root(modal).querySelector("loading-spinner")).toBeNull();
  });
});
