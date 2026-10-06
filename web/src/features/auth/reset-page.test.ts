import {
  describe,
  expect,
  test,
  beforeEach,
  afterEach,
  mock,
  spyOn,
} from "bun:test";

import { el, retryAfterMessage } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  mockSessionContext,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  waitFor,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./reset-page.js";
import {
  RESET_SUCCESS_STORAGE_KEY,
  type ResetPasswordPage,
} from "./reset-page.js";

/** Minimal session context with a revalidate spy. */
function mockSession() {
  return mockSessionContext({ status: "anonymous" });
}

/**
 * Render the page under a URL. The page reads ?token= in
 * connectedCallback, so the URL must be set BEFORE mounting.
 *
 * happy-dom's history API does not update window.location (production
 * browsers do) — the shared shim routes pushState/replaceState through
 * setURL, so both the page's token read AND its token-strip work. The
 * install/restore pairing is the shared-window gotcha rule.
 */
async function renderWithToken(token: string | null) {
  const url = token ? `/auth/reset?token=${token}` : "/auth/reset";
  // The shim resolves relative URLs against window.location — about:blank
  // (the pristine happy-dom default) is not a valid base, so the test URL
  // must start from a real origin.
  setHappyDOMURL("http://localhost:3000/");
  window.history.pushState({}, "", url);
  const page = document.createElement(
    "reset-password-page",
  ) as ResetPasswordPage;
  (page as any).session = mockSession();
  document.body.appendChild(page);
  await page.updateComplete;
  return page;
}

function teardown() {
  document.body.innerHTML = "";
  restoreMockFetch();
  sessionStorage.removeItem(RESET_SUCCESS_STORAGE_KEY);
}

beforeEach(() => {
  shimHistoryLocationSync();
});

/** A fetch double that fails the test if it is ever called. */
function failOnFetch() {
  const spy = mock((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.reject(new Error("unexpected fetch")),
  );
  mockFetchImpl(spy);
  return spy;
}

afterEach(() => {
  teardown();
  restoreHistoryLocationSync();
  resetHappyDOMURL();
});

describe("reset-password page", () => {
  test("reads the token and strips it from the URL, keeping other params", async () => {
    const replaceSpy = spyOn(history, "replaceState");
    setHappyDOMURL("http://localhost:3000/");
    window.history.pushState({}, "", "/auth/reset?token=tok123&utm=x#frag");
    const page = document.createElement(
      "reset-password-page",
    ) as ResetPasswordPage;
    (page as any).session = mockSession();
    document.body.appendChild(page);
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
    expect(replaceSpy).toHaveBeenCalled();
    // Only the token goes: other params and the hash survive.
    expect(window.location.search).toBe("?utm=x");
    expect(window.location.hash).toBe("#frag");
    replaceSpy.mockRestore();
  });

  test("missing token renders the generic invalid state", async () => {
    const page = await renderWithToken(null);

    expect(page.shadowRoot?.querySelector("form")).toBeNull();
    const note = page.shadowRoot?.querySelector(".invalid-note");
    expect(note?.textContent?.trim()).toBe(el.auth.resetTokenInvalid);
    // The terminal verdict is a focusable live region — the house status
    // shape, so the terminal state is announced.
    expect(note?.getAttribute("role")).toBe("status");
    expect(note?.getAttribute("tabindex")).toBe("-1");
    expect(page.shadowRoot?.activeElement).toBe(note);
    expect(
      page.shadowRoot?.querySelector(".footer-link a")?.getAttribute("href"),
    ).toBe("/auth/forgot");
  });

  test("shows the form with catalog copy", async () => {
    const page = await renderWithToken("tok123");

    expect(page.shadowRoot?.querySelector("h1")?.textContent).toBe(
      el.auth.resetTitle,
    );
    expect(
      page.shadowRoot
        ?.querySelector("button[type='submit']")
        ?.textContent?.trim(),
    ).toBe(el.auth.resetButton);
  });

  test("empty password shows the required error without calling the API", async () => {
    const fetchSpy = failOnFetch();
    const page = await renderWithToken("tok123");

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector("#reset-password-error")
        ?.textContent?.trim(),
    ).toBe(el.violations.required);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test("mismatched confirm blocks the submit with a field error", async () => {
    const fetchSpy = failOnFetch();
    const page = await renderWithToken("tok123");

    const password = page.shadowRoot?.querySelector(
      "#reset-password",
    ) as HTMLInputElement;
    password.value = "new-password-123";
    password.dispatchEvent(new InputEvent("input"));
    const confirm = page.shadowRoot?.querySelector(
      "#reset-confirm",
    ) as HTMLInputElement;
    confirm.value = "different-password";
    confirm.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    // The mismatch copy renders as a FIELD error on the confirm input —
    // and the submit never reaches the wire.
    expect(
      page.shadowRoot
        ?.querySelector("#reset-confirm-error")
        ?.textContent?.trim(),
    ).toBe(el.auth.passwordsDoNotMatch);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test("successful reset stores the banner, revalidates, and redirects", async () => {
    mockFetchOnce({ ok: true, status: 204 });
    const replaceSpy = spyOn(history, "replaceState");
    const page = await renderWithToken("tok123");

    const password = page.shadowRoot?.querySelector(
      "#reset-password",
    ) as HTMLInputElement;
    password.value = "new-password-123";
    password.dispatchEvent(new InputEvent("input"));
    const confirm = page.shadowRoot?.querySelector(
      "#reset-confirm",
    ) as HTMLInputElement;
    confirm.value = "new-password-123";
    confirm.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await waitFor(
      () => sessionStorage.getItem(RESET_SUCCESS_STORAGE_KEY) === "1",
    );

    expect(sessionStorage.getItem(RESET_SUCCESS_STORAGE_KEY)).toBe("1");
    // redirect("/sign-in") calls replaceState — at least one call beyond
    // the token-strip.
    const signInCall = replaceSpy.mock.calls.some(
      (call) => call[2] === "/sign-in",
    );
    expect(signInCall).toBe(true);
    expect((page as any).session.revalidate).toHaveBeenCalled();
    replaceSpy.mockRestore();
  });

  test("token-invalid 422 shows the catalog banner", async () => {
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
    const page = await renderWithToken("stale-token");

    const password = page.shadowRoot?.querySelector(
      "#reset-password",
    ) as HTMLInputElement;
    password.value = "new-password-123";
    password.dispatchEvent(new InputEvent("input"));
    const confirm = page.shadowRoot?.querySelector(
      "#reset-confirm",
    ) as HTMLInputElement;
    confirm.value = "new-password-123";
    confirm.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await waitFor(() => !!page.shadowRoot?.querySelector("#reset-error"));

    const banner = page.shadowRoot?.querySelector("#reset-error");
    expect(
      banner?.shadowRoot?.querySelector(".error")?.textContent?.trim(),
    ).toBe(el.problems["/problems/auth/reset-token-invalid"]);
    expect(sessionStorage.getItem(RESET_SUCCESS_STORAGE_KEY)).toBeNull();
  });

  test("a server 422 focuses the first invalid field", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [
          { field: "password", code: "minLength", message: "too short" },
        ],
      },
    });
    const page = await renderWithToken("tok123");

    const password = page.shadowRoot?.querySelector(
      "#reset-password",
    ) as HTMLInputElement;
    password.value = "short";
    password.dispatchEvent(new InputEvent("input"));
    const confirm = page.shadowRoot?.querySelector(
      "#reset-confirm",
    ) as HTMLInputElement;
    confirm.value = "short";
    confirm.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));

    await waitFor(
      () =>
        (page.shadowRoot?.activeElement as HTMLElement | null)?.id ===
        "reset-password",
    );
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
    const page = await renderWithToken("tok123");

    const password = page.shadowRoot?.querySelector(
      "#reset-password",
    ) as HTMLInputElement;
    password.value = "new-password-123";
    password.dispatchEvent(new InputEvent("input"));
    const confirm = page.shadowRoot?.querySelector(
      "#reset-confirm",
    ) as HTMLInputElement;
    confirm.value = "new-password-123";
    confirm.dispatchEvent(new InputEvent("input"));
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
