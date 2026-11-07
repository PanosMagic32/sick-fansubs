import { describe, expect, test } from "bun:test";
import { el } from "@shared/catalog/el.js";

import "./error-banner.js";
import type { ErrorBanner } from "./error-banner.js";

describe("error-banner", () => {
  test("renders the message with an alert live region", async () => {
    const banner = document.createElement("error-banner");
    banner.message = el.ui.error;
    document.body.appendChild(banner);
    await banner.updateComplete;

    const error = banner.shadowRoot?.querySelector(".error");
    expect(error?.textContent?.trim()).toBe(el.ui.error);
    expect(error?.getAttribute("role")).toBe("alert");
    // No retry control unless the host asks for one.
    expect(banner.shadowRoot?.querySelector(".banner-retry")).toBeNull();

    banner.remove();
  });

  test("dispatching retry is the host's recovery path", async () => {
    const banner = document.createElement("error-banner") as ErrorBanner;
    banner.message = el.ui.error;
    banner.retry = true;
    document.body.appendChild(banner);
    await banner.updateComplete;

    let retries = 0;
    banner.addEventListener("retry", () => {
      retries++;
    });

    const button = banner.shadowRoot?.querySelector(
      ".banner-retry",
    ) as HTMLButtonElement;
    expect(button?.textContent?.trim()).toBe(el.ui.retry);
    button.click();

    expect(retries).toBe(1);
    banner.remove();
  });

  test("a retry press moves focus to the declared target before dispatching", async () => {
    const target = document.createElement("h1");
    target.id = "retry-target";
    target.tabIndex = -1;
    document.body.appendChild(target);

    const banner = document.createElement("error-banner") as ErrorBanner;
    banner.message = el.ui.error;
    banner.retry = true;
    banner.retryFocus = "#retry-target";
    document.body.appendChild(banner);
    await banner.updateComplete;

    const button = banner.shadowRoot?.querySelector(
      ".banner-retry",
    ) as HTMLButtonElement;
    button.click();

    expect(document.activeElement).toBe(target);

    // Do not leave a detached element as document.activeElement: happy-dom's
    // shadow-root activeElement traversal throws on the next file's page.
    target.blur();
    banner.remove();
    target.remove();
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
