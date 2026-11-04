/**
 * Browser push subscription lifecycle — the per-device opt-in marker, the
 * subscribe helper shared by the account settings and the global prompt, and
 * the reconciliation the app shell runs on bootstrap and on wake-up events.
 *
 * The marker gates the client; the server's strict store form is the hard
 * boundary. `sf-push-owner` records which account explicitly enabled push
 * on THIS browser. Only the explicit opt-in (`subscribePush`) sends
 * `takeover: true`; background reconciliation omits it, and the server
 * refuses to move a foreign endpoint (409). A stale tab with a newer session
 * cookie therefore cannot hand one account's notifications to another.
 *
 * A pre-existing subscriber has no marker yet. It is adopted only when the
 * current account's server rows already contain this browser's endpoint —
 * that pairing proves the account opted in on this device, so the marker can
 * be written without another prompt. An explicit off switch, a dismissed
 * prompt, and a sign-out all clear the marker; sign-out also releases the
 * row and the local subscription.
 */

import { ApiError, type RequestOptions } from "@shared/api/client.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { VAPID_PUBLIC_KEY } from "@shared/config/vapid.js";

import {
  createPushSubscription,
  deletePushSubscription,
  listPushSubscriptions,
} from "./push-api.js";
import type { PushSubscriptionCreate } from "./types.js";

/** The per-device opt-in record. The value is the owning account's id. */
export const PUSH_OWNER_KEY = "sf-push-owner";

/** The worker's relay message type (web/public/sw.js — pinned by
 * push-artifacts.test.ts; keep in sync). */
export const PUSH_SUBSCRIPTION_CHANGE_MESSAGE = "sf-push-subscription-change";

/** Dispatched after this device's server row is created or healed. The
 * account settings section reads it to replace a stale local row. */
export const PUSH_RECONCILED_EVENT = "sf-push-reconciled";

/** This device's stored row — the DELETE target and the local match key. */
export interface StoredPushSubscription {
  id: string;
  endpoint: string;
}

export type PushReconcileOutcome =
  | "unsupported"
  | "not-owner"
  | "no-registration"
  | "blocked"
  | "needs-opt-in"
  | "healthy"
  | "healed";

export interface PushReconcileResult {
  outcome: PushReconcileOutcome;
  /** Present for `healthy` and `healed` — the row matching this device. */
  subscription?: StoredPushSubscription;
}

/** Read the marker without letting a blocked storage surface fail a caller. */
export function readPushOwner(): string | null {
  try {
    const value = localStorage.getItem(PUSH_OWNER_KEY);
    return value ? value : null;
  } catch {
    return null;
  }
}

/** Record an explicit opt-in by `userID`; storage failure is not fatal, it
 * only disables background healing for this profile. */
export function rememberPushOwner(userID: string): void {
  try {
    localStorage.setItem(PUSH_OWNER_KEY, userID);
  } catch {
    /* storage unavailable — the explicit subscription still stands */
  }
}

/** Clear the opt-in record on an explicit unsubscribe or a dismissed prompt. */
export function forgetPushOwner(): void {
  try {
    localStorage.removeItem(PUSH_OWNER_KEY);
  } catch {
    /* storage unavailable — nothing to clear */
  }
}

/** Whether this environment can carry a web-push subscription at all. */
export function pushSupported(): boolean {
  return (
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    typeof Notification !== "undefined"
  );
}

/**
 * urlBase64ToUint8Array converts the URL-safe base64 public key to the
 * byte buffer PushManager.subscribe expects (the web-push standard
 * conversion; the value is the committed VAPID_PUBLIC_KEY constant).
 * Returns the underlying ArrayBuffer — a valid BufferSource.
 */
function urlBase64ToUint8Array(base64String: string): ArrayBuffer {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const buffer = new ArrayBuffer(raw.length);
  const output = new Uint8Array(buffer);
  for (let i = 0; i < raw.length; i++) {
    output[i] = raw.charCodeAt(i);
  }
  return buffer;
}

/** The strict create body — toJSON() also carries `expirationTime`, which the
 * endpoint rejects as an unknown field. */
function subscriptionPayload(
  subscription: PushSubscription,
): PushSubscriptionCreate {
  const { endpoint, keys } = subscription.toJSON() as PushSubscriptionCreate;
  return { endpoint, keys };
}

/**
 * Create this browser's subscription and store its server row. The caller
 * owns the user gesture that granted permission and records the owner
 * marker after re-checking the live session; this helper never writes the
 * marker, so a session switch during the permission prompt cannot leave a
 * stale owner behind.
 */
export async function subscribePush(
  opts?: RequestOptions,
): Promise<StoredPushSubscription> {
  const registration = await navigator.serviceWorker.getRegistration();
  if (!registration) {
    throw new Error("no service worker registration");
  }
  const subscription = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(VAPID_PUBLIC_KEY),
  });
  const payload = subscriptionPayload(subscription);
  const { id } = await createPushSubscription(
    { ...payload, takeover: true },
    opts,
  );
  return { id, endpoint: payload.endpoint };
}

/**
 * Release this browser's push ownership before the session ends. The local
 * subscription is unsubscribed so the push service stops delivering to this
 * browser, and the server row is deleted when the still-valid session can
 * find it. Cleanup is best-effort: a failure must never block sign-out, and
 * the marker is cleared either way so a later account cannot adopt the
 * previous account's device.
 */
export async function releasePushOwnership(): Promise<void> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 2_000);
  const signal = requestSignal(controller);
  try {
    if (!pushSupported()) return;
    const registration = await navigator.serviceWorker.getRegistration();
    const current = await registration?.pushManager.getSubscription();
    if (!current) return;

    const endpoint = subscriptionPayload(current).endpoint;
    try {
      const { subscriptions } = await listPushSubscriptions({ signal });
      const match = subscriptions.find((row) => row.endpoint === endpoint);
      if (match) await deletePushSubscription(match.id, { signal });
    } catch {
      /* the local unsubscribe below still cuts delivery */
    }
    try {
      await current.unsubscribe();
    } catch {
      /* the server row delete above is the durable cleanup */
    }
  } catch {
    /* best-effort — sign-out must proceed */
  } finally {
    clearTimeout(timer);
    forgetPushOwner();
  }
}

/**
 * Heal one account's push state on this browser:
 *  - an existing local subscription with no server row is POSTed back;
 *  - an existing local subscription whose row is present is healthy;
 *  - a missing local subscription is only reported, never recreated — the
 *    shell prompts instead, because the browser may have dropped it after a
 *    deliberate block or a site-data reset.
 *
 * The marker gate and the no-subscription branch are load-bearing: the marker
 * keeps a shared device from even attempting a heal for an account that did
 * not opt in, and the heal POST omits takeover, so the server refuses a
 * foreign endpoint (409 → not-owner) even if a stale tab slips past the
 * marker.
 */
export async function reconcilePushSubscription(
  userID: string,
  opts?: RequestOptions,
): Promise<PushReconcileResult> {
  if (!pushSupported()) {
    return { outcome: "unsupported" };
  }

  const owner = readPushOwner();
  const registration = await navigator.serviceWorker.getRegistration();
  if (!registration) {
    return { outcome: "no-registration" };
  }
  const current = await registration.pushManager.getSubscription();

  if (!current) {
    if (owner !== userID) {
      return { outcome: "not-owner" };
    }
    return Notification.permission === "denied"
      ? { outcome: "blocked" }
      : { outcome: "needs-opt-in" };
  }

  const payload = subscriptionPayload(current);
  if (owner !== userID) {
    if (owner !== null) {
      return { outcome: "not-owner" };
    }
    // No marker yet: the server row is the evidence that this account
    // already enabled this device. A different account's row never matches,
    // so adoption cannot cross accounts.
    const { subscriptions } = await listPushSubscriptions(opts);
    const match = subscriptions.find(
      (row) => row.endpoint === payload.endpoint,
    );
    if (!match) {
      return { outcome: "not-owner" };
    }
    rememberPushOwner(userID);
    return {
      outcome: "healthy",
      subscription: { id: match.id, endpoint: match.endpoint },
    };
  }

  const { subscriptions } = await listPushSubscriptions(opts);
  const match = subscriptions.find((row) => row.endpoint === payload.endpoint);
  if (match) {
    return {
      outcome: "healthy",
      subscription: { id: match.id, endpoint: match.endpoint },
    };
  }

  try {
    const { id } = await createPushSubscription(payload, opts);
    return {
      outcome: "healed",
      subscription: { id, endpoint: payload.endpoint },
    };
  } catch (err) {
    if (
      err instanceof ApiError &&
      err.status === 409 &&
      err.type === "/problems/conflict"
    ) {
      // The server still sees the endpoint under another account: this
      // profile's marker is stale. Drop it so the next wake-up takes the
      // no-marker path instead of retrying the heal forever.
      forgetPushOwner();
      return { outcome: "not-owner" };
    }
    throw err;
  }
}
