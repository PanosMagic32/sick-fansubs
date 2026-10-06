/**
 * Auth endpoint suite — the wire contract of the authentication endpoints
 * (paths, methods, bodies, CSRF knobs) against the shared transport.
 * Transport semantics (problem parsing, 401 callback, timeouts) are covered
 * by shared/api/client.test.ts.
 */

import { describe, expect, test, beforeEach, afterEach } from "bun:test";

import {
  ApiError,
  ValidationError,
  clearCSRFToken,
  onUnauthenticated,
  setCSRFToken,
} from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  bootstrapSession,
  changePassword,
  forgotPassword,
  register,
  resendVerification,
  resetPassword,
  signIn,
  signOut,
  signOutAll,
  verifyEmail,
} from "./auth-api.js";

// Make CSRF token state predictable between tests.
beforeEach(() => {
  clearCSRFToken();
});

afterEach(() => {
  restoreMockFetch();
});

// ── Successful API calls ────────────────────────────────────────────

describe("signIn", () => {
  test("returns SignInResponse on success", async () => {
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        csrfToken: "tok_abc",
        user: { id: "1", username: "alice", role: "user" },
      },
    });

    const result = await signIn({
      identifier: "alice",
      password: "secret",
    });

    expect(result.csrfToken).toBe("tok_abc");
    expect(result.user.username).toBe("alice");
    expect(result.user.role).toBe("user");
  });

  test("sends correct method, path, and body", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        csrfToken: "tok",
        user: { id: "1", username: "a", role: "user" },
      },
    });

    await signIn({ identifier: "alice@example.com", password: "p4ss" });

    expect(req.method).toBe("POST");
    expect(req.body).toContain('"identifier":"alice@example.com"');
    expect(req.body).toContain('"password":"p4ss"');
    expect(req.credentials).toBe("same-origin");
    expect(req.url).toBe("/api/v1/auth/sign-in");
  });

  test("does NOT send CSRF token header", async () => {
    setCSRFToken("existing-token");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        csrfToken: "tok",
        user: { id: "1", username: "a", role: "user" },
      },
    });

    await signIn({ identifier: "a", password: "b" });

    // The positive request first: the mock's defaults make "sent nothing"
    // green, so the absent header alone cannot anchor this pin.
    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/auth/sign-in");
    expect(JSON.parse(req.body as string)).toEqual({
      identifier: "a",
      password: "b",
    });
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("includes Content-Type header with body", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        csrfToken: "tok",
        user: { id: "1", username: "a", role: "user" },
      },
    });

    await signIn({ identifier: "a", password: "b" });

    expect(req.headers["content-type"]).toBe("application/json");
  });

  test("throws ApiError on 401", async () => {
    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await signIn({ identifier: "x", password: "y" });
      expect.unreachable("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(401);
      expect((err as ApiError).type).toBe("/problems/auth/invalid-credentials");
    }
  });
});

describe("register", () => {
  test("returns RegisterResponse on 201", async () => {
    mockFetchOnce({
      ok: true,
      status: 201,
      jsonBody: {
        csrfToken: "new_tok",
        user: { id: "2", username: "bob", role: "user" },
      },
    });

    const result = await register({
      username: "bob",
      email: "bob@test.com",
      password: "p4ssw0rd",
    });

    expect(result.user.username).toBe("bob");
  });

  test("throws ValidationError on 422", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [{ field: "username", code: "alreadyTaken" }],
      },
    });

    try {
      await register({
        username: "taken",
        email: "t@t.com",
        password: "12345678",
      });
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ValidationError);
      const ve = err as ValidationError;
      expect(ve.violations).toHaveLength(1);
      const [first] = ve.violations;
      expect(first?.field).toBe("username");
      expect(first?.code).toBe("alreadyTaken");
    }
  });
});

describe("bootstrapSession", () => {
  test("returns authenticated shape", async () => {
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        authenticated: true,
        csrfToken: "boot_tok",
        user: { id: "1", username: "alice", role: "admin" },
      },
    });

    const result = await bootstrapSession();

    expect(result.authenticated).toBe(true);
    expect(result.csrfToken).toBe("boot_tok");
    expect(result.user?.role).toBe("admin");
  });

  test("returns unauthenticated shape", async () => {
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        authenticated: false,
        user: null,
      },
    });

    const result = await bootstrapSession();

    expect(result.authenticated).toBe(false);
    expect(result.user).toBeNull();
    expect(result.csrfToken).toBeUndefined();
  });

  test("sends GET request without body", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false, user: null },
    });

    await bootstrapSession();

    expect(req.method).toBe("GET");
    expect(req.body).toBeNull();
    expect(req.headers["content-type"]).toBeUndefined();
  });
});

describe("signOut", () => {
  test("returns authenticated:false", async () => {
    setCSRFToken("tok");
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false },
    });

    const result = await signOut();

    expect(result.authenticated).toBe(false);
  });

  test("sends CSRF token header", async () => {
    setCSRFToken("my-csrf-token");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false },
    });

    await signOut();

    expect(req.headers["x-csrf-token"]).toBe("my-csrf-token");
  });

  test("does NOT send Content-Type (no body)", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false },
    });

    await signOut();

    // The positive request first (no-body POST to the sign-out path), then
    // the absent header.
    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/auth/sign-out");
    expect(req.body).toBeNull();
    expect(req.headers["content-type"]).toBeUndefined();
  });
});

describe("signOutAll", () => {
  test("sends CSRF token and returns authenticated:false", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false },
    });

    const result = await signOutAll();

    expect(result.authenticated).toBe(false);
    expect(req.headers["x-csrf-token"]).toBe("tok");
    expect(req.headers["content-type"]).toBeUndefined();
  });
});

describe("changePassword", () => {
  test("returns authenticated:false", async () => {
    setCSRFToken("tok");
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false },
    });

    const result = await changePassword({
      currentPassword: "old",
      newPassword: "newp4ssword",
    });

    expect(result.authenticated).toBe(false);
  });

  test("sends CSRF token + body with Content-Type", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { authenticated: false },
    });

    await changePassword({
      currentPassword: "old",
      newPassword: "new",
    });

    expect(req.headers["x-csrf-token"]).toBe("tok");
    expect(req.headers["content-type"]).toBe("application/json");
    expect(req.body).toContain('"currentPassword":"old"');
    expect(req.body).toContain('"newPassword":"new"');
  });

  test("throws ApiError on wrong current password (401)", async () => {
    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await changePassword({
        currentPassword: "wrong",
        newPassword: "new",
      });
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(401);
    }
  });
});

// ── 401 auto-transition via the endpoint knobs ──────────────────────

describe("401 auto-transition knobs", () => {
  test("signIn 401 fires the onUnauthenticated callback", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });

    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await signIn({ identifier: "x", password: "y" });
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(true);
  });

  test("changePassword 401 does NOT fire the callback (suppress401Callback)", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });

    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await changePassword({
        currentPassword: "wrong",
        newPassword: "new",
      });
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(false);
  });

  test("signIn 403 does NOT fire the callback (non-401)", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });

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

    try {
      await signIn({ identifier: "x", password: "y" });
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(false);
  });
});

describe("forgotPassword", () => {
  test("POSTs the identifier and resolves the empty object", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {},
    });

    const result = await forgotPassword({ identifier: "someone@example.com" });

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/auth/forgot-password");
    expect(JSON.parse(req.body as string)).toEqual({
      identifier: "someone@example.com",
    });
    expect(result).toEqual({});
  });

  test("does NOT send CSRF (anonymous caller)", async () => {
    setCSRFToken("stale-token");
    const { req } = mockFetchOnce({ ok: true, status: 200, jsonBody: {} });

    await forgotPassword({ identifier: "someone" });

    // The positive request first: the mock's defaults make "sent nothing"
    // green, so the absent header alone cannot anchor this pin.
    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/auth/forgot-password");
    expect(JSON.parse(req.body as string)).toEqual({ identifier: "someone" });
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });
});

describe("resetPassword", () => {
  test("POSTs token + password and resolves undefined on 204", async () => {
    const { req } = mockFetchOnce({ ok: true, status: 204 });

    const result = await resetPassword({
      token: "tok",
      password: "new-password-123",
    });

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/auth/reset-password");
    expect(JSON.parse(req.body as string)).toEqual({
      token: "tok",
      password: "new-password-123",
    });
    expect(result).toBeUndefined();
  });

  test("sends CSRF when a session token exists", async () => {
    setCSRFToken("my-csrf");
    const { req } = mockFetchOnce({ ok: true, status: 204 });

    await resetPassword({ token: "tok", password: "new-password-123" });

    expect(req.headers["x-csrf-token"]).toBe("my-csrf");
  });

  test("sends NO CSRF header when no session token exists", async () => {
    const { req } = mockFetchOnce({ ok: true, status: 204 });

    await resetPassword({ token: "tok", password: "new-password-123" });

    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("throws ApiError on the generic token-invalid problem", async () => {
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

    try {
      await resetPassword({ token: "bad", password: "new-password-123" });
      expect.unreachable("should have thrown");
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(422);
      expect((err as ApiError).type).toBe("/problems/auth/reset-token-invalid");
    }
  });
});

// ── Email verification ───────────────────────────────────────────────

describe("verifyEmail", () => {
  test("POSTs to /auth/verify-email with the token and resolves on 204", async () => {
    const { req } = mockFetchOnce({ ok: true, status: 204 });
    const result = await verifyEmail({
      token: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
    });
    expect(result).toBeUndefined();
    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/auth/verify-email");
    expect(req.body).toContain(
      '"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"',
    );
  });

  test("attaches the CSRF token only when one exists (the reset precedent)", async () => {
    setCSRFToken("tok_csrf");
    const { req } = mockFetchOnce({ ok: true, status: 204 });
    await verifyEmail({
      token: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
    });
    expect(req.headers["x-csrf-token"]).toBe("tok_csrf");
  });

  test("sends NO CSRF header when no session token exists", async () => {
    const { req } = mockFetchOnce({ ok: true, status: 204 });
    await verifyEmail({
      token: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
    });
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("throws ApiError on the generic 422 token-invalid problem", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      jsonBody: {
        type: "/problems/auth/verification-token-invalid",
        title: "Invalid or expired verification token",
        status: 422,
      },
    });
    await expect(
      verifyEmail({
        token: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
      }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("resendVerification", () => {
  test("POSTs empty to /auth/resend-verification with CSRF and resolves on 204", async () => {
    setCSRFToken("tok_csrf");
    const { req } = mockFetchOnce({ ok: true, status: 204 });
    const result = await resendVerification();
    expect(result).toBeUndefined();
    expect(req.url).toBe("/api/v1/auth/resend-verification");
    expect(req.method).toBe("POST");
    expect(req.headers["x-csrf-token"]).toBe("tok_csrf");
  });

  test("throws ApiError on 429 with the Retry-After surface", async () => {
    mockFetchOnce({
      ok: false,
      status: 429,
      jsonBody: {
        type: "/problems/rate-limited",
        title: "Too many",
        status: 429,
      },
      headers: { "Retry-After": "1800" },
    });
    await expect(resendVerification()).rejects.toBeInstanceOf(ApiError);
  });
});
