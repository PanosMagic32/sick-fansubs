/**
 * Global push lifecycle — the shell-mounted element that keeps this browser's
 * push row healthy on every route, not only while the account page's settings
 * section is open.
 *
 * It reconciles on authenticated bootstrap, on `online`, on a visible tab, and
 * when the worker reports a `pushsubscriptionchange`. A browser subscription
 * that is missing from the server is POSTed back; a browser subscription that
 * disappeared is never recreated silently. Instead the element shows a slim
 * prompt, because the browser may have dropped it after a deliberate block or
 * a site-data reset. Both paths are gated by the per-device owner marker in
 * `data-access/push-subscription.ts`, so a shared browser cannot hand one
 * account's notifications to another.
 */

import { consume } from "@lit/context";
import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import controlStyles from "@shared/ui/controls.css?inline";
import { mapError } from "@shared/utils/format.js";

import {
  forgetPushOwner,
  PUSH_RECONCILED_EVENT,
  PUSH_SUBSCRIPTION_CHANGE_MESSAGE,
  pushSupported,
  reconcilePushSubscription,
  rememberPushOwner,
  type StoredPushSubscription,
  subscribePush,
} from "../data-access/push-subscription.js";
import styles from "./push-lifecycle.css?inline";

@customElement("push-lifecycle")
export class PushLifecycle extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The browser lost its local subscription while the marker still names
   * this account — only an explicit enable may recreate it. */
  @state() private _needsOptIn = false;

  /** The prompt's enable press is in flight. */
  @state() private _pending = false;

  /** A failed enable press, mapped like every other action surface. */
  @state() private _error = "";

  /** Status text that must outlive the prompt: the blocked-permission
   * guidance is announced even though the prompt itself is removed. */
  @state() private _notice = "";

  private _reconciling = false;
  private _queued = false;
  private _disconnected = false;
  private _controller: AbortController | null = null;
  private _enableController: AbortController | null = null;
  private _lastUserID: string | null = null;

  connectedCallback(): void {
    super.connectedCallback();
    this._disconnected = false;
    window.addEventListener("online", this._onOnline);
    document.addEventListener("visibilitychange", this._onVisibility);
    window.addEventListener("message", this._onMessage);
    window.addEventListener(PUSH_RECONCILED_EVENT, this._onReconciled);
  }

  disconnectedCallback(): void {
    this._disconnected = true;
    this._queued = false;
    window.removeEventListener("online", this._onOnline);
    document.removeEventListener("visibilitychange", this._onVisibility);
    window.removeEventListener("message", this._onMessage);
    window.removeEventListener(PUSH_RECONCILED_EVENT, this._onReconciled);
    this._controller?.abort();
    this._controller = null;
    this._enableController?.abort();
    this._enableController = null;
    super.disconnectedCallback();
  }

  /** First authenticated user runs the first reconcile; a later user (or a
   * sign-out) resets the prompt. Revalidate re-sets the same user and must
   * not re-run the reconcile. */
  protected updated(): void {
    const state = this.session?.state;
    if (state?.status === "authenticated") {
      if (this._lastUserID !== state.user.id) {
        this._lastUserID = state.user.id;
        this._needsOptIn = false;
        this._error = "";
        this._notice = "";
        void this._reconcile();
      }
      return;
    }
    this._lastUserID = null;
    this._needsOptIn = false;
    this._error = "";
    this._notice = "";
  }

  private _onOnline = (): void => {
    void this._reconcile();
  };

  private _onVisibility = (): void => {
    if (document.visibilityState !== "visible") return;
    void this._reconcile();
  };

  private _onMessage = (event: MessageEvent): void => {
    // Trust only our own origin's messages (a third-party iframe must not
    // trigger authenticated requests).
    if (event.origin !== location.origin) return;
    if (event.data?.type !== PUSH_SUBSCRIPTION_CHANGE_MESSAGE) return;
    void this._reconcile();
  };

  /** Another surface created or adopted this device's row — clear the prompt
   * even when this element did not run the reconcile itself. */
  private _onReconciled = (): void => {
    const hadPrompt = this._needsOptIn;
    this._needsOptIn = false;
    this._error = "";
    this._notice = "";
    // Only hand focus off when it actually sits inside the removed prompt;
    // another surface may have raised this event while the user is
    // elsewhere on the page.
    if (hadPrompt && this.shadowRoot?.activeElement) this._focusMain();
  };

  /** One reconcile at a time; wake-up events that arrive during a run queue a
   * single follow-up instead of starting a parallel one. */
  private async _reconcile(): Promise<void> {
    if (this._disconnected) return;
    const state = this.session?.state;
    if (state?.status !== "authenticated") return;
    const userID = state.user.id;

    if (this._reconciling) {
      this._queued = true;
      return;
    }
    this._reconciling = true;
    const controller = new AbortController();
    this._controller = controller;

    try {
      const result = await reconcilePushSubscription(userID, {
        signal: requestSignal(controller),
      });
      if (this._disconnected || controller.signal.aborted) return;

      // The session may have changed while the requests were out. Never let
      // a stale answer turn into a prompt for the next account.
      const current = this.session?.state;
      if (current?.status !== "authenticated" || current.user.id !== userID) {
        return;
      }

      this._needsOptIn = result.outcome === "needs-opt-in";
      this._error = "";
      this._notice =
        result.outcome === "blocked" ? el.notifications.pushDenied : "";
      if (result.outcome === "healed" && result.subscription) {
        this._announce(result.subscription);
      }
    } catch {
      /* best-effort — the next wake-up retries */
    } finally {
      if (this._controller === controller) this._controller = null;
      this._reconciling = false;
      if (this._queued && !this._disconnected) {
        this._queued = false;
        void this._reconcile();
      }
    }
  }

  private _announce(row: StoredPushSubscription): void {
    window.dispatchEvent(
      new CustomEvent<StoredPushSubscription>(PUSH_RECONCILED_EVENT, {
        detail: row,
      }),
    );
  }

  /** The prompt's enable press is the explicit opt-in: permission first, then
   * the subscription and its server row. */
  private _onEnable = async (): Promise<void> => {
    if (this._pending || this._disconnected) return;
    const state = this.session?.state;
    if (state?.status !== "authenticated") return;
    if (!pushSupported()) return;
    if (Notification.permission === "denied") {
      // The browser will not prompt again; keeping the enable control would
      // leave a dead button and a false retry message. The status region
      // still states why, and where to change it.
      this._needsOptIn = false;
      this._error = "";
      this._notice = el.notifications.pushDenied;
      this._focusMain();
      return;
    }

    const controller = new AbortController();
    this._enableController = controller;
    this._pending = true;
    this._error = "";
    try {
      const permission = await Notification.requestPermission();
      if (this._disconnected || controller.signal.aborted) return;
      if (permission === "denied") {
        this._needsOptIn = false;
        this._error = "";
        this._notice = el.notifications.pushDenied;
        this._focusMain();
        return;
      }
      if (permission !== "granted") {
        this._error = el.notifications.pushFailure;
        return;
      }
      // The permission prompt can stay open while another tab switches
      // accounts. Never start the explicit takeover with the captured id.
      const current = this.session?.state;
      if (
        current?.status !== "authenticated" ||
        current.user.id !== state.user.id
      ) {
        this._needsOptIn = false;
        this._error = "";
        this._notice = "";
        this._focusMain();
        return;
      }
      const row = await subscribePush({ signal: requestSignal(controller) });
      if (this._disconnected || controller.signal.aborted) return;
      const afterSubscribe = this.session?.state;
      if (
        afterSubscribe?.status !== "authenticated" ||
        afterSubscribe.user.id !== state.user.id
      ) {
        return;
      }
      // The marker is written only after the live session still matches, so
      // a switch during the request cannot leave a stale owner behind.
      rememberPushOwner(afterSubscribe.user.id);
      this._needsOptIn = false;
      this._notice = "";
      this._announce(row);
      this._focusMain();
    } catch (err) {
      if (!this._disconnected && !controller.signal.aborted) {
        this._error = mapError(err);
      }
    } finally {
      if (this._enableController === controller) this._enableController = null;
      this._pending = false;
    }
  };

  /** Dismissing the prompt also clears the marker: the user declined this
   * device, so later boots must not ask again until an explicit enable. */
  private _onDismiss = (): void => {
    if (this._pending) return;
    forgetPushOwner();
    this._needsOptIn = false;
    this._error = "";
    this._notice = "";
    this._focusMain();
  };

  /** The prompt's controls disappear on success or dismissal; hand focus to
   * the shell's main region instead of dropping it to the document body. */
  private _focusMain(): void {
    const root = this.getRootNode();
    if (root instanceof ShadowRoot) {
      root.querySelector<HTMLElement>("main")?.focus();
    }
  }

  protected willUpdate(): void {
    // With no prompt the host is display: contents, so the persistent live
    // region below takes no layout space.
    this.toggleAttribute("data-live-only", !this._needsOptIn);
  }

  render() {
    // The live region mounts empty on the first render and receives its text
    // after the async reconcile, so assistive tech announces the prompt.
    return html`
      <p class="sr-only" role="status">
        ${this._needsOptIn ? el.notifications.pushHealBody : this._notice}
      </p>
      ${
        this._needsOptIn
          ? html`<section
              class="push-heal"
              aria-label=${el.notifications.pushTitle}
            >
              <p class="push-heal-copy">${el.notifications.pushHealBody}</p>
              ${
                this._error
                  ? html`<p class="push-heal-error" role="alert">
                      ${this._error}
                    </p>`
                  : nothing
              }
              <div class="push-heal-actions">
                <button
                  type="button"
                  class="button button--primary"
                  aria-disabled=${this._pending ? "true" : nothing}
                  @click=${this._onEnable}
                >
                  ${
                    this._pending
                      ? el.notifications.pushEnablePending
                      : el.notifications.pushEnable
                  }
                </button>
                <button
                  type="button"
                  class="button button--secondary"
                  aria-disabled=${this._pending ? "true" : nothing}
                  @click=${this._onDismiss}
                >
                  ${el.notifications.pushHealDismiss}
                </button>
              </div>
            </section>`
          : nothing
      }
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "push-lifecycle": PushLifecycle;
  }
}
