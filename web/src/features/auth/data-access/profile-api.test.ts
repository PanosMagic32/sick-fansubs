/**
 * Profile endpoint suite — the wire contract of the profile endpoints:
 * GET /api/v1/users/me (no CSRF), PUT /api/v1/users/me/email (CSRF + the
 * auth-action deadline), and the avatar upload. Transport semantics live in
 * shared/api/client.test.ts.
 */

import { describe, expect, test, afterEach } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import {
  mockFetchOnce,
  restoreMockFetch,
  stubAbortSignalTimeout,
} from "@shared/api/test-utils.js";

import { AUTH_ACTION_TIMEOUT_MS } from "./auth-api.js";
import {
  changeEmail,
  fetchCurrentUserProfile,
  uploadAvatar,
} from "./profile-api.js";

/** The AbortSignal.timeout stub of the current test — restored in afterEach. */
let timeoutSpy: ReturnType<typeof stubAbortSignalTimeout> | null = null;

afterEach(() => {
  timeoutSpy?.restore();
  timeoutSpy = null;
  clearCSRFToken();
  restoreMockFetch();
});

describe("fetchCurrentUserProfile", () => {
  test("returns UserProfile on success", async () => {
    mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u1",
        username: "Katakuri",
        email: "katakuri@example.com",
        role: "admin",
        createdAt: "2023-11-14T22:13:20.000Z",
        avatarUrl: null,
      },
    });

    const result = await fetchCurrentUserProfile();

    expect(result.id).toBe("u1");
    expect(result.username).toBe("Katakuri");
    expect(result.email).toBe("katakuri@example.com");
    expect(result.role).toBe("admin");
    expect(result.avatarUrl).toBeNull();
  });

  test("sends GET to /api/v1/users/me without CSRF", async () => {
    setCSRFToken("existing-token");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u1",
        username: "Katakuri",
        email: "katakuri@example.com",
        role: "user",
        createdAt: "2023-11-14T22:13:20.000Z",
        avatarUrl: null,
      },
    });

    await fetchCurrentUserProfile();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/users/me");
    // Safe read: the CSRF token must never be attached.
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });
});

describe("changeEmail", () => {
  test("PUTs the new email + current password with CSRF and a deadline", async () => {
    setCSRFToken("token-123");
    timeoutSpy = stubAbortSignalTimeout();
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u1",
        username: "Katakuri",
        email: "new@example.com",
        role: "user",
        createdAt: "2023-11-14T22:13:20.000Z",
        avatarUrl: null,
      },
    });

    const profile = await changeEmail({
      email: "new@example.com",
      currentPassword: "current-pass",
    });

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/users/me/email");
    expect(req.headers["x-csrf-token"]).toBe("token-123");
    expect(JSON.parse(req.body as string)).toEqual({
      email: "new@example.com",
      currentPassword: "current-pass",
    });
    // No caller signal — the auth-action deadline is the default.
    expect(req.signal).toBeInstanceOf(AbortSignal);
    expect(timeoutSpy.spy.mock.calls[0]?.[0]).toBe(AUTH_ACTION_TIMEOUT_MS);
    expect(profile.email).toBe("new@example.com");
  });
});

describe("uploadAvatar", () => {
  test("PUTs a multipart file with CSRF and returns the refreshed profile", async () => {
    setCSRFToken("token-123");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u1",
        username: "Katakuri",
        email: "katakuri@example.com",
        role: "user",
        createdAt: "2023-11-14T22:13:20.000Z",
        avatarUrl:
          "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
      },
    });

    const file = new File(["x"], "me.png", { type: "image/png" });
    const profile = await uploadAvatar(file);

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/users/me/avatar");
    // The unsafe method attaches the CSRF header (the multipart body
    // carries the file; the browser owns Content-Type).
    expect(req.headers["x-csrf-token"]).toBe("token-123");
    expect(profile.avatarUrl).toBe(
      "http://localhost:3000/media/images/ab1234567890abcdef1234567890abcd.png",
    );
  });
});
