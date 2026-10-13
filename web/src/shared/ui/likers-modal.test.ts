/**
 * Likers modal tests — the shared who-liked dialog: page 1 on open, cursor
 * append, empty/error/retry states, and the close contract. The loader is
 * the host's stub function (the element never touches the wire itself).
 */

import { afterEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";

// Side-effect import registers the custom element.
import "./likers-modal.js";
import type { LikerPage, LikersModal } from "./likers-modal.js";

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
  modal.heading = el.likers.heading;
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
  return [...root(modal).querySelectorAll(".liker-name")].map(
    (n) => n.textContent?.trim() ?? "",
  );
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("likers-modal", () => {
  test("loads page 1 on open and renders the users", async () => {
    const calls: Array<string | null> = [];
    const modal = await render();
    modal.loader = async (after) => {
      calls.push(after);
      return page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    };

    modal.open = true;
    await settle(modal);

    expect(calls).toEqual([null]);
    expect(dialog(modal).hasAttribute("open")).toBe(true);
    expect(names(modal)).toEqual(["Perospero"]);
    expect(
      root(modal).querySelector(".likers-title")?.textContent?.trim(),
    ).toBe(el.likers.heading);
  });

  test("load more appends the next page through the end cursor", async () => {
    const calls: Array<string | null> = [];
    const modal = await render();
    modal.loader = async (after) => {
      calls.push(after);
      if (after === null) {
        return page(
          [{ id: "u2", username: "A", avatarUrl: null }],
          true,
          "cur1",
        );
      }
      return page([{ id: "u3", username: "B", avatarUrl: null }]);
    };

    modal.open = true;
    await settle(modal);

    const more = root(modal).querySelector(".likers-more") as HTMLButtonElement;
    expect(more).not.toBeNull();
    more.click();
    await settle(modal);

    expect(calls).toEqual([null, "cur1"]);
    expect(names(modal)).toEqual(["A", "B"]);
    expect(root(modal).querySelector(".likers-more")).toBeNull();
  });

  test("an empty list renders the empty state and no more button", async () => {
    const modal = await render();
    modal.loader = async () => page([]);

    modal.open = true;
    await settle(modal);

    expect(
      root(modal).querySelector(".likers-empty")?.textContent?.trim(),
    ).toBe(el.likers.empty);
    expect(root(modal).querySelector(".likers-more")).toBeNull();
  });

  test("a failed load renders the error banner and retry reloads", async () => {
    let fail = true;
    const modal = await render();
    modal.loader = async () => {
      if (fail) throw new Error("boom");
      return page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    };

    modal.open = true;
    await settle(modal);
    expect(root(modal).querySelector(".likers-retry")).not.toBeNull();

    fail = false;
    (root(modal).querySelector(".likers-retry") as HTMLButtonElement).click();
    await settle(modal);
    expect(names(modal)).toEqual(["Perospero"]);
  });

  test("closing informs the host, and open=false closes the dialog", async () => {
    const modal = await render();
    modal.loader = async () =>
      page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    const closes: string[] = [];
    modal.addEventListener("sf-likers-close", () => {
      closes.push("close");
      modal.open = false;
    });

    modal.open = true;
    await settle(modal);
    expect(dialog(modal).hasAttribute("open")).toBe(true);

    (root(modal).querySelector(".likers-close") as HTMLButtonElement).click();
    await settle(modal);

    expect(closes).toEqual(["close"]);
    expect(dialog(modal).hasAttribute("open")).toBe(false);
  });

  test("a re-open refetches page 1 (likes are live data)", async () => {
    const calls: Array<string | null> = [];
    const modal = await render();
    modal.loader = async (after) => {
      calls.push(after);
      return page([{ id: "u2", username: "Perospero", avatarUrl: null }]);
    };

    modal.open = true;
    await settle(modal);
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
    const modal = await render();
    modal.loader = async (after) => {
      if (after === null) {
        return page(
          [{ id: "u2", username: "A", avatarUrl: null }],
          true,
          "cur1",
        );
      }
      throw new Error("boom");
    };

    modal.open = true;
    await settle(modal);
    (root(modal).querySelector(".likers-more") as HTMLButtonElement).click();
    await settle(modal);

    expect(names(modal)).toEqual(["A"]);
    expect(root(modal).querySelector(".likers-more-error")).not.toBeNull();
    expect(root(modal).querySelector(".likers-more")).not.toBeNull();
  });

  test("the native close informs the host (the Escape path)", async () => {
    const modal = await render();
    modal.loader = async () => page([]);
    const closes: string[] = [];
    modal.addEventListener("sf-likers-close", () => closes.push("close"));

    modal.open = true;
    await settle(modal);
    dialog(modal).dispatchEvent(new Event("close"));

    expect(closes).toEqual(["close"]);
  });

  test("a missing loader surfaces as the error state, not an eternal spinner", async () => {
    const modal = await render();
    modal.open = true;
    await settle(modal);

    expect(root(modal).querySelector(".likers-retry")).not.toBeNull();
    expect(root(modal).querySelector("loading-spinner")).toBeNull();
  });
});
