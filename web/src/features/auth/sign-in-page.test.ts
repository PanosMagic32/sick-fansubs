import { describe, expect, test, beforeEach, afterEach, mock } from "bun:test";

import { ApiError, NetworkError, ValidationError } from "@shared/api/client.js";
import {
  mockFetchImpl,
  mockSessionContext,
  restoreMockFetch,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

import "@features/auth/data-access/session.js";
import "./sign-in-page.js";
import { RESET_SUCCESS_STORAGE_KEY } from "./reset-page.js";
import type { SignInPage } from "./sign-in-page.js";
import type { SessionProvider } from "@features/auth/data-access/session.js";

// ── Helpers ─────────────────────────────────────────────────────────

/**
 * Create a mock SessionContext with the given state and a spy for signIn.
 * Injected by directly setting it on the element for deterministic test
 * control (no session-provider in the test DOM).
 */
function mockSession(
  overrides: {
    status?: "initializing" | "anonymous" | "authenticated" | "error";
    message?: string;
    mustChangePassword?: boolean;
    signIn?: (id: string, pw: string) => Promise<void>;
    retryBootstrap?: () => void | Promise<void>;
  } = {},
) {
  return mockSessionContext({ status: "anonymous", ...overrides });
}

/** Render a sign-in page, inject session, wait for render. */
async function render(
  status: "initializing" | "anonymous" | "authenticated" = "anonymous",
) {
  const page = document.createElement("sign-in-page") as SignInPage;
  // Set the session directly for deterministic test control (no
  // session-provider in the test DOM).
  (page as any).session = mockSession({ status });
  document.body.appendChild(page);
  await page.updateComplete;
  return page;
}

/**
 * Read the text inside an <error-banner> host in the page's shadow root.
 * The banner renders its alert paragraph inside its own shadow root.
 */
function bannerText(page: SignInPage, id: string): string | undefined {
  const host = page.shadowRoot?.querySelector(`#${id}`);
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim();
}

function teardown() {
  document.body.innerHTML = "";
  restoreMockFetch();
}

let originalLocation: Location;

beforeEach(() => {
  // Mock window.location.href to prevent redirect side-effects.
  originalLocation = window.location;
  Object.defineProperty(window, "location", {
    value: { href: "" },
    writable: true,
    configurable: true,
  });
});

afterEach(() => {
  // Restore the REAL location object — delete would remove it entirely
  // (it is an own property here) and break location-dependent tests in
  // later files (the search-page URL-sync tests).
  Object.defineProperty(window, "location", {
    value: originalLocation,
    writable: true,
    configurable: true,
  });
  teardown();
});

// ── Rendering ───────────────────────────────────────────────────────

describe("rendering", () => {
  test("shows loading spinner when initializing", async () => {
    const page = await render("initializing");

    const spinner = page.shadowRoot?.querySelector("loading-spinner");
    expect(spinner).not.toBeNull();
    const loading = spinner?.shadowRoot?.querySelector(".loading");
    expect(loading?.textContent).toContain(el.ui.loading);
    expect(loading?.getAttribute("role")).toBe("status");
    // The spinner branch keeps the route's heading landmark — the only h1
    // in the branch, so the page never renders headingless while it waits.
    const headings = [...(page.shadowRoot?.querySelectorAll("h1") ?? [])];
    expect(headings).toHaveLength(1);
    expect(headings[0]?.classList.contains("sr-only")).toBe(true);
    expect(headings[0]?.textContent?.trim()).toBe(el.auth.signInTitle);
  });

  test("shows loading spinner when authenticated", async () => {
    const page = await render("authenticated");

    expect(page.shadowRoot?.querySelector("loading-spinner")).not.toBeNull();
    const headings = [...(page.shadowRoot?.querySelectorAll("h1") ?? [])];
    expect(headings).toHaveLength(1);
    expect(headings[0]?.classList.contains("sr-only")).toBe(true);
    expect(headings[0]?.textContent?.trim()).toBe(el.auth.signInTitle);
  });

  test("shows sign-in form when anonymous", async () => {
    const page = await render("anonymous");

    const heading = page.shadowRoot?.querySelector("h1");
    expect(heading?.textContent).toBe(el.auth.signInTitle);

    const identifierInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const passwordInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    expect(identifierInput).not.toBeNull();
    expect(passwordInput).not.toBeNull();
  });

  test("labels are from catalog", async () => {
    const page = await render("anonymous");

    const identifierLabel = page.shadowRoot?.querySelector(
      'label[for="signin-identifier"]',
    );
    expect(identifierLabel?.textContent).toBe(el.auth.identifierLabel);

    const passwordLabel = page.shadowRoot?.querySelector(
      'label[for="signin-password"]',
    );
    expect(passwordLabel?.textContent).toBe(el.ui.password);
  });

  test("submit button text is from catalog", async () => {
    const page = await render("anonymous");

    const button = page.shadowRoot?.querySelector("button[type='submit']");
    expect(button?.textContent?.trim()).toBe(el.auth.signInButton);
  });

  test("footer link points to register page", async () => {
    const page = await render("anonymous");

    const link = page.shadowRoot?.querySelector(".footer-link a");
    expect(link?.getAttribute("href")).toBe("/register");
    expect(link?.textContent).toBe(el.auth.noAccount);
  });

  test("form has novalidate to use custom validation", async () => {
    const page = await render("anonymous");

    const form = page.shadowRoot?.querySelector("form");
    expect(form?.hasAttribute("novalidate")).toBe(true);
  });

  test("renders without a provider and submit shows the generic banner", async () => {
    // No provider tree and NO direct session injection: the @consume
    // field stays undefined. The honest optional type makes the guards
    // reachable — render must not throw, and submit must fail with the
    // generic banner instead of crashing on a truthy sentinel.
    const page = document.createElement("sign-in-page") as SignInPage;
    document.body.appendChild(page);
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(bannerText(page, "signin-error")).toBe(el.ui.error);
  });
});

// ── Client-side validation ──────────────────────────────────────────

describe("client validation", () => {
  test("shows required errors on both fields for empty submit", async () => {
    const page = await render("anonymous");

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    const identifierError = page.shadowRoot?.querySelector(
      "#signin-identifier-error",
    );
    const passwordError = page.shadowRoot?.querySelector(
      "#signin-password-error",
    );
    expect(identifierError?.textContent?.trim()).toBe(el.violations.required);
    expect(passwordError?.textContent?.trim()).toBe(el.violations.required);
  });

  test("marks empty fields as invalid and wires describedby", async () => {
    const page = await render("anonymous");

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    const identifier = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    expect(identifier.getAttribute("aria-invalid")).toBe("true");
    expect(identifier.getAttribute("aria-describedby")).toBe(
      "signin-identifier-error",
    );
  });

  test("shows required error only on the empty field", async () => {
    const page = await render("anonymous");

    const identifierInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    identifierInput.value = "testuser";
    identifierInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector("#signin-identifier-error"),
    ).toBeNull();
    const passwordError = page.shadowRoot?.querySelector(
      "#signin-password-error",
    );
    expect(passwordError?.textContent?.trim()).toBe(el.violations.required);
  });

  test("clears field error when user starts typing", async () => {
    const page = await render("anonymous");

    // Trigger an empty-submit error first.
    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    expect(
      page.shadowRoot?.querySelector("#signin-identifier-error"),
    ).not.toBeNull();

    // Now type in the identifier field.
    const input = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    input.value = "a";
    input.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector("#signin-identifier-error"),
    ).toBeNull();
  });
});

// ── Forced-change redirect ───────────────────────────────────────────

describe("forced-change redirect", () => {
  test("a flagged authenticated session redirects to /account", async () => {
    const originalReplace = window.history.replaceState;
    const replaceSpy = mock(() => {});
    (window.history as unknown as { replaceState: unknown }).replaceState =
      replaceSpy;
    try {
      const page = document.createElement("sign-in-page") as SignInPage;
      (page as any).session = mockSession({
        status: "authenticated",
        mustChangePassword: true,
      });
      document.body.appendChild(page);
      await page.updateComplete;

      // connectedCallback fires the guard immediately (replaceState, not
      // pushState — Back must not ping-pong through /sign-in).
      expect(replaceSpy).toHaveBeenCalledWith({}, "", "/account");
    } finally {
      (window.history as unknown as { replaceState: unknown }).replaceState =
        originalReplace;
      teardown();
    }
  });

  test("an unflagged authenticated session still redirects home", async () => {
    const originalReplace = window.history.replaceState;
    const replaceSpy = mock(() => {});
    (window.history as unknown as { replaceState: unknown }).replaceState =
      replaceSpy;
    try {
      const page = document.createElement("sign-in-page") as SignInPage;
      (page as any).session = mockSession({
        status: "authenticated",
        mustChangePassword: false,
      });
      document.body.appendChild(page);
      await page.updateComplete;

      expect(replaceSpy).toHaveBeenCalledWith({}, "", "/");
    } finally {
      (window.history as unknown as { replaceState: unknown }).replaceState =
        originalReplace;
      teardown();
    }
  });
});

// ── Successful sign-in ──────────────────────────────────────────────

describe("sign-in submission", () => {
  test("calls session.signIn with trimmed values", async () => {
    const signInSpy = mock(async (_id: string, _pw: string) => {});
    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    // Fill in fields.
    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "  testuser  ";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "secret";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    // Submit.
    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    // Wait for async signIn to complete.
    await new Promise((r) => setTimeout(r, 10));

    expect(signInSpy).toHaveBeenCalledTimes(1);
    expect(signInSpy).toHaveBeenCalledWith("testuser", "secret");
  });

  test("disables button during pending state", async () => {
    // Use a signIn that never resolves to observe the pending state.
    let resolveSignIn: () => void = () => {};
    const signInSpy = mock(
      async () =>
        new Promise<void>((r) => {
          resolveSignIn = r;
        }),
    );

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    // Fill fields.
    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    // Submit.
    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    // Button should be disabled and show pending text.
    const button = page.shadowRoot?.querySelector(
      "button[type='submit']",
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.textContent?.trim()).toBe(el.auth.signInPending);

    // Resolve and clean up.
    resolveSignIn();
    await new Promise((r) => setTimeout(r, 10));
  });
});

// ── Error handling ──────────────────────────────────────────────────

describe("error handling", () => {
  test("shows invalid credentials message on 401", async () => {
    const signInSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      });
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    // Fill and submit.
    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "wrong";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "wrong";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "signin-error")).toBe(
      el.problems["/problems/auth/invalid-credentials"],
    );
  });

  test("shows rate-limited message on 429", async () => {
    const signInSpy = mock(async () => {
      throw new ApiError(
        {
          type: "/problems/rate-limited",
          title: "Too many requests",
          status: 429,
        },
        1,
      );
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // The server's Retry-After is surfaced, not dropped.
    expect(bannerText(page, "signin-error")).toBe(
      `${el.problems["/problems/rate-limited"]} Δοκιμάστε ξανά σε 1 δευτερόλεπτο.`,
    );
  });

  test("disables submit for the Retry-After window with a live countdown", async () => {
    const signInSpy = mock(async () => {
      throw new ApiError(
        {
          type: "/problems/rate-limited",
          title: "Too many requests",
          status: 429,
        },
        1,
      );
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    const submitBtn = page.shadowRoot?.querySelector(
      'button[type="submit"]',
    ) as HTMLButtonElement;

    // The 429 window disables the button and shows the remaining time on
    // its label — no immediate re-submit.
    expect(submitBtn.disabled).toBe(true);
    expect(submitBtn.textContent?.trim()).toBe(
      "Δοκιμάστε ξανά σε 1 δευτερόλεπτο.",
    );

    // The Enter-key/implicit-submission path must also respect the window:
    // a second submit event during the cooldown must not call the API again.
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    expect(signInSpy).toHaveBeenCalledTimes(1);

    // Window closes (real 1s timer) → button re-enables with the normal
    // label. The banner stays the static announced-once message.
    await new Promise((r) => setTimeout(r, 1200));
    await page.updateComplete;
    expect(submitBtn.disabled).toBe(false);
    expect(submitBtn.textContent?.trim()).toBe(el.auth.signInButton);
    expect(bannerText(page, "signin-error")).toBe(
      `${el.problems["/problems/rate-limited"]} Δοκιμάστε ξανά σε 1 δευτερόλεπτο.`,
    );
  });

  test("shows already-authenticated message on 403 and re-bootstraps", async () => {
    const retrySpy = mock(() => {});
    const signInSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/auth/already-authenticated",
        title: "Already authenticated",
        status: 403,
      });
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({
      signIn: signInSpy,
      retryBootstrap: retrySpy,
    });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "signin-error")).toBe(
      el.problems["/problems/auth/already-authenticated"],
    );
    // Stale client state is reconciled via a session re-bootstrap.
    expect(retrySpy).toHaveBeenCalledTimes(1);
  });

  test("shows generic error on network failure", async () => {
    const signInSpy = mock(async () => {
      throw new NetworkError("Offline");
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "signin-error")).toBe(el.ui.error);
  });

  test("shows generic error when the client-side timeout aborts", async () => {
    const signInSpy = mock(async () => {
      throw new DOMException("The operation was aborted", "AbortError");
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Timeout = same UX as a network failure: generic banner, form stays
    // usable (inputs re-enabled after _pending flips false).
    expect(bannerText(page, "signin-error")).toBe(el.ui.error);
    expect(idInput.hasAttribute("disabled")).toBe(false);
  });

  test("maps 422 maxLength to per-field catalog message", async () => {
    const signInSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [
          {
            field: "identifier",
            code: "maxLength",
          },
        ],
      });
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "test";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Violation codes map to the Greek catalog, not to server free text.
    const fieldError = page.shadowRoot?.querySelector(
      "#signin-identifier-error",
    );
    expect(fieldError?.textContent?.trim()).toBe(el.violations.maxLength);
    expect(page.shadowRoot?.querySelector("#signin-error")).toBeNull();
  });

  test("clears general banner when user edits a field", async () => {
    const signInSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      });
    });

    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    idInput.value = "wrong";
    idInput.dispatchEvent(new InputEvent("input"));
    pwInput.value = "wrong";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
    expect(page.shadowRoot?.querySelector("#signin-error")).not.toBeNull();

    idInput.value = "other";
    idInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;
    expect(page.shadowRoot?.querySelector("#signin-error")).toBeNull();
  });
});

// ── Refresh flow (regression: @consume subscribe) ───────────────────

describe("refresh flow", () => {
  function flush() {
    return new Promise((r) => setTimeout(r, 0));
  }

  test("receives the bootstrap result when mounted before it completes", async () => {
    mockFetchImpl(
      async () =>
        new Response(JSON.stringify({ authenticated: false, user: null }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );

    const provider = document.createElement(
      "session-provider",
    ) as SessionProvider;
    const page = document.createElement("sign-in-page") as SignInPage;
    provider.appendChild(page);
    document.body.appendChild(provider);

    await provider.updateComplete;
    await flush();
    await page.updateComplete;
    await flush();

    // Regression: without `subscribe: true` on @consume, the page received
    // `initializing` once and stayed on the loading spinner forever.
    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("loading-spinner")).toBeNull();
  });

  test("authenticated bootstrap redirects home", async () => {
    // Deferred fetch: sample the DOM while the bootstrap is still in
    // flight to pin that only the spinner renders pre-settle.
    let resolveFetch!: (r: Response) => void;
    mockFetchImpl(
      () =>
        new Promise<Response>((resolve) => {
          resolveFetch = resolve;
        }),
    );

    const nav = { path: null as string | null };
    const listener = ((e: Event) => {
      nav.path = (e as CustomEvent).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);

    try {
      const provider = document.createElement(
        "session-provider",
      ) as SessionProvider;
      const page = document.createElement("sign-in-page") as SignInPage;
      provider.appendChild(page);
      document.body.appendChild(provider);

      await provider.updateComplete;
      await page.updateComplete;

      // Pre-settle: only the spinner may render — never the form.
      expect(page.shadowRoot?.querySelector("loading-spinner")).not.toBeNull();
      expect(page.shadowRoot?.querySelector("form")).toBeNull();

      resolveFetch(
        new Response(
          JSON.stringify({
            authenticated: true,
            csrfToken: "dummy",
            user: { id: "u1", username: "tester", role: "user" },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
      await flush();
      await page.updateComplete;
      await flush();

      // An authenticated visitor must never see the sign-in form — even
      // on direct URL entry.
      expect(page.shadowRoot?.querySelector("form")).toBeNull();
    } finally {
      window.removeEventListener("sf-navigate", listener);
    }

    expect(nav.path).toBe("/");
  });
});

// ── Redirect ────────────────────────────────────────────────────────

describe("redirect", () => {
  test("navigates to home when session is already authenticated on mount", async () => {
    // navigate() from core/router.ts dispatches NAVIGATE_EVENT on window.
    const nav = { path: null as string | null };
    const listener = ((e: Event) => {
      nav.path = (e as CustomEvent).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);

    try {
      const page = document.createElement("sign-in-page") as SignInPage;
      (page as any).session = mockSession({ status: "authenticated" });
      document.body.appendChild(page);
      await page.updateComplete;
    } finally {
      window.removeEventListener("sf-navigate", listener);
    }

    expect(nav.path).toBe("/");
  });
});

// ── Bootstrap error state ───────────────────────────────────────────

describe("bootstrap error state", () => {
  test("renders message and retry button re-bootstraps", async () => {
    const retrySpy = mock(() => {});
    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({
      status: "error",
      message: el.ui.error,
      retryBootstrap: retrySpy,
    });
    document.body.appendChild(page);
    await page.updateComplete;

    const error = page.shadowRoot?.querySelector("error-banner");
    expect(
      error?.shadowRoot?.querySelector(".error")?.textContent?.trim(),
    ).toBe(el.ui.error);

    const retry = page.shadowRoot?.querySelector(".retry-btn") as HTMLElement;
    retry?.click();
    expect(retrySpy).toHaveBeenCalledTimes(1);

    // The retry flips bootstrap back to a known state; the page must leave
    // the error view for the form (the observable outcome of the flow).
    (page as any).session = mockSession();
    page.requestUpdate();
    await page.updateComplete;
    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("error-banner")).toBeNull();

    page.remove();
  });
});

// ── Focus management ───────────────────────────────────────────────

describe("focus management", () => {
  test("client-side errors focus the first invalid field", async () => {
    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession();
    document.body.appendChild(page);
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(page.shadowRoot?.activeElement?.id).toBe("signin-identifier");
  });

  test("422 field errors focus the first invalid field after re-enable", async () => {
    const signInSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [
          {
            field: "password",
            code: "maxLength",
          },
        ],
      });
    });
    const page = document.createElement("sign-in-page") as SignInPage;
    (page as any).session = mockSession({ signIn: signInSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    const idInput = page.shadowRoot?.querySelector(
      "#signin-identifier",
    ) as HTMLInputElement;
    idInput.value = "tester";
    idInput.dispatchEvent(new InputEvent("input"));
    const pwInput = page.shadowRoot?.querySelector(
      "#signin-password",
    ) as HTMLInputElement;
    pwInput.value = "pass";
    pwInput.dispatchEvent(new InputEvent("input"));
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Focus lands AFTER _pending flips false (a disabled input cannot
    // receive focus) — the pending-disable ordering pin.
    expect(page.shadowRoot?.activeElement?.id).toBe("signin-password");
  });
});

// ── Forgot-password link + post-reset banner ───────────────────────────

describe("forgot-password link and reset banner", () => {
  test("renders the forgot-password link under the form", async () => {
    const page = await render("anonymous");

    const links = page.shadowRoot?.querySelectorAll(".footer-link a");
    const forgot = Array.from(links ?? []).find(
      (a) => a.getAttribute("href") === "/auth/forgot",
    );
    expect(forgot?.textContent).toBe(el.auth.forgotLink);
  });

  test("consumes the post-reset sessionStorage banner once", async () => {
    sessionStorage.setItem(RESET_SUCCESS_STORAGE_KEY, "1");
    const page = await render("anonymous");

    const note = page.shadowRoot?.querySelector(".reset-success");
    expect(note?.textContent?.trim()).toBe(el.auth.resetSuccess);
    // Read once, removed immediately — a refresh must not re-show it.
    expect(sessionStorage.getItem(RESET_SUCCESS_STORAGE_KEY)).toBeNull();
    // The note is a focusable live region — the house status shape.
    expect(note?.getAttribute("role")).toBe("status");
    expect(note?.getAttribute("tabindex")).toBe("-1");
    expect(page.shadowRoot?.activeElement).toBe(note);
  });

  test("focuses the post-reset note once it appears after the bootstrap spinner", async () => {
    // The hard-load path renders the spinner first: the note mounts later,
    // and the one-shot focus must wait for it instead of firing into nothing.
    sessionStorage.setItem(RESET_SUCCESS_STORAGE_KEY, "1");
    const page = await render("initializing");
    expect(page.shadowRoot?.querySelector(".reset-success")).toBeNull();

    (page as any).session = mockSession();
    page.requestUpdate();
    await page.updateComplete;

    const note = page.shadowRoot?.querySelector(".reset-success");
    expect(note).not.toBeNull();
    expect(page.shadowRoot?.activeElement).toBe(note);
  });

  test("shows no banner without the storage key", async () => {
    const page = await render("anonymous");
    expect(page.shadowRoot?.querySelector(".reset-success")).toBeNull();
  });
});
