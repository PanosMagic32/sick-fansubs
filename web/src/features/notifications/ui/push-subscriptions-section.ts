/**
 * Push subscriptions section of <push-settings> — this device's capability
 * half: the subscribe button and its double-permission flow (our own confirm
 * dialog first, then the browser prompt), the off switch that deletes the
 * server row and unsubscribes locally, the self-test press, and the worker
 * relay that silently resubscribes after a push-service rotation.
 *
 * A reactive section controller: the element owns the shadow-root markup and
 * reads this section's state; the section owns the subscription state, the
 * handlers, and the lifecycle (the window-message listener and the load's
 * abort controller). Every state change asks the host for a re-render.
 *
 * The capability is refined once loaded: no service-worker registration
 * states the unavailable line through the element (a subscribe flow without
 * a registration would hang on `serviceWorker.ready` forever, and Vite dev
 * has no worker), and a denied permission blocks re-prompting from the page,
 * so the denied state renders an explanation instead of the enable flow.
 */

import { type ReactiveController, type ReactiveControllerHost } from "lit";

import type { SessionState } from "@features/auth/data-access/session.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { VAPID_PUBLIC_KEY } from "@shared/config/vapid.js";
import { mapError } from "@shared/utils/format.js";
import { isIOS, isStandalone } from "@shared/utils/platform.js";

import {
  createPushSubscription,
  deletePushSubscription,
  getNotificationPreferences,
  listPushSubscriptions,
  sendTestPush,
} from "../data-access/push-api.js";
import type {
  NotificationPreference,
  PushSubscriptionItem,
  PushTestResult,
} from "../data-access/types.js";

/** The worker's relay message type (web/public/sw.js — pinned by
 * push-artifacts.test.ts; keep in sync). */
const SUBSCRIPTION_CHANGE_MESSAGE = "sf-push-subscription-change";

type SectionStatus = "loading" | "ready" | "error";

/** The local device's stored row — id (for DELETE) + endpoint (for
 * matching). The create response carries only {id}; the endpoint comes
 * from the browser's own subscription, never a fabricated createdAt. */
interface LocalSubscription {
  id: string;
  endpoint: string;
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

/**
 * Host contract for the subscriptions section: the consumed session's state,
 * the element's shadow root (the confirm dialog is queried there), and the
 * preferences half.
 */
export interface PushSubscriptionsHost extends ReactiveControllerHost {
  /** The consumed session's state — the one-shot latch waits for the
   * authenticated status. */
  readonly sessionState: SessionState | undefined;
  /** The element's shadow root — the confirm dialog is queried there. */
  readonly shadowRoot: ShadowRoot | null;
  /** The preferences half: its save flag joins the shared busy predicate,
   * and the load hands it the fetched preferences. */
  readonly preferences: PushPreferencesSurface;
}

/** The narrow surface the preferences half reads from this section. */
export interface PushSubscriptionsSurface {
  /** The shared error banner (both halves write it). */
  get error(): string;
  set error(value: string);
  /** The in-flight action, for the checkbox gating. */
  get pending(): "none" | "subscribe" | "unsubscribe";
  /** Reload both halves (the error banner's retry control). */
  retry(): void;
}

/** The narrow surface this section reads from the preferences half. */
export interface PushPreferencesSurface {
  /** A save fan-out in flight — joins the shared busy predicate. */
  get savePending(): boolean;
  /** Hand the fetched preferences to the half that renders them. */
  applyLoaded(preferences: NotificationPreference[]): void;
}

export class PushSubscriptionsSection implements ReactiveController {
  /** The load state — the element renders one branch per value. */
  status: SectionStatus = "loading";

  /** The local device's server row (matched by endpoint), if any. */
  _subscription: LocalSubscription | null = null;

  /** "none" | the in-flight action ("subscribe" | "unsubscribe"). */
  private _pending: "none" | "subscribe" | "unsubscribe" = "none";

  /** The in-flight action, read by the preferences half's gating. */
  get pending(): "none" | "subscribe" | "unsubscribe" {
    return this._pending;
  }

  /** The shared error banner (the preferences save flow writes it too). */
  private _error = "";

  /** The shared error banner; a write repaints both halves. */
  get error(): string {
    return this._error;
  }
  set error(value: string) {
    this._error = value;
    this.refresh();
  }

  /** Notification.permission at last read — "denied" blocks re-prompting. */
  _denied = false;

  /** Whether the iOS install hint applies (set by the authenticated latch). */
  _iosHint = false;

  /** Set after the load refines capability with the real registration: no
   * service-worker registration → the element states the unavailable line
   * instead of a working control. */
  _hidden = false;

  /** The self-test press: its press state and the last result line.
   * `_testResult` is null until a press answers and is cleared when the
   * device is switched off, so a re-enable never shows a stale result. */
  _testPending: "none" | "sending" = "none";
  _testResult: string | null = null;

  /** A relay-triggered resubscribe in flight — it joins `busy` so a second
   * rotation message cannot start a parallel subscribe. */
  private _relayPending = false;

  private readonly _host: PushSubscriptionsHost;
  private _started = false;
  private _abort: AbortController | null = null;

  constructor(host: PushSubscriptionsHost) {
    this._host = host;
    host.addController(this);
  }

  hostConnected() {
    window.addEventListener("message", this._onMessage);
  }

  hostDisconnected() {
    window.removeEventListener("message", this._onMessage);
    this._abort?.abort();
  }

  /** The one-shot latch: the first authenticated host update sets the iOS
   * hint and loads (revalidate re-sets the session object without a status
   * change, so this must not re-run on every update). */
  hostUpdated() {
    if (this._host.sessionState?.status === "authenticated" && !this._started) {
      this._started = true;
      this._iosHint = isIOS() && !isStandalone();
      this.refresh();
      void this.load();
    }
  }

  /** One busy predicate for every control in the section (a press in flight
   * must not race the off switch, and vice versa). */
  get busy(): boolean {
    return (
      this._pending !== "none" ||
      this._testPending !== "none" ||
      this._relayPending ||
      this._host.preferences.savePending
    );
  }

  get supported(): boolean {
    return "serviceWorker" in navigator && "PushManager" in window;
  }

  /** Ask the host for a re-render — the controller's own state does not
   * schedule one. */
  private refresh() {
    this._host.requestUpdate();
  }

  /* ── Worker relay (best-effort resubscribe) ─────────────────────── */

  private _onMessage = (event: MessageEvent) => {
    // Trust only our own origin's messages (a third-party iframe must not
    // trigger POSTs).
    if (event.origin !== location.origin) return;
    if (
      event.data?.type === SUBSCRIPTION_CHANGE_MESSAGE &&
      !this.busy &&
      typeof Notification !== "undefined" &&
      Notification.permission === "granted"
    ) {
      // The push service rotated the subscription — the local match is now
      // stale, so resubscribe REGARDLESS of _subscription (the exact
      // scenario the relay exists for). The in-flight flag joins `busy` so
      // two rotation messages cannot start two subscribes.
      this._relayPending = true;
      this.refresh();
      void this._subscribe()
        .catch(() => {
          /* best-effort — the next visit retries */
        })
        .finally(() => {
          this._relayPending = false;
          this.refresh();
        });
    }
  };

  /* ── Data ───────────────────────────────────────────────────────── */

  async load() {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this.status = "loading";
    this._error = "";
    this.refresh();
    try {
      const [list, prefs] = await Promise.all([
        listPushSubscriptions({ signal: requestSignal(controller) }),
        getNotificationPreferences({ signal: requestSignal(controller) }),
      ]);
      if (controller.signal.aborted) return;
      this._host.preferences.applyLoaded(prefs.preferences);
      // Capability refinement: no SW registration → the unavailable line.
      // This also covers Vite dev, where the SW never registers and
      // `serviceWorker.ready` would hang a subscribe flow.
      const registration = await navigator.serviceWorker.getRegistration();
      if (!registration) {
        this._hidden = true;
        this.status = "ready";
        this.refresh();
        return;
      }
      this._subscription = await this._matchLocalSubscription(
        registration,
        list.subscriptions,
      );
      this._denied = Notification.permission === "denied";
      this.status = "ready";
      this.refresh();
    } catch (err) {
      if (controller.signal.aborted) return;
      this._error = mapError(err);
      this.status = "error";
      this.refresh();
    }
  }

  /** Retry after a failed load: re-arm the one-shot latch; the update this
   * schedules performs the single reload. Calling load() here as well would
   * double-fetch — the re-armed latch fires its own load and aborts the
   * first. */
  retry() {
    this._started = false;
    this.refresh();
  }

  /** Match the server rows against the browser's CURRENT registration —
   * the list is per-user (all devices); only the endpoint match is THIS
   * device. A subscribed-but-unmatched device renders the enable flow
   * again (resubscribing heals it). */
  private async _matchLocalSubscription(
    registration: ServiceWorkerRegistration,
    rows: PushSubscriptionItem[],
  ): Promise<LocalSubscription | null> {
    try {
      const current = await registration.pushManager.getSubscription();
      if (!current) return null;
      const match = rows.find((r) => r.endpoint === current.endpoint);
      return match ? { id: match.id, endpoint: match.endpoint } : null;
    } catch {
      return null;
    }
  }

  /* ── Subscribe / unsubscribe ────────────────────────────────────── */

  _onEnableClick = () => {
    const dialog = this._host.shadowRoot?.querySelector("dialog");
    // showModal gives the focus trap + restore + Escape handling; the
    // attribute fallback covers a DOM without the method.
    if (typeof dialog?.showModal === "function") {
      dialog.showModal();
    } else {
      dialog?.setAttribute("open", "");
    }
  };

  _onDialogConfirm = () => {
    if (this.busy) return; // a press in flight must not race the flow
    this._host.shadowRoot?.querySelector("dialog")?.close();
    void this._requestPermissionAndSubscribe();
  };

  _onDialogCancel = () => {
    this._host.shadowRoot?.querySelector("dialog")?.close();
  };

  /** The gesture-triggered flow: browser permission FIRST (the double-
   * permission pattern's second half), then subscribe + store. */
  private async _requestPermissionAndSubscribe() {
    this._pending = "subscribe";
    this._error = "";
    this.refresh();
    try {
      const permission = await Notification.requestPermission();
      if (permission === "denied") {
        this._denied = true;
        // The ONLY non-API failure — a user choice, not a wire problem:
        // the pushFailure copy fits better than a network mapping.
        this._error = el.notifications.pushFailure;
        return;
      }
      if (permission !== "granted") {
        this._error = el.notifications.pushFailure;
        return;
      }
      await this._subscribe();
      this._denied = false;
      this._error = "";
    } catch (err) {
      this._error = mapError(err);
    } finally {
      this._pending = "none";
      this.refresh();
    }
  }

  private async _subscribe() {
    // getRegistration, not `serviceWorker.ready`: a missing registration
    // must FAIL the flow (ready would hang forever — dev has no SW).
    const registration = await navigator.serviceWorker.getRegistration();
    if (!registration) {
      throw new Error("no service worker registration");
    }
    const sub = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(VAPID_PUBLIC_KEY),
    });
    // The browser's toJSON() also carries expirationTime (null) — the
    // strict-JSON endpoint rejects unknown fields, so send exactly the
    // accepted shape.
    const { endpoint, keys } = sub.toJSON() as {
      endpoint: string;
      keys: { p256dh: string; auth: string };
    };
    const { id } = await createPushSubscription({ endpoint, keys });
    this._subscription = { id, endpoint };
    this.refresh();
  }

  _onDisableClick = async () => {
    if (this.busy || !this._subscription) {
      return;
    }
    this._pending = "unsubscribe";
    this._error = "";
    this.refresh();
    try {
      await deletePushSubscription(this._subscription.id);
      const registration = await navigator.serviceWorker.getRegistration();
      const current = await registration?.pushManager.getSubscription();
      await current?.unsubscribe();
      this._subscription = null;
      this._testResult = null;
    } catch (err) {
      this._error = mapError(err);
    } finally {
      this._pending = "none";
      this.refresh();
    }
  };

  /**
   * The self-test press: one real push to this user's devices through the
   * server. The result line speaks about THIS device where the report names
   * its row — a healthy desktop must not hide a phone the service refused.
   * Every press refreshes this device's row: the send cleans up endpoints
   * the push service no longer knows, so the row may have just disappeared.
   * Wire failures map through mapError like every other press here.
   */
  _onTestClick = async () => {
    if (this.busy) return;
    this._testPending = "sending";
    this._testResult = null;
    this._error = "";
    this.refresh();
    try {
      const res = await sendTestPush();
      // Build the line BEFORE the refresh: a removed row clears the subscribed
      // state, and the line about that removal must survive it.
      const line = this._testResultLine(res);
      await this._refreshSubscription();
      this._testResult = line;
    } catch (err) {
      this._error = mapError(err);
    } finally {
      this._testPending = "none";
      this.refresh();
    }
  };

  /** The press outcome, phrased around THIS device when the report names its
   * row. An accepted endpoint means the PUSH SERVICE took the message — which
   * is as far as the server can see, so the copy must not promise that a
   * notification appeared. A missing status separates "the push service
   * refused" from "we never reached it" (the same report, two different
   * remedies). */
  private _testResultLine(res: PushTestResult): string {
    const own = res.endpoints.find((e) => e.id === this._subscription?.id);
    if (own && !own.accepted) {
      if (own.removed) return el.notifications.pushTestDeviceRemoved;
      return own.status === undefined
        ? el.notifications.pushTestDeviceUnreachable
        : el.notifications.pushTestDeviceRejected;
    }
    return res.delivered > 0
      ? el.notifications.pushTestSent
      : el.notifications.pushTestNone;
  }

  /**
   * Re-read the server's row for THIS device after a test press: the send
   * removes endpoints the push service no longer knows, so the row may have
   * just disappeared — and a subscribed state that survived its own row would
   * offer a dead switch. Deliberately quiet — no loading state, no error
   * banner (the press's own result line already speaks); the row simply
   * disappears when it is gone.
   */
  private async _refreshSubscription() {
    if (!this._subscription) return;
    try {
      const { subscriptions } = await listPushSubscriptions();
      if (!subscriptions.some((s) => s.id === this._subscription?.id)) {
        this._subscription = null;
        this._testResult = null;
        this.refresh();
      }
    } catch {
      /* best-effort freshness — the press's own outcome already showed */
    }
  }
}
