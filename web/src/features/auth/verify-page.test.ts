import { describe, expect, test, beforeEach, afterEach, spyOn } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  mockResponse,
  mockSessionContext,
  resetHappyDOMURL,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  restoreHistoryLocationSync,
  waitFor,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./verify-page.js";
import {
  VERIFY_SUCCESS_STORAGE_KEY,
  type VerifyEmailPage,
} from "./verify-page.js";

/** Minimal session context with a revalidate spy. */
function mockSession(authenticated = false) {
  return mockSessionContext({
    status: authenticated ? "authenticated" : "anonymous",
  });
}

/**
 * Render the page under a URL. The page reads ?token= in
 * connectedCallback, so the URL must be set BEFORE mounting (the
 * reset-page shim contract — the token read AND the token strip depend
 * on the real location updating).
 */
async function renderWithToken(token: string | null, authenticated = false) {
  const url = token ? `/auth/verify?token=${token}` : "/auth/verify";
  setHappyDOMURL("http://localhost:3000/");
  window.history.pushState({}, "", url);
  const page = document.createElement("verify-email-page") as VerifyEmailPage;
  (page as any).session = mockSession(authenticated);
  document.body.appendChild(page);
  await page.updateComplete;
  return page;
}

function teardown() {
  document.body.innerHTML = "";
  restoreMockFetch();
  clearCSRFToken();
  sessionStorage.removeItem(VERIFY_SUCCESS_STORAGE_KEY);
}

beforeEach(() => {
  shimHistoryLocationSync();
});

afterEach(() => {
  teardown();
  restoreHistoryLocationSync();
  resetHappyDOMURL();
});

describe("verify-email page", () => {
  test("reads the token and strips the query from the URL", async () => {
    const replaceSpy = spyOn(history, "replaceState");
    const page = await renderWithToken("tok123");

    expect(page.shadowRoot?.querySelector(".intro-note")).not.toBeNull();
    expect(replaceSpy).toHaveBeenCalled();
    expect(window.location.search).toBe("");
    replaceSpy.mockRestore();
  });

  test("missing token renders the generic invalid state", async () => {
    const page = await renderWithToken(null);

    const note = page.shadowRoot?.querySelector(".invalid-note");
    expect(note?.textContent?.trim()).toBe(el.auth.verifyTokenInvalid);
    // The terminal verdict is a focusable live region — the house status
    // shape, so the terminal state is announced.
    expect(note?.getAttribute("role")).toBe("status");
    expect(note?.getAttribute("tabindex")).toBe("-1");
    expect(page.shadowRoot?.activeElement).toBe(note);
    expect(
      page.shadowRoot?.querySelector(".footer-link a")?.getAttribute("href"),
    ).toBe("/sign-in");
  });

  test("auto-submits and redirects an ANONYMOUS user to sign-in with the banner", async () => {
    mockFetchOnce({ ok: true, status: 204 });
    const replaceSpy = spyOn(history, "replaceState");
    const page = await renderWithToken("tok123");

    await waitFor(
      () => sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY) === "1",
    );

    expect(sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY)).toBe("1");
    const signInCall = replaceSpy.mock.calls.some(
      (call) => call[2] === "/sign-in",
    );
    expect(signInCall).toBe(true);
    expect((page as any).session.revalidate).toHaveBeenCalled();
    replaceSpy.mockRestore();
  });

  test("redirects an AUTHENTICATED user to account", async () => {
    mockFetchOnce({ ok: true, status: 204 });
    const replaceSpy = spyOn(history, "replaceState");
    await renderWithToken("tok123", true);

    await waitFor(
      () => sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY) === "1",
    );
    const accountCall = replaceSpy.mock.calls.some(
      (call) => call[2] === "/account",
    );
    expect(accountCall).toBe(true);
    replaceSpy.mockRestore();
  });

  test("token-invalid 422 shows the generic catalog copy", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/verification-token-invalid",
        title: "Invalid or expired verification token",
        status: 422,
      },
    });
    const page = await renderWithToken("stale-token");

    await waitFor(
      () =>
        page.shadowRoot?.querySelector(".invalid-note")?.textContent?.trim() ===
        el.auth.verifyTokenInvalid,
    );
    const note = page.shadowRoot?.querySelector(".invalid-note");
    expect(note?.getAttribute("role")).toBe("status");
    expect(note?.getAttribute("tabindex")).toBe("-1");
    expect(page.shadowRoot?.activeElement).toBe(note);
    expect(sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY)).toBeNull();
  });

  test("waits for session bootstrap on a session-bearing cold load (CSRF header rides)", async () => {
    setCSRFToken("cold-load-csrf");
    const call = mockFetchOnce({ ok: true, status: 204 });
    setHappyDOMURL("http://localhost:3000/");
    window.history.pushState({}, "", "/auth/verify?token=tok123");
    const page = document.createElement("verify-email-page") as VerifyEmailPage;
    const session = mockSession(false);
    (session as { state: unknown }).state = { status: "initializing" };
    (page as any).session = session;
    document.body.appendChild(page);
    await page.updateComplete;

    // Bootstrap unresolved: the verify POST must not go out — a request
    // without X-CSRF-Token is rejected 403 whenever a session cookie
    // rides the navigation, and the token itself is still valid.
    expect(call.req.url).toBe("");

    // Bootstrap resolves authenticated; the context update notifies the
    // consumer and the deferred submit runs with the CSRF header.
    (session as { state: unknown }).state = {
      status: "authenticated",
      user: { id: "u1", username: "u", role: "member" },
      mustChangePassword: false,
    };
    page.requestUpdate();
    await waitFor(
      () => sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY) === "1",
    );

    expect(call.req.method).toBe("POST");
    expect(call.req.headers["x-csrf-token"]).toBe("cold-load-csrf");
  });

  test("a transient failure keeps the token and offers a retry (exactly one re-post)", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return calls === 1
        ? mockResponse({
            ok: false,
            status: 500,
            headers: { "content-type": "application/problem+json" },
            jsonBody: {
              type: "/problems/internal-error",
              title: "Internal server error",
              status: 500,
            },
          })
        : mockResponse({ ok: true, status: 204 });
    });
    const page = await renderWithToken("tok123");

    await waitFor(() => !!page.shadowRoot?.querySelector(".retry-btn"));
    // A 500 is NOT the terminal token verdict — no invalid note.
    expect(page.shadowRoot?.querySelector(".invalid-note")).toBeNull();
    expect(calls).toBe(1);

    // The retry re-posts the token still held in memory — exactly once.
    (page.shadowRoot?.querySelector(".retry-btn") as HTMLButtonElement).click();
    await waitFor(
      () => sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY) === "1",
    );
    expect(calls).toBe(2);
  });

  test("a 403 re-bootstraps the session so the retry can succeed", async () => {
    mockFetchOnce({
      ok: false,
      status: 403,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 403,
      },
    });
    const page = await renderWithToken("tok123");

    await waitFor(() => !!page.shadowRoot?.querySelector(".retry-btn"));
    expect((page as any).session.retryBootstrap).toHaveBeenCalled();
    expect(page.shadowRoot?.querySelector(".invalid-note")).toBeNull();
  });

  test("outside a provider it submits immediately without a CSRF header", async () => {
    const call = mockFetchOnce({ ok: true, status: 204 });
    setHappyDOMURL("http://localhost:3000/");
    window.history.pushState({}, "", "/auth/verify?token=tok123");
    const page = document.createElement("verify-email-page") as VerifyEmailPage;
    document.body.appendChild(page);
    await page.updateComplete;
    await waitFor(
      () => sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY) === "1",
    );

    expect(call.req.method).toBe("POST");
    expect(call.req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("the token strip keeps other query params and the hash", async () => {
    mockFetchOnce({ ok: true, status: 204 });
    setHappyDOMURL("http://localhost:3000/");
    window.history.pushState({}, "", "/auth/verify?token=tok123&utm=x#frag");
    const page = document.createElement("verify-email-page") as VerifyEmailPage;
    (page as any).session = mockSession(false);
    document.body.appendChild(page);
    await page.updateComplete;

    expect(window.location.search).toBe("?utm=x");
    expect(window.location.hash).toBe("#frag");
  });

  test("a 429 renders the retry surface with the cooldown", async () => {
    mockFetchOnce({
      ok: false,
      status: 429,
      headers: {
        "content-type": "application/problem+json",
        "Retry-After": "2",
      },
      jsonBody: {
        type: "/problems/rate-limited",
        title: "Too many requests",
        status: 429,
      },
    });
    const page = await renderWithToken("hot-token");

    await waitFor(
      () =>
        (page.shadowRoot?.querySelector(".retry-btn") as HTMLButtonElement)
          ?.disabled === true,
    );

    // The retry surface renders and the button stays disabled with the
    // ticking label while the cooldown runs.
    const retry = page.shadowRoot?.querySelector(
      ".retry-btn",
    ) as HTMLButtonElement;
    expect(retry).not.toBeNull();
    expect(retry.textContent?.trim()).toBe(retryAfterMessage(2));
  });
});
