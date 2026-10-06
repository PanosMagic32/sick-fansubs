import { describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";

import "./loading-spinner.js";

describe("loading-spinner", () => {
  test("renders the catalog loading text with a status live region", async () => {
    const spinner = document.createElement("loading-spinner");
    document.body.appendChild(spinner);
    await spinner.updateComplete;

    const loading = spinner.shadowRoot?.querySelector(".loading");
    expect(loading?.textContent?.trim()).toBe(el.ui.loading);
    expect(loading?.getAttribute("role")).toBe("status");

    spinner.remove();
  });
});
