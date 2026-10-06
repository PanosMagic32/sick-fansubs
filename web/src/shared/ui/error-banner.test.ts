import { describe, expect, test } from "bun:test";
import { el } from "@shared/catalog/el.js";

import "./error-banner.js";

describe("error-banner", () => {
  test("renders the message with an alert live region", async () => {
    const banner = document.createElement("error-banner");
    banner.message = el.ui.error;
    document.body.appendChild(banner);
    await banner.updateComplete;

    const error = banner.shadowRoot?.querySelector(".error");
    expect(error?.textContent?.trim()).toBe(el.ui.error);
    expect(error?.getAttribute("role")).toBe("alert");

    banner.remove();
  });

  test("updates when the message property changes", async () => {
    const banner = document.createElement("error-banner");
    banner.message = "first";
    document.body.appendChild(banner);
    await banner.updateComplete;

    banner.message = "second";
    await banner.updateComplete;

    expect(banner.shadowRoot?.querySelector(".error")?.textContent).toBe(
      "second",
    );

    banner.remove();
  });
});
