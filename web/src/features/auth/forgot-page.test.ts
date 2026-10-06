import { describe, expect, test, afterEach, mock } from "bun:test";

import { el, retryAfterMessage } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  mockSessionContext,
  restoreMockFetch,
  waitFor,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./forgot-page.js";
import type { ForgotPasswordPage } from "./forgot-page.js";

/** Minimal session context for deterministic test control. */
function mockSession(
  overrides: {
    status?: "initializing" | "anonymous" | "authenticated" | "error";
    message?: string;
    retryBootstrap?: () => void | Promise<void>;
  } = {},
) {
  return mockSessionContext({ status: "anonymous", ...overrides });
}

async function render(
  status: "initializing" | "anonymous" | "authenticated" = "anonymous",
) {
  const page = document.createElement(
    "forgot-password-page",
  ) as ForgotPasswordPage;
  (page as any).session = mockSession({ status });
  document.body.appendChild(page);
  await page.updateComplete;
  return page;
}

function teardown() {
  document.body.innerHTML = "";
  restoreMockFetch();
}

/** A fetch double that fails the test if it is ever called. */
function failOnFetch() {
  const spy = mock((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.reject(new Error("unexpected fetch")),
  );
  mockFetchImpl(spy);
  return spy;
}

// No window.location mock here: the page only reads location for the
// authenticated redirect, which no test in this file triggers.
afterEach(() => {
  teardown();
});

describe("forgot-password page", () => {
  test("shows the form when anonymous with catalog copy", async () => {
    const page = await render("anonymous");

    expect(page.shadowRoot?.querySelector("h1")?.textContent).toBe(
      el.auth.forgotTitle,
    );
    const label = page.shadowRoot?.querySelector(
      'label[for="forgot-identifier"]',
    );
    expect(label?.textContent).toBe(el.auth.identifierLabel);
    expect(
      page.shadowRoot
        ?.querySelector("button[type='submit']")
        ?.textContent?.trim(),
    ).toBe(el.auth.forgotButton);
  });

  test("shows spinner while initializing or authenticated", async () => {
    for (const status of ["initializing", "authenticated"] as const) {
      const page = await render(status);
      expect(page.shadowRoot?.querySelector("loading-spinner")).not.toBeNull();
      // The spinner branch keeps the route's heading landmark — the only h1
      // in the branch, so the page never renders headingless while it waits.
      const headings = [...(page.shadowRoot?.querySelectorAll("h1") ?? [])];
      expect(headings).toHaveLength(1);
      expect(headings[0]?.classList.contains("sr-only")).toBe(true);
      expect(headings[0]?.textContent?.trim()).toBe(el.auth.forgotTitle);
      teardown();
    }
  });

  test("shows required error on empty submit without calling the API", async () => {
    const fetchSpy = failOnFetch();
    const page = await render("anonymous");

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector("#forgot-identifier-error")
        ?.textContent?.trim(),
    ).toBe(el.violations.required);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test("successful submit shows the GENERIC success copy", async () => {
    mockFetchOnce({ ok: true, status: 200, jsonBody: {} });
    const page = await render("anonymous");

    const input = page.shadowRoot?.querySelector(
      "#forgot-identifier",
    ) as HTMLInputElement;
    input.value = "someone";
    input.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await waitFor(() => !!page.shadowRoot?.querySelector(".success-note"));

    expect(
      page.shadowRoot?.querySelector(".success-note")?.textContent?.trim(),
    ).toBe(el.auth.forgotSuccess);
    expect(page.shadowRoot?.querySelector("form")).toBeNull();
    // The note is a focusable live region — focus moves to it (the
    // account-page success pattern), so the result is announced.
    expect(
      page.shadowRoot?.querySelector(".success-note")?.getAttribute("role"),
    ).toBe("status");
    await waitFor(
      () =>
        !!page.shadowRoot?.activeElement?.classList.contains("success-note"),
    );
    // The success state links back to sign-in.
    expect(
      page.shadowRoot?.querySelector(".footer-link a")?.getAttribute("href"),
    ).toBe("/sign-in");
  });

  test("token-invalid-style problems surface through the generic mapper", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/reset-token-invalid",
        title: "Invalid or expired reset token",
        status: 422,
      },
    });
    const page = await render("anonymous");

    const input = page.shadowRoot?.querySelector(
      "#forgot-identifier",
    ) as HTMLInputElement;
    input.value = "someone";
    input.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await waitFor(() => !!page.shadowRoot?.querySelector("#forgot-error"));

    const banner = page.shadowRoot?.querySelector("#forgot-error");
    expect(
      banner?.shadowRoot?.querySelector(".error")?.textContent?.trim(),
    ).toBe(el.problems["/problems/auth/reset-token-invalid"]);
  });

  test("a 429 starts the submit cooldown", async () => {
    mockFetchOnce({
      ok: false,
      status: 429,
      headers: {
        "content-type": "application/problem+json",
        "Retry-After": "3",
      },
      jsonBody: {
        type: "/problems/rate-limited",
        title: "Too many requests",
        status: 429,
      },
    });
    const page = await render("anonymous");

    const input = page.shadowRoot?.querySelector(
      "#forgot-identifier",
    ) as HTMLInputElement;
    input.value = "someone";
    input.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));

    await waitFor(() => {
      const button = page.shadowRoot?.querySelector(
        "button[type='submit']",
      ) as HTMLButtonElement | null;
      return !!button && button.disabled;
    });
    const button = page.shadowRoot?.querySelector(
      "button[type='submit']",
    ) as HTMLButtonElement;
    expect(button.textContent?.trim()).toBe(retryAfterMessage(3));
  });
});
