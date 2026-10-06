import { describe, expect, test, beforeEach, afterEach, mock } from "bun:test";

import { ApiError, NetworkError, ValidationError } from "@shared/api/client.js";
import {
  mockFetchImpl,
  mockSessionContext,
  restoreMockFetch,
} from "@shared/api/test-utils.js";
import { el } from "@shared/catalog/el.js";

import "@features/auth/data-access/session.js";
import "./register-page.js";
import { REGISTER_SUCCESS_STORAGE_KEY } from "./register-page.js";
import type { RegisterPage } from "./register-page.js";
import type { SessionProvider } from "@features/auth/data-access/session.js";

// ── Helpers ─────────────────────────────────────────────────────────

/**
 * Create a mock SessionContext with the given state and a spy for register.
 * Injected by directly setting it on the element for deterministic test
 * control (no session-provider in the test DOM).
 */
function mockSession(
  overrides: {
    status?: "initializing" | "anonymous" | "authenticated" | "error";
    message?: string;
    register?: (
      username: string,
      email: string,
      password: string,
    ) => Promise<void>;
    retryBootstrap?: () => void;
  } = {},
) {
  return mockSessionContext({ status: "anonymous", ...overrides });
}

/** Render a registration page, inject session, wait for render. */
async function render(
  status:
    "initializing" | "anonymous" | "authenticated" | "error" = "anonymous",
) {
  const page = document.createElement("register-page") as RegisterPage;
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
function bannerText(page: RegisterPage, id: string): string | undefined {
  const host = page.shadowRoot?.querySelector(`#${id}`);
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim();
}

/** Fill one form input by id and dispatch an input event. */
async function fill(
  page: RegisterPage,
  id: string,
  value: string,
): Promise<HTMLInputElement> {
  const input = page.shadowRoot?.querySelector(`#${id}`) as HTMLInputElement;
  input.value = value;
  input.dispatchEvent(new InputEvent("input"));
  await page.updateComplete;
  return input;
}

/** Submit the form and wait for rendering. */
async function submit(page: RegisterPage): Promise<void> {
  const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
  form.dispatchEvent(new Event("submit", { cancelable: true }));
  await page.updateComplete;
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
    expect(headings[0]?.textContent?.trim()).toBe(el.auth.registerTitle);
  });

  test("shows loading spinner when authenticated", async () => {
    const page = await render("authenticated");

    expect(page.shadowRoot?.querySelector("loading-spinner")).not.toBeNull();
    const headings = [...(page.shadowRoot?.querySelectorAll("h1") ?? [])];
    expect(headings).toHaveLength(1);
    expect(headings[0]?.classList.contains("sr-only")).toBe(true);
    expect(headings[0]?.textContent?.trim()).toBe(el.auth.registerTitle);
  });

  test("shows bootstrap error with retry button", async () => {
    const retrySpy = mock(() => {});
    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({
      status: "error",
      message: "bootstrap boom",
      retryBootstrap: retrySpy,
    });
    document.body.appendChild(page);
    await page.updateComplete;

    const error = page.shadowRoot?.querySelector("error-banner");
    expect(
      error?.shadowRoot?.querySelector(".error")?.textContent?.trim(),
    ).toBe("bootstrap boom");

    const retry = page.shadowRoot?.querySelector(
      ".retry-btn",
    ) as HTMLButtonElement;
    expect(retry?.textContent?.trim()).toBe(el.ui.retry);
    retry.click();
    expect(retrySpy).toHaveBeenCalledTimes(1);

    // The retry flips bootstrap back to a known state; the page must leave
    // the error view for the form (the observable outcome of the flow).
    (page as any).session = mockSession();
    page.requestUpdate();
    await page.updateComplete;
    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("error-banner")).toBeNull();
  });

  test("shows registration form when anonymous", async () => {
    const page = await render("anonymous");

    const heading = page.shadowRoot?.querySelector("h1");
    expect(heading?.textContent).toBe(el.auth.registerTitle);

    expect(page.shadowRoot?.querySelector("#register-username")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("#register-email")).not.toBeNull();
    expect(page.shadowRoot?.querySelector("#register-password")).not.toBeNull();
    expect(
      page.shadowRoot?.querySelector("#register-confirm-password"),
    ).not.toBeNull();
  });

  test("labels are from catalog", async () => {
    const page = await render("anonymous");

    const labels = [
      page.shadowRoot?.querySelector('label[for="register-username"]'),
      page.shadowRoot?.querySelector('label[for="register-email"]'),
      page.shadowRoot?.querySelector('label[for="register-password"]'),
      page.shadowRoot?.querySelector('label[for="register-confirm-password"]'),
    ];
    expect(labels[0]?.textContent).toBe(el.ui.username);
    expect(labels[1]?.textContent).toBe(el.ui.email);
    expect(labels[2]?.textContent).toBe(el.ui.password);
    expect(labels[3]?.textContent).toBe(el.ui.confirmPassword);
  });

  test("email field shows the recovery hint", async () => {
    const page = await render("anonymous");

    const hint = page.shadowRoot?.querySelector("#register-email-hint");
    expect(hint?.textContent?.trim()).toBe(el.auth.emailRecoveryHint);
    expect(hint?.classList.contains("field-hint")).toBe(true);
  });

  test("submit button text is from catalog", async () => {
    const page = await render("anonymous");

    const button = page.shadowRoot?.querySelector("button[type='submit']");
    expect(button?.textContent?.trim()).toBe(el.auth.registerButton);
  });

  test("footer link points to sign-in page", async () => {
    const page = await render("anonymous");

    const link = page.shadowRoot?.querySelector(".footer-link a");
    expect(link?.getAttribute("href")).toBe("/sign-in");
    expect(link?.textContent).toBe(el.auth.alreadyHaveAccount);
  });

  test("form has novalidate to use custom validation", async () => {
    const page = await render("anonymous");

    const form = page.shadowRoot?.querySelector("form");
    expect(form?.hasAttribute("novalidate")).toBe(true);
  });

  test("confirm password input uses new-password autocomplete", async () => {
    const page = await render("anonymous");

    const input = page.shadowRoot?.querySelector(
      "#register-confirm-password",
    ) as HTMLInputElement;
    expect(input.getAttribute("autocomplete")).toBe("new-password");
  });
});

// ── Client-side validation ──────────────────────────────────────────

describe("client validation", () => {
  test("shows required errors on all fields for empty submit", async () => {
    const page = await render("anonymous");

    await submit(page);

    const errors = [
      page.shadowRoot?.querySelector("#register-username-error"),
      page.shadowRoot?.querySelector("#register-email-error"),
      page.shadowRoot?.querySelector("#register-password-error"),
      page.shadowRoot?.querySelector("#register-confirm-password-error"),
    ];
    for (const error of errors) {
      expect(error?.textContent?.trim()).toBe(el.violations.required);
    }
  });

  test("marks required fields as invalid", async () => {
    const page = await render("anonymous");

    await submit(page);

    const username = page.shadowRoot?.querySelector(
      "#register-username",
    ) as HTMLInputElement;
    expect(username.getAttribute("aria-invalid")).toBe("true");
    expect(username.getAttribute("aria-describedby")).toBe(
      "register-username-error",
    );
  });

  test("shows mismatch error when passwords differ", async () => {
    const page = await render("anonymous");

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-1");
    await fill(page, "register-confirm-password", "secret-2");
    await submit(page);

    const error = page.shadowRoot?.querySelector(
      "#register-confirm-password-error",
    );
    expect(error?.textContent?.trim()).toBe(el.auth.passwordsDoNotMatch);
    // The other fields must not show errors.
    expect(
      page.shadowRoot?.querySelector("#register-password-error"),
    ).toBeNull();
  });

  test("clears field error when user starts typing", async () => {
    const page = await render("anonymous");

    // Trigger an empty-submit error first.
    await submit(page);
    expect(
      page.shadowRoot?.querySelector("#register-username-error"),
    ).not.toBeNull();

    // Now type in the username field.
    await fill(page, "register-username", "a");
    expect(
      page.shadowRoot?.querySelector("#register-username-error"),
    ).toBeNull();
  });

  test("clears mismatch error when password is corrected", async () => {
    const page = await render("anonymous");

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-1");
    await fill(page, "register-confirm-password", "secret-2");
    await submit(page);

    const error = page.shadowRoot?.querySelector(
      "#register-confirm-password-error",
    );
    expect(error?.textContent?.trim()).toBe(el.auth.passwordsDoNotMatch);

    // Editing the password field must also clear the now-stale mismatch.
    await fill(page, "register-password", "secret-2");
    expect(
      page.shadowRoot?.querySelector("#register-confirm-password-error"),
    ).toBeNull();
  });
});

// ── Successful registration ─────────────────────────────────────────

describe("registration submission", () => {
  test("calls session.register with trimmed values and no confirmPassword", async () => {
    const registerSpy = mock(
      async (_username: string, _email: string, _password: string) => {},
    );
    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "  testuser  ");
    await fill(page, "register-email", "  test@example.com  ");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    // Wait for async register to complete.
    await new Promise((r) => setTimeout(r, 10));

    expect(registerSpy).toHaveBeenCalledTimes(1);
    // confirmPassword is client-only and never reaches the session API.
    expect(registerSpy).toHaveBeenCalledWith(
      "testuser",
      "test@example.com",
      "secret-pw",
    );
    // The post-registration notice rides sessionStorage to
    // /account (the reset-banner transport).
    expect(sessionStorage.getItem(REGISTER_SUCCESS_STORAGE_KEY)).toBe("1");
    sessionStorage.removeItem(REGISTER_SUCCESS_STORAGE_KEY);
  });

  test("disables button during pending state", async () => {
    // Use a register that never resolves to observe the pending state.
    let resolveRegister: () => void = () => {};
    const registerSpy = mock(
      async () =>
        new Promise<void>((r) => {
          resolveRegister = r;
        }),
    );

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);

    // Button should be disabled and show pending text.
    const button = page.shadowRoot?.querySelector(
      "button[type='submit']",
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.textContent?.trim()).toBe(el.auth.registerPending);

    // Resolve and clean up.
    resolveRegister();
    await new Promise((r) => setTimeout(r, 10));
  });
});

// ── Error handling ──────────────────────────────────────────────────

describe("error handling", () => {
  test("maps 422 violations to per-field errors from the catalog", async () => {
    // The two username length codes are separate server outcomes (the
    // normalized-to-empty vs over-32 case) and map to the one length copy.
    for (const usernameCode of ["maxLength", "minLength"]) {
      const registerSpy = mock(async () => {
        throw new ValidationError({
          type: "/problems/validation",
          title: "Validation failed",
          status: 422,
          violations: [
            { field: "username", code: usernameCode },
            { field: "email", code: "alreadyTaken" },
          ],
        });
      });

      const page = document.createElement("register-page") as RegisterPage;
      (page as any).session = mockSession({ register: registerSpy });
      document.body.appendChild(page);
      await page.updateComplete;

      await fill(page, "register-username", "testuser");
      await fill(page, "register-email", "test@example.com");
      await fill(page, "register-password", "secret-pw");
      await fill(page, "register-confirm-password", "secret-pw");
      await submit(page);
      await new Promise((r) => setTimeout(r, 10));
      await page.updateComplete;

      // Violation codes map to the Greek catalog, not to server free text,
      // and the register form names each rule.
      const usernameError = page.shadowRoot?.querySelector(
        "#register-username-error",
      );
      expect(usernameError?.textContent?.trim()).toBe(
        el.auth.registerUsernameLength,
      );
      const emailError = page.shadowRoot?.querySelector(
        "#register-email-error",
      );
      expect(emailError?.textContent?.trim()).toBe(el.auth.registerEmailTaken);
      // No general banner for known-field violations.
      expect(page.shadowRoot?.querySelector("#register-error")).toBeNull();

      page.remove();
    }
  });

  test("names the failed rule per field", async () => {
    const registerSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [
          { field: "username", code: "invalidFormat" },
          { field: "email", code: "invalidFormat" },
          { field: "password", code: "minLength" },
        ],
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector("#register-username-error")
        ?.textContent?.trim(),
    ).toBe(el.auth.registerUsernameFormat);
    expect(
      page.shadowRoot
        ?.querySelector("#register-email-error")
        ?.textContent?.trim(),
    ).toBe(el.auth.registerEmailFormat);
    expect(
      page.shadowRoot
        ?.querySelector("#register-password-error")
        ?.textContent?.trim(),
    ).toBe(el.auth.registerPasswordMin);
  });

  test("an unmapped code falls back to the generic Greek message", async () => {
    const registerSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [{ field: "password", code: "mysteryCode" }],
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Raw codes and server free text must never reach the UI.
    expect(
      page.shadowRoot
        ?.querySelector("#register-password-error")
        ?.textContent?.trim(),
    ).toBe(el.ui.error);
  });

  test("shows validation banner for 422 with no violations", async () => {
    const registerSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [],
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "register-error")).toBe(
      el.problems["/problems/validation"],
    );
  });

  test("shows banner for violations on unknown fields", async () => {
    const registerSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [{ field: "general", code: "invalid" }],
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Unknown codes fall back to the code itself — never server free text.
    expect(bannerText(page, "register-error")).toBe(el.violations.invalid);
  });

  test("shows conflict message on 409", async () => {
    const registerSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/conflict",
        title: "Conflict",
        status: 409,
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "register-error")).toBe(
      el.problems["/problems/conflict"],
    );
  });

  test("shows already-authenticated message on 403", async () => {
    const retrySpy = mock(() => {});
    const registerSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/auth/already-authenticated",
        title: "Already authenticated",
        status: 403,
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({
      register: registerSpy,
      retryBootstrap: retrySpy,
    });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "register-error")).toBe(
      el.problems["/problems/auth/already-authenticated"],
    );
    // Stale client state is reconciled via a session re-bootstrap.
    expect(retrySpy).toHaveBeenCalledTimes(1);
  });

  test("shows rate-limited message on 429", async () => {
    const registerSpy = mock(async () => {
      throw new ApiError(
        {
          type: "/problems/rate-limited",
          title: "Too many requests",
          status: 429,
        },
        1,
      );
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // The server's Retry-After is surfaced, not dropped.
    expect(bannerText(page, "register-error")).toBe(
      `${el.problems["/problems/rate-limited"]} Δοκιμάστε ξανά σε 1 δευτερόλεπτο.`,
    );
  });

  test("disables submit for the Retry-After window with a live countdown", async () => {
    const registerSpy = mock(async () => {
      throw new ApiError(
        {
          type: "/problems/rate-limited",
          title: "Too many requests",
          status: 429,
        },
        1,
      );
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
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
    submit(page);
    await page.updateComplete;
    expect(registerSpy).toHaveBeenCalledTimes(1);

    // Window closes (real 1s timer) → button re-enables with the normal
    // label. The banner stays the static announced-once message.
    await new Promise((r) => setTimeout(r, 1200));
    await page.updateComplete;
    expect(submitBtn.disabled).toBe(false);
    expect(submitBtn.textContent?.trim()).toBe(el.auth.registerButton);
    expect(bannerText(page, "register-error")).toBe(
      `${el.problems["/problems/rate-limited"]} Δοκιμάστε ξανά σε 1 δευτερόλεπτο.`,
    );
  });

  test("clears general banner when user edits a field", async () => {
    const registerSpy = mock(async () => {
      throw new ApiError({
        type: "/problems/conflict",
        title: "Conflict",
        status: 409,
      });
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;
    expect(page.shadowRoot?.querySelector("#register-error")).not.toBeNull();

    await fill(page, "register-username", "otheruser");
    expect(page.shadowRoot?.querySelector("#register-error")).toBeNull();
  });

  test("shows generic error on network failure", async () => {
    const registerSpy = mock(async () => {
      throw new NetworkError("Offline");
    });

    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "test@example.com");
    await fill(page, "register-password", "secret-pw");
    await fill(page, "register-confirm-password", "secret-pw");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(bannerText(page, "register-error")).toBe(el.ui.error);
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
    const page = document.createElement("register-page") as RegisterPage;
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

  test("authenticated bootstrap redirects to account without painting the form", async () => {
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
      const page = document.createElement("register-page") as RegisterPage;
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

      // An authenticated visitor must never see the registration form —
      // even on direct URL entry.
      expect(page.shadowRoot?.querySelector("form")).toBeNull();
    } finally {
      window.removeEventListener("sf-navigate", listener);
    }

    expect(nav.path).toBe("/account");
  });
});

// ── Redirect ────────────────────────────────────────────────────────

describe("redirect", () => {
  test("navigates to account when session is already authenticated on mount", async () => {
    // navigate() from core/router.ts dispatches NAVIGATE_EVENT on window.
    const nav = { path: null as string | null };
    const listener = ((e: Event) => {
      nav.path = (e as CustomEvent).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);

    try {
      const page = document.createElement("register-page") as RegisterPage;
      (page as any).session = mockSession({ status: "authenticated" });
      document.body.appendChild(page);
      await page.updateComplete;
    } finally {
      window.removeEventListener("sf-navigate", listener);
    }

    expect(nav.path).toBe("/account");
  });
});

// ── Focus management ───────────────────────────────────────────────

describe("focus management", () => {
  test("client-side errors focus the first invalid field", async () => {
    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession();
    document.body.appendChild(page);
    await page.updateComplete;

    const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    await page.updateComplete;

    expect(page.shadowRoot?.activeElement?.id).toBe("register-username");
  });

  test("422 field errors focus the first invalid field after re-enable", async () => {
    const registerSpy = mock(async () => {
      throw new ValidationError({
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [
          {
            field: "email",
            code: "invalidFormat",
          },
        ],
      });
    });
    const page = document.createElement("register-page") as RegisterPage;
    (page as any).session = mockSession({ register: registerSpy });
    document.body.appendChild(page);
    await page.updateComplete;

    await fill(page, "register-username", "testuser");
    await fill(page, "register-email", "bad-email");
    await fill(page, "register-password", "password123");
    await fill(page, "register-confirm-password", "password123");
    await submit(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    // Focus lands AFTER _pending flips false (a disabled input cannot
    // receive focus) — the pending-disable ordering pin.
    expect(page.shadowRoot?.activeElement?.id).toBe("register-email");
  });
});
