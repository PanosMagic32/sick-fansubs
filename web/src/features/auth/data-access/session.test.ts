/**
 * Session provider tests.
 *
 * These tests verify the provider's internal state machine directly via
 * the public getState() method. Context delivery to consuming components
 * is covered by the `refresh flow` regression tests in sign-in-page.test.ts
 * and register-page.test.ts (which require @consume to pass
 * `subscribe: true` — see those files for the bug this guards against).
 */

import { describe, expect, test, beforeEach, afterEach } from "bun:test";

import { clearCSRFToken, getCSRFToken } from "@shared/api/client.js";
import { ApiError } from "@shared/api/client.js";
import { mockFetchImpl, restoreMockFetch } from "@shared/api/test-utils.js";

import "./session.js"; // registers <session-provider>
import type { SessionProvider } from "./session.js";

// ── Helpers ─────────────────────────────────────────────────────────

/** Wait for microtasks to flush. */
function flush() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

/**
 * Create and mount a session provider, wait for bootstrap to complete.
 * Returns the provider element for direct state inspection.
 */
async function setup(): Promise<SessionProvider> {
  clearCSRFToken();

  const provider = document.createElement(
    "session-provider",
  ) as SessionProvider;
  document.body.appendChild(provider);

  // Wait for the first render (firstUpdated triggers bootstrap) and for
  // the async bootstrap fetch to complete.
  await provider.updateComplete;
  await new Promise((r) => setTimeout(r, 20));
  await provider.updateComplete;
  await flush();

  return provider;
}

function teardown() {
  document.body.innerHTML = "";
  restoreMockFetch();
}

beforeEach(() => {
  clearCSRFToken();
});

afterEach(() => {
  teardown();
});

/** Create a simple mock fetch that always returns the given JSON. */
function mockFixedJSON(status: number, body: unknown) {
  mockFetchImpl(
    async () =>
      ({
        ok: status >= 200 && status < 300,
        status,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () => body,
        text: async () => JSON.stringify(body),
      }) as unknown as Response,
  );
}

/** Create a route-aware mock fetch for multi-endpoint tests. */
function mockRoutingFetch(
  routes: Record<
    string,
    (method: string) => Promise<{ status: number; body: unknown }> | null
  >,
) {
  mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : "";
    const method = init?.method ?? "GET";

    for (const [pathPattern, handler] of Object.entries(routes)) {
      if (url.includes(pathPattern)) {
        const result = await handler(method);
        if (result) {
          return {
            ok: result.status >= 200 && result.status < 300,
            status: result.status,
            headers: new Headers({ "content-type": "application/json" }),
            json: async () => result.body,
            text: async () => JSON.stringify(result.body),
          } as Response;
        }
      }
    }
    throw new Error(`Unexpected request: ${method} ${url}`);
  });
}

// ── Bootstrap tests ─────────────────────────────────────────────────

describe("bootstrap", () => {
  test("transitions to anonymous when unauthenticated", async () => {
    mockFixedJSON(200, { authenticated: false, user: null });

    const p = await setup();

    expect(p.getState()).toEqual({ status: "anonymous" });
    expect(getCSRFToken()).toBeNull();
  });

  test("transitions to authenticated when session is valid", async () => {
    mockFixedJSON(200, {
      authenticated: true,
      csrfToken: "boot_tok",
      user: { id: "1", username: "alice", role: "user" },
      mustChangePassword: false,
    });

    const p = await setup();

    expect(p.getState()).toEqual({
      status: "authenticated",
      user: { id: "1", username: "alice", role: "user" },
      mustChangePassword: false,
    });
    expect(getCSRFToken()).toBe("boot_tok");
  });

  test("carries the forced-change flag when authenticated", async () => {
    mockFixedJSON(200, {
      authenticated: true,
      csrfToken: "boot_tok",
      user: { id: "1", username: "alice", role: "user" },
      mustChangePassword: true,
    });

    const p = await setup();

    expect(p.getState()).toEqual({
      status: "authenticated",
      user: { id: "1", username: "alice", role: "user" },
      mustChangePassword: true,
    });
  });

  test("transitions to error on network failure", async () => {
    mockFetchImpl(async () => {
      throw new TypeError("Failed to fetch");
    });

    const p = await setup();

    const s = p.getState();
    expect(s.status).toBe("error");
    // Error message is Greek (not the NetworkError's English message).
    expect((s as { message: string }).message).toContain("Αποτυχία");
  });

  test("starts in initializing before bootstrap completes", () => {
    mockFixedJSON(200, { authenticated: false, user: null });

    const provider = document.createElement(
      "session-provider",
    ) as SessionProvider;
    document.body.appendChild(provider);

    // Should be initializing before any async work.
    expect(provider.getState()).toEqual({ status: "initializing" });

    provider.remove();
  });
});

// ── Action tests ────────────────────────────────────────────────────

describe("signIn", () => {
  test("transitions to authenticated and sets CSRF token", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: { authenticated: false, user: null },
        }),
      "/auth/sign-in": (method) =>
        method === "POST"
          ? Promise.resolve({
              status: 200,
              body: {
                csrfToken: "signin_tok",
                user: { id: "2", username: "bob", role: "admin" },
                mustChangePassword: false,
              },
            })
          : null,
    });

    const p = await setup();
    expect(p.getState().status).toBe("anonymous");

    // Call signIn directly on the provider's context.
    // We access the context via the internal property for testing.
    const ctx = (p as any)._ctx;
    await ctx.signIn("bob", "password");
    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({
      status: "authenticated",
      user: { id: "2", username: "bob", role: "admin" },
      mustChangePassword: false,
    });
    expect(getCSRFToken()).toBe("signin_tok");
  });

  test("stale sign-in response is discarded (monotonic guard)", async () => {
    let signInResolve: ((v: Response) => void) | null = null;
    let signOutResolve: ((v: Response) => void) | null = null;

    mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : "";
      const method = init?.method ?? "GET";

      if (url.includes("/auth/session")) {
        return {
          ok: true,
          status: 200,
          headers: new Headers({ "content-type": "application/json" }),
          json: async () => ({ authenticated: false, user: null }),
          text: async () =>
            JSON.stringify({ authenticated: false, user: null }),
        } as Response;
      }
      if (url.includes("/auth/sign-in") && method === "POST") {
        return new Promise<Response>((resolve) => {
          signInResolve = resolve;
        });
      }
      if (url.includes("/auth/sign-out") && method === "POST") {
        return new Promise<Response>((resolve) => {
          signOutResolve = resolve;
        });
      }
      throw new Error(`Unexpected: ${method} ${url}`);
    });

    const p = await setup();
    expect(p.getState().status).toBe("anonymous");

    const ctx = (p as any)._ctx;

    // Start sign-in (paused).
    const signInPromise = ctx.signIn("bob", "password");
    // Start sign-out (paused).
    const signOutPromise = ctx.signOut();

    // Resolve sign-out first → should win.
    await new Promise((r) => setTimeout(r, 10));
    signOutResolve!({
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": "application/json" }),
      json: async () => ({ authenticated: false }),
      text: async () => JSON.stringify({ authenticated: false }),
    } as Response);
    await signOutPromise;
    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({ status: "anonymous" });

    // Now resolve the stale sign-in.
    await new Promise((r) => setTimeout(r, 10));
    signInResolve!({
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": "application/json" }),
      json: async () => ({
        csrfToken: "stale_tok",
        user: { id: "99", username: "stale", role: "user" },
      }),
      text: async function () {
        return JSON.stringify(await (this as Response).json());
      },
    } as Response);
    await signInPromise;
    await p.updateComplete;
    await flush();

    // Stale sign-in MUST NOT overwrite the sign-out state.
    expect(p.getState()).toEqual({ status: "anonymous" });
    expect(getCSRFToken()).toBeNull();
  });
});

describe("signOut", () => {
  test("transitions to anonymous and clears CSRF token", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "boot_tok",
            user: { id: "1", username: "alice", role: "user" },
          },
        }),
      "/auth/sign-out": (method) =>
        method === "POST"
          ? Promise.resolve({
              status: 200,
              body: { authenticated: false },
            })
          : null,
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    const ctx = (p as any)._ctx;
    await ctx.signOut();
    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({ status: "anonymous" });
    expect(getCSRFToken()).toBeNull();
  });
});

describe("signOutAll", () => {
  test("transitions to anonymous and clears CSRF token", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "a", role: "user" },
          },
        }),
      "/auth/sign-out-all": (method) =>
        method === "POST"
          ? Promise.resolve({
              status: 200,
              body: { authenticated: false },
            })
          : null,
    });

    const p = await setup();

    const ctx = (p as any)._ctx;
    await ctx.signOutAll();
    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({ status: "anonymous" });
    expect(getCSRFToken()).toBeNull();
  });
});

describe("register", () => {
  test("transitions to authenticated on success", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: { authenticated: false, user: null },
        }),
      "/auth/register": (method) =>
        method === "POST"
          ? Promise.resolve({
              status: 201,
              body: {
                csrfToken: "reg_tok",
                user: { id: "3", username: "newbie", role: "user" },
              },
            })
          : null,
    });

    const p = await setup();

    const ctx = (p as any)._ctx;
    await ctx.register("newbie", "n@test.com", "p4ssw0rd");
    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({
      status: "authenticated",
      user: { id: "3", username: "newbie", role: "user" },
      mustChangePassword: false,
    });
    expect(getCSRFToken()).toBe("reg_tok");
  });
});

describe("changePassword", () => {
  test("transitions to anonymous and clears CSRF", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "a", role: "user" },
          },
        }),
      "/auth/password": (method) =>
        method === "PUT"
          ? Promise.resolve({
              status: 200,
              body: { authenticated: false },
            })
          : null,
    });

    const p = await setup();

    const ctx = (p as any)._ctx;
    await ctx.changePassword("old", "new");
    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({ status: "anonymous" });
    expect(getCSRFToken()).toBeNull();
  });

  test("wrong current password 401 keeps the session authenticated", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "a", role: "user" },
          },
        }),
      "/auth/password": (method) =>
        method === "PUT"
          ? Promise.resolve({
              status: 401,
              body: {
                type: "/problems/auth/invalid-credentials",
                title: "Invalid credentials",
                status: 401,
              },
            })
          : null,
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    const ctx = (p as any)._ctx;
    await expect(ctx.changePassword("wrong", "new")).rejects.toBeInstanceOf(
      ApiError,
    );

    // suppress401Callback: the 401 means "wrong current password",
    // not session expiry — no anonymous transition.
    expect(p.getState().status).toBe("authenticated");
  });
});

// ── 401 auto-transition ─────────────────────────────────────────────

describe("revalidate", () => {
  test("re-checks the session when the window regains focus", async () => {
    let sessionCalls = 0;
    mockFetchImpl(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : "";
      if (!url.includes("/auth/session")) {
        throw new Error("unexpected fetch: " + url);
      }
      sessionCalls++;
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () =>
          sessionCalls === 1
            ? {
                authenticated: true,
                csrfToken: "tok",
                user: { id: "1", username: "alice", role: "user" },
              }
            : { authenticated: false, user: null },
        text: async function () {
          return JSON.stringify(await (this as Response).json());
        },
      } as unknown as Response;
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    window.dispatchEvent(new Event("focus"));
    await flush();

    // Cross-tab sign-out is picked up silently: authenticated → anonymous.
    expect(sessionCalls).toBe(2);
    expect(p.getState()).toEqual({ status: "anonymous" });
  });

  test("does not stack revalidate requests during a focus storm", async () => {
    let sessionCalls = 0;
    let resolveSession!: (r: Response) => void;
    mockFetchImpl(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : "";
      if (!url.includes("/auth/session")) {
        throw new Error("unexpected fetch: " + url);
      }
      sessionCalls++;
      if (sessionCalls > 1) {
        // Hold the revalidate in flight so the burst below can only queue
        // against the in-flight dedupe guard.
        return new Promise<Response>((resolve) => {
          resolveSession = resolve;
        });
      }
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () => ({
          authenticated: true,
          csrfToken: "tok",
          user: { id: "1", username: "alice", role: "user" },
        }),
        text: async () =>
          JSON.stringify({
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "alice", role: "user" },
          }),
      } as unknown as Response;
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    // Rapid focus cycling while the first revalidate is in flight must
    // produce exactly ONE extra request, not one per event.
    window.dispatchEvent(new Event("focus"));
    window.dispatchEvent(new Event("focus"));
    window.dispatchEvent(new Event("focus"));
    await flush();
    expect(sessionCalls).toBe(2);

    // Complete the in-flight request with the same authenticated state.
    resolveSession({
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": "application/json" }),
      json: async () => ({
        authenticated: true,
        csrfToken: "tok",
        user: { id: "1", username: "alice", role: "user" },
      }),
      text: async () =>
        JSON.stringify({
          authenticated: true,
          csrfToken: "tok",
          user: { id: "1", username: "alice", role: "user" },
        }),
    } as unknown as Response);
    await flush();
    expect(p.getState().status).toBe("authenticated");

    // The guard released, not latched: a later focus re-checks again.
    window.dispatchEvent(new Event("focus"));
    await flush();
    expect(sessionCalls).toBe(3);
  });

  test("revalidate works again after the provider is removed and re-added", async () => {
    let sessionCalls = 0;
    mockFetchImpl(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : "";
      if (!url.includes("/auth/session")) {
        throw new Error("unexpected fetch: " + url);
      }
      sessionCalls++;
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () =>
          sessionCalls === 1
            ? {
                authenticated: true,
                csrfToken: "tok",
                user: { id: "1", username: "alice", role: "user" },
              }
            : { authenticated: false, user: null },
        text: async function () {
          return JSON.stringify(await (this as Response).json());
        },
      } as unknown as Response;
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    // Disconnect sets the _disconnected latch; the reconnect must reset it
    // or the focus re-check below is silently discarded.
    p.remove();
    document.body.appendChild(p);
    await p.updateComplete;

    window.dispatchEvent(new Event("focus"));
    await flush();

    expect(sessionCalls).toBe(2);
    expect(p.getState()).toEqual({ status: "anonymous" });
  });

  test("keeps the current state when a focus re-check fails", async () => {
    let sessionCalls = 0;
    mockFetchImpl(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : "";
      if (!url.includes("/auth/session")) {
        throw new Error("unexpected fetch: " + url);
      }
      sessionCalls++;
      if (sessionCalls > 1) {
        throw new TypeError("Failed to fetch");
      }
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () => ({
          authenticated: true,
          csrfToken: "tok",
          user: { id: "1", username: "alice", role: "user" },
        }),
        text: async () =>
          JSON.stringify({
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "alice", role: "user" },
          }),
      } as unknown as Response;
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    window.dispatchEvent(new Event("focus"));
    await flush();

    // Network failures must not log the user out or flip to the error state.
    expect(p.getState().status).toBe("authenticated");
  });

  test("does not re-check while anonymous", async () => {
    let sessionCalls = 0;
    mockFetchImpl(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : "";
      if (!url.includes("/auth/session")) {
        throw new Error("unexpected fetch: " + url);
      }
      sessionCalls++;
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () => ({ authenticated: false, user: null }),
        text: async () => JSON.stringify({ authenticated: false, user: null }),
      } as unknown as Response;
    });

    const p = await setup();
    expect(p.getState().status).toBe("anonymous");

    window.dispatchEvent(new Event("focus"));
    await flush();

    // The silent re-check only targets the stale-authenticated case.
    expect(sessionCalls).toBe(1);
  });

  test("discards a stale authenticated result if the state flipped meanwhile", async () => {
    let sessionCalls = 0;
    let resolveSession!: (r: Response) => void;
    mockFetchImpl(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : "";
      if (!url.includes("/auth/session")) {
        throw new Error("unexpected fetch: " + url);
      }
      sessionCalls++;
      if (sessionCalls > 1) {
        // The revalidate request is deferred so the state can flip first.
        return new Promise<Response>((resolve) => {
          resolveSession = resolve;
        });
      }
      return {
        ok: true,
        status: 200,
        headers: new Headers({ "content-type": "application/json" }),
        json: async () => ({
          authenticated: true,
          csrfToken: "tok",
          user: { id: "1", username: "alice", role: "user" },
        }),
        text: async () =>
          JSON.stringify({
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "alice", role: "user" },
          }),
      } as unknown as Response;
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    window.dispatchEvent(new Event("focus"));
    await flush();
    expect(sessionCalls).toBe(2);

    // While the revalidate is in flight the session flips to anonymous
    // WITHOUT bumping the generation (the 401-callback path).
    (p as any)._setState({ status: "anonymous" });

    resolveSession({
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": "application/json" }),
      json: async () => ({
        authenticated: true,
        csrfToken: "tok2",
        user: { id: "1", username: "alice", role: "user" },
      }),
      text: async () =>
        JSON.stringify({
          authenticated: true,
          csrfToken: "tok2",
          user: { id: "1", username: "alice", role: "user" },
        }),
    } as unknown as Response);
    await flush();

    // A pre-commit authenticated snapshot must not resurrect the session.
    expect(p.getState()).toEqual({ status: "anonymous" });
  });
});

describe("401 auto-transition", () => {
  test("session transitions to anonymous on API 401", async () => {
    mockRoutingFetch({
      "/auth/session": () =>
        Promise.resolve({
          status: 200,
          body: {
            authenticated: true,
            csrfToken: "tok",
            user: { id: "1", username: "a", role: "user" },
          },
        }),
      // All non-session requests → 401
      "/auth/sign-out": () =>
        Promise.resolve({
          status: 401,
          body: {
            type: "/problems/auth/invalid-credentials",
            title: "Invalid credentials",
            status: 401,
          },
        }),
    });

    const p = await setup();
    expect(p.getState().status).toBe("authenticated");

    const ctx = (p as any)._ctx;
    try {
      await ctx.signOut();
    } catch {
      // Expected — 401 throws ApiError
    }

    await p.updateComplete;
    await flush();

    expect(p.getState()).toEqual({ status: "anonymous" });
    expect(getCSRFToken()).toBeNull();
  });
});

// ── Context shape ───────────────────────────────────────────────────

describe("context shape", () => {
  test("context object has all expected methods and state", async () => {
    mockFixedJSON(200, { authenticated: false, user: null });

    const p = await setup();
    const ctx = (p as any)._ctx;

    expect(typeof ctx.signIn).toBe("function");
    expect(typeof ctx.signOut).toBe("function");
    expect(typeof ctx.signOutAll).toBe("function");
    expect(typeof ctx.register).toBe("function");
    expect(typeof ctx.revalidate).toBe("function");
    expect(typeof ctx.changePassword).toBe("function");
    expect(ctx.state).toBeDefined();
  });
});

// ── Action timeouts ───────────────────────────────────────────────

describe("action timeouts", () => {
  test("auth actions pass a 15s client-side timeout signal", async () => {
    // Stub AbortSignal.timeout so the test controls the abort instead of
    // waiting 15 real seconds; capture the requested duration.
    // (Box object: closure assignments don't widen TS narrowing.)
    const originalTimeout = AbortSignal.timeout;
    const captured = { ms: null as number | null };
    const controller = new AbortController();
    AbortSignal.timeout = ((ms: number) => {
      captured.ms = ms;
      return controller.signal;
    }) as unknown as typeof AbortSignal.timeout;

    try {
      mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = typeof input === "string" ? input : "";
        if (url.includes("/auth/session")) {
          return {
            ok: true,
            status: 200,
            headers: new Headers({ "content-type": "application/json" }),
            json: async () => ({ authenticated: false, user: null }),
            text: async () =>
              JSON.stringify({ authenticated: false, user: null }),
          } as unknown as Response;
        }
        if (url.includes("/auth/sign-in")) {
          // Hang like a stalled connection; the timeout signal's abort
          // rejects the request exactly as a real fetch would.
          return new Promise<Response>((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => {
              reject(
                new DOMException("The operation was aborted", "AbortError"),
              );
            });
          });
        }
        throw new Error("Unexpected request: " + url);
      });

      const p = await setup();
      expect(p.getState().status).toBe("anonymous");

      const ctx = (p as any)._ctx;
      const pending = ctx.signIn("bob", "password");

      // The seam is wired with the 15s timeout (client.ts constant).
      expect(captured.ms).toBe(15_000);

      // Simulate the timeout firing.
      controller.abort();
      try {
        await pending;
        expect.unreachable();
      } catch (err) {
        expect((err as DOMException).name).toBe("AbortError");
      }

      // Failure with a live (non-initializing) state: state stands and no
      // CSRF token was set.
      await p.updateComplete;
      await flush();
      expect(p.getState().status).toBe("anonymous");
      expect(getCSRFToken()).toBeNull();
    } finally {
      AbortSignal.timeout = originalTimeout;
    }
  });
});
