/**
 * Web push endpoint functions — the subscription and preference endpoints
 * behind the account-page settings section, built on the shared transport
 * like the other data-access modules. The wire types live in `types.ts`.
 *
 * Wire facts:
 *  - Floor: AUTHENTICATED — the server rejects anonymous with the generic
 *    401.
 *  - Writes (create/delete/toggle) require CSRF — the client attaches
 *    X-CSRF-Token automatically via needsCSRF.
 *  - POST /push-subscriptions returns 201 + { id } — the stored row's id
 *    (the existing row on a same-endpoint resubscribe); keep it for the
 *    DELETE.
 *  - DELETE is 204 ALWAYS (idempotent, masked) — no 404 handling exists.
 *  - POST /push-subscriptions/test is the self-test send: no request body,
 *    200 + { delivered, endpoints } — `delivered` counts the endpoints that
 *    ACCEPTED the message (0 = nothing subscribed, delivery disabled, or
 *    every device rejected), and `endpoints` carries one outcome per
 *    attempted row. User-keyed rate limit 5/10 min (429).
 *  - The preferences read returns the EFFECTIVE toggle per kind (the stored
 *    override or the role default — moderator+ on, everyone else off).
 *    Below-moderator sessions are served a SHORTER list and the PUT for a
 *    withheld kind answers a masked 404, so the client renders exactly the
 *    kinds it is given and never role-checks a kind itself.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

import type {
  NotificationPreferenceList,
  PushNotificationKind,
  PushSubscriptionCreate,
  PushSubscriptionList,
  PushTestResult,
} from "./types.js";

/** GET /api/v1/push-subscriptions — own subscription rows. */
export function listPushSubscriptions(
  opts?: RequestOptions,
): Promise<PushSubscriptionList> {
  return request<PushSubscriptionList>(
    "GET",
    "/push-subscriptions",
    undefined,
    { signal: opts?.signal },
  );
}

/** POST /api/v1/push-subscriptions — store/resubscribe one device, 201 +
 * { id } (the stored row id). */
export function createPushSubscription(
  body: PushSubscriptionCreate,
  opts?: RequestOptions,
): Promise<{ id: string }> {
  return request<{ id: string }>("POST", "/push-subscriptions", body, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/** DELETE /api/v1/push-subscriptions/{id} — remove one row, 204 always. */
export function deletePushSubscription(
  id: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/push-subscriptions/${encodeURIComponent(id)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** GET /api/v1/notification-preferences — the effective per-kind toggles. */
export function getNotificationPreferences(
  opts?: RequestOptions,
): Promise<NotificationPreferenceList> {
  return request<NotificationPreferenceList>(
    "GET",
    "/notification-preferences",
    undefined,
    { signal: opts?.signal },
  );
}

/** PUT /api/v1/notification-preferences/{kind} — write one explicit toggle,
 * 204. */
export function setNotificationPreference(
  kind: PushNotificationKind,
  pushEnabled: boolean,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "PUT",
    `/notification-preferences/${encodeURIComponent(kind)}`,
    { pushEnabled },
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** POST /api/v1/push-subscriptions/test — send one self-test notification to
 * the caller's own devices. Takes no body; the response reports how many
 * endpoints accepted it, with one outcome per endpoint. */
export function sendTestPush(opts?: RequestOptions): Promise<PushTestResult> {
  return request<PushTestResult>(
    "POST",
    "/push-subscriptions/test",
    undefined,
    {
      needsCSRF: true,
      signal: opts?.signal,
    },
  );
}
