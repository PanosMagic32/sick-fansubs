/**
 * Direct pins for the push-subscription data-access module: the per-device
 * owner marker's storage safety, the single subscribe path, and the sign-out
 * release. The lifecycle suite covers the reconcile outcomes through the UI.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import {
  mockFetchImpl,
  mockResponse,
  restoreMockFetch,
  stubProperty,
} from "@shared/api/test-utils.js";

import {
  forgetPushOwner,
  PUSH_OWNER_KEY,
  readPushOwner,
  reconcilePushSubscription,
  releasePushOwnership,
  rememberPushOwner,
  subscribePush,
} from "./push-subscription.js";

const restoreStubs: Array<() => void> = [];

function stubSubscription(endpoint: string) {
  return {
    endpoint,
    toJSON: () => ({
      endpoint,
      expirationTime: null,
      keys: { p256dh: "cHVibGljLWtleQ", auth: "YXV0aC1zZWNyZXQ" },
    }),
    unsubscribe: mock(async () => true),
  };
}

function stubPushEnvironment(
  local: ReturnType<typeof stubSubscription> | null,
) {
  const registration = {
    pushManager: {
      getSubscription: mock(async () => local),
      subscribe: mock(async () => local),
    },
  };
  restoreStubs.push(
    stubProperty(globalThis, "PushManager", class {}),
    stubProperty(navigator, "serviceWorker", {
      getRegistration: mock(async () => registration),
    }),
    stubProperty(globalThis, "Notification", {
      permission: "granted",
      requestPermission: mock(async () => "granted"),
    }),
  );
  return registration;
}

beforeEach(() => {
  localStorage.removeItem(PUSH_OWNER_KEY);
});

afterEach(() => {
  for (let i = restoreStubs.length - 1; i >= 0; i--) restoreStubs[i]!();
  restoreStubs.length = 0;
  restoreMockFetch();
  localStorage.removeItem(PUSH_OWNER_KEY);
});

describe("push-subscription", () => {
  test("the owner marker round-trips and clears", () => {
    expect(readPushOwner()).toBeNull();
    rememberPushOwner("u1");
    expect(readPushOwner()).toBe("u1");
    forgetPushOwner();
    expect(readPushOwner()).toBeNull();
  });

  test("a blocked storage surface never throws", () => {
    const blocked = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
      removeItem: () => {
        throw new Error("blocked");
      },
    };
    const restore = stubProperty(globalThis, "localStorage", blocked);
    try {
      expect(readPushOwner()).toBeNull();
      expect(() => rememberPushOwner("u1")).not.toThrow();
      expect(() => forgetPushOwner()).not.toThrow();
    } finally {
      restore();
    }
  });

  test("subscribePush stores the row and posts the explicit takeover", async () => {
    const endpoint = "https://push.example.com/current";
    const local = stubSubscription(endpoint);
    stubPushEnvironment(local);
    let posted = "";
    mockFetchImpl(async (_input, init) => {
      posted = String(init?.body ?? "");
      return mockResponse({ ok: true, status: 201, jsonBody: { id: "s1" } });
    });

    const row = await subscribePush();

    expect(row).toEqual({ id: "s1", endpoint });
    // The caller writes the marker only after it re-checks the live session;
    // the helper itself must not record an owner.
    expect(readPushOwner()).toBeNull();
    // The explicit enable path may claim the endpoint from another account.
    expect(JSON.parse(posted)).toMatchObject({ takeover: true });
  });

  test("reconcile heals without the takeover flag", async () => {
    const endpoint = "https://push.example.com/current";
    const local = stubSubscription(endpoint);
    rememberPushOwner("u1");
    stubPushEnvironment(local);
    let posted = "";
    mockFetchImpl(async (_input, init) => {
      if ((init?.method ?? "GET") === "GET") {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { subscriptions: [] },
        });
      }
      posted = String(init?.body ?? "");
      return mockResponse({ ok: true, status: 201, jsonBody: { id: "s1" } });
    });

    const result = await reconcilePushSubscription("u1");

    expect(result.outcome).toBe("healed");
    expect(JSON.parse(posted).takeover).toBeUndefined();
  });

  test("a 409 heal drops the stale marker instead of retrying", async () => {
    const endpoint = "https://push.example.com/current";
    const local = stubSubscription(endpoint);
    rememberPushOwner("u1");
    stubPushEnvironment(local);
    mockFetchImpl(async (_input, init) => {
      if ((init?.method ?? "GET") === "GET") {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: { subscriptions: [] },
        });
      }
      return mockResponse({
        ok: false,
        status: 409,
        headers: { "content-type": "application/problem+json" },
        jsonBody: { type: "/problems/conflict", title: "Conflict" },
      });
    });

    const result = await reconcilePushSubscription("u1");

    // The server sees the endpoint under another account: the profile's
    // marker is stale, so the next wake-up must not retry the heal.
    expect(result.outcome).toBe("not-owner");
    expect(readPushOwner()).toBeNull();
  });

  test("releasePushOwnership deletes the row, unsubscribes, and clears the marker", async () => {
    const endpoint = "https://push.example.com/current";
    const local = stubSubscription(endpoint);
    rememberPushOwner("u1");
    stubPushEnvironment(local);
    const calls: Array<{ method: string; url: string }> = [];
    mockFetchImpl(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url: String(input) });
      if (method === "GET") {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            subscriptions: [
              { id: "s1", endpoint, createdAt: "2026-09-13T12:00:00.000Z" },
            ],
          },
        });
      }
      return mockResponse({ ok: true, status: 204 });
    });

    await releasePushOwnership();

    expect(calls.map((call) => call.method)).toEqual(["GET", "DELETE"]);
    expect(local.unsubscribe).toHaveBeenCalledTimes(1);
    expect(readPushOwner()).toBeNull();
  });

  test("reconcile never subscribes when the local subscription is gone", async () => {
    rememberPushOwner("u1");
    const registration = stubPushEnvironment(null);

    const result = await reconcilePushSubscription("u1");

    expect(result.outcome).toBe("needs-opt-in");
    expect(registration.pushManager.subscribe).not.toHaveBeenCalled();
  });
});
