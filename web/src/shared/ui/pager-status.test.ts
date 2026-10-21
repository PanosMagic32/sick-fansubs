import { describe, expect, test } from "bun:test";

import { pagerPageOf } from "@shared/catalog/el.js";

import "./pager-status.js";
import type { PagerStatus } from "./pager-status.js";

function root(el: HTMLElement): HTMLElement {
  return el.shadowRoot! as unknown as HTMLElement;
}

async function render(props: Partial<PagerStatus>): Promise<PagerStatus> {
  const el = document.createElement("pager-status");
  Object.assign(el, props);
  document.body.appendChild(el);
  await el.updateComplete;
  return el;
}

describe("pager-status", () => {
  test("carries the page sentence in a visually hidden live region", async () => {
    const status = await render({ page: 2, total: 23, limit: 10 });

    const region = root(status).querySelector('[role="status"]');
    expect(region).not.toBeNull();
    expect(region?.classList.contains("sr-only")).toBe(true);
    expect(region?.textContent?.trim()).toBe(pagerPageOf(2, 3));

    status.remove();
  });

  test("stays mounted and empties while a load resets the total", async () => {
    const status = await render({ page: 2, total: 23, limit: 10 });
    const region = root(status).querySelector('[role="status"]');

    // The host resets total to 0 when a page turn starts; the SAME region
    // must survive and go quiet, then announce when the new total lands.
    status.total = 0;
    await status.updateComplete;
    expect(root(status).querySelector('[role="status"]')).toBe(region);
    expect(region?.textContent?.trim()).toBe("");

    status.total = 44;
    status.page = 3;
    await status.updateComplete;
    expect(root(status).querySelector('[role="status"]')).toBe(region);
    expect(region?.textContent?.trim()).toBe(pagerPageOf(3, 5));

    status.remove();
  });
});
