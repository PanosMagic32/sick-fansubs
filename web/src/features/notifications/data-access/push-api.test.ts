/**
 * Push endpoint suite — paths,
 * methods, and the CSRF knobs of the six functions. Transport semantics
 * live in shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  createPushSubscription,
  deletePushSubscription,
  getNotificationPreferences,
  listPushSubscriptions,
  sendTestPush,
  setNotificationPreference,
} from "./push-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

const CREATE_BODY = {
  endpoint: "https://push.example.com/e",
  keys: { p256dh: "cHVibGljLWtleQ", auth: "YXV0aC1zZWNyZXQ" },
};

describe("push API", () => {
  test("listPushSubscriptions sends GET and returns the envelope", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        subscriptions: [
          {
            id: "s1",
            endpoint: "https://push.example.com/e",
            createdAt: "2026-09-13T12:00:00.000Z",
          },
        ],
      },
    });

    const result = await listPushSubscriptions();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/push-subscriptions");
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.subscriptions).toHaveLength(1);
    expect(result.subscriptions[0]?.id).toBe("s1");
  });

  test("createPushSubscription POSTs the browser body with CSRF and returns the id", async () => {
    setCSRFToken("token-abc");
    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      jsonBody: { id: "s9" },
    });

    const result = await createPushSubscription(CREATE_BODY);

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/push-subscriptions");
    expect(req.headers["x-csrf-token"]).toBe("token-abc");
    expect(JSON.parse(req.body ?? "null")).toEqual(CREATE_BODY);
    expect(result.id).toBe("s9");
  });

  test("deletePushSubscription DELETEs by id with CSRF", async () => {
    setCSRFToken("token-abc");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: null });

    await deletePushSubscription("s1");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/push-subscriptions/s1");
    expect(req.headers["x-csrf-token"]).toBe("token-abc");
  });

  test("getNotificationPreferences sends GET and returns the toggles", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        preferences: [
          { kind: "heart", pushEnabled: true },
          { kind: "comment_reply", pushEnabled: false },
        ],
      },
    });

    const result = await getNotificationPreferences();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/notification-preferences");
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.preferences[0]).toEqual({ kind: "heart", pushEnabled: true });
  });

  test("setNotificationPreference PUTs the toggle with CSRF", async () => {
    setCSRFToken("token-abc");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: null });

    await setNotificationPreference("comment_reply", false);

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/notification-preferences/comment_reply");
    expect(req.headers["x-csrf-token"]).toBe("token-abc");
    expect(JSON.parse(req.body ?? "null")).toEqual({ pushEnabled: false });
  });

  test("sendTestPush POSTs without a body and returns the per-endpoint report", async () => {
    setCSRFToken("token-abc");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        delivered: 1,
        endpoints: [
          { id: "s1", service: "mozilla", accepted: false, status: 413 },
          { id: "s9", service: "fcm", accepted: true, status: 201 },
        ],
      },
    });

    const result = await sendTestPush();

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/push-subscriptions/test");
    expect(req.headers["x-csrf-token"]).toBe("token-abc");
    // The endpoint takes no request body (the server reads none).
    expect(req.body).toBeNull();
    expect(result.delivered).toBe(1);
    // The per-endpoint outcomes survive the transport untouched — the
    // settings section reads THIS device's row out of them.
    expect(result.endpoints).toEqual([
      { id: "s1", service: "mozilla", accepted: false, status: 413 },
      { id: "s9", service: "fcm", accepted: true, status: 201 },
    ]);
  });
});
