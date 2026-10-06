/**
 * Favorite toggle — the heart button on the
 * blog/project detail pages.
 *
 * Attributes: kind ("blog-posts" | "projects") and content-id (the opaque
 * content identifier). The element owns the whole interaction:
 *  - renders NOTHING for anonymous visitors (server authorization is the
 *    real gate; the frontend only hides the affordance),
 *  - loads the favorite status when it first becomes authenticated for the
 *    current kind:id pair (the _loadedKey latch stops revalidate's session
 *    re-set from refetching),
 *  - toggles with CONFIRMED updates: pending disables the button, success
 *    flips the heart, failure keeps the state and shows the Greek error
 *    inline (role="alert"). No optimistic flip to roll back. Clicks ride
 *    the shared trailing-edge debounce: the
 *    button stays enabled during the window (clicks update the desired
 *    target, last one wins), the request phase disables it — a click-play
 *    burst delivers at most one request, a net-zero burst none.
 *  - a 403 (stale in-memory CSRF token after a cross-tab re-sign-in) heals
 *    through a silent session revalidate, same as the account page.
 *
 * The heart states are the shared SVG pair (icons.heart / icons.heartFilled)
 * — same path, same 24×24 box, so the heart never changes size when it
 * flips. Decorative
 * (aria-hidden); the accessible name comes from the catalog aria-label.
 */

import { consume } from "@lit/context";
import { html, LitElement, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { ApiError } from "@shared/api/client.js";
import { el } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";
import { icons } from "@shared/ui/icons.js";
import {
  FAVORITE_CHANGED_EVENT,
  type FavoriteChangedDetail,
} from "@shared/events.js";
import { ToggleDebouncer } from "@shared/utils/toggle-debouncer.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { buttonClass } from "@shared/ui/controls.js";

import {
  addFavorite,
  favoriteStatus,
  removeFavorite,
} from "../data-access/favorites-api.js";
import type { FavoriteKind } from "../data-access/types.js";
import styles from "./favorite-toggle.css?inline";

@customElement("favorite-toggle")
export class FavoriteToggle extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  // No stacked @state — @consume requests updates itself.
  // Optional: outside a <session-provider> the field stays undefined.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The favoritable content kind (the URL segment name). */
  @property() kind: FavoriteKind = "blog-posts";

  /** The opaque content identifier. */
  @property({ attribute: "content-id" }) contentId = "";

  @state() private _favorited = false;
  @state() private _statusLoaded = false;
  @state() private _pending = false;
  @state() private _error = "";

  /** The desired target while the debounce window is open (null = idle).
   * Not reactive by itself — the window state re-renders via requestUpdate
   * (it only styles the button, the confirmed state stays the source of
   * truth for aria-pressed). */
  private _desired: boolean | null = null;

  /** The shared trailing-edge debouncer. */
  private readonly _debouncer = new ToggleDebouncer();

  /** The "kind:id" pair the status was loaded for — reloading only happens
   * for a NEW pair or a re-authentication, never on session re-renders. */
  private _loadedKey = "";

  /** In-flight status request — aborted on teardown or a new pair. */
  private _abort: AbortController | null = null;

  disconnectedCallback() {
    this._debouncer.cancel();
    this._desired = null; // drop any open-window target — a re-added
    // instance must not inherit a stale is-pending state
    this._abort?.abort();
    this._abort = null;
    super.disconnectedCallback();
  }

  updated(changed: PropertyValues) {
    super.updated(changed);
    const key = `${this.kind}:${this.contentId}`;
    if (this._isAuthed()) {
      if (this._loadedKey !== key) {
        // A new pair: drop the previous pair's state so the refetch window
        // shows the neutral disabled heart, never the old pair's heart.
        this._favorited = false;
        this._statusLoaded = false;
        this._loadedKey = key;
        this._debouncer.cancel();
        this._desired = null;
        void this._loadStatus();
      }
    } else {
      // Signed out — drop the latch so a future re-authentication reloads
      // instead of showing a previous user's favorite state. Any open
      // debounce window dies with the session: it could otherwise deliver
      // an anonymous mutation whose error the hidden UI never shows.
      this._loadedKey = "";
      this._debouncer.cancel();
      this._desired = null;
    }
  }

  private _isAuthed(): boolean {
    return this.session?.state.status === "authenticated";
  }

  private async _loadStatus() {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this._error = "";

    try {
      const res = await favoriteStatus(this.kind, this.contentId, {
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      this._favorited = res.favorited;
      this._statusLoaded = true;
    } catch (err) {
      if (controller.signal.aborted) return;
      this._error = mapError(err);
      this._statusLoaded = true; // outline heart + the error explains
    }
  }
  /** The first click opens the debounce window: the desired target flips
   * per click (last wins), delivery happens when the stream settles. */
  private _onToggle() {
    if (this._pending || !this._isAuthed() || !this._statusLoaded) return;
    this._error = "";
    this._desired = this._desired === null ? !this._favorited : !this._desired;
    this._debouncer.schedule("favorite", () => {
      void this._deliver();
    });
    this.requestUpdate();
  }

  /** The window expired: skip net-zero bursts (even click count), else
   * send ONE mutation for the final desired target. */
  private async _deliver() {
    const target = this._desired;
    if (!this._isAuthed()) {
      // Belt-and-braces: the sign-out branch in updated() already cancels
      // the window, but a delivery that races the context update must
      // still never mutate anonymously.
      this._desired = null;
      this.requestUpdate();
      return;
    }
    if (target === null || target === this._favorited) {
      this._desired = null;
      this.requestUpdate();
      return;
    }
    this._pending = true;
    this.requestUpdate();
    try {
      if (target) {
        await addFavorite(this.kind, this.contentId);
      } else {
        await removeFavorite(this.kind, this.contentId);
      }
      this._favorited = target;
      // Confirmed success only — the detail pages sync their public count
      // indicator from this event.
      window.dispatchEvent(
        new CustomEvent<FavoriteChangedDetail>(FAVORITE_CHANGED_EVENT, {
          detail: {
            kind: this.kind,
            contentId: this.contentId,
            favorited: this._favorited,
          },
        }),
      );
    } catch (err) {
      this._error = mapError(err);
      // A 403 is the stale-CSRF ambiguity (cross-tab re-sign-in) — the
      // silent revalidate re-syncs session + token so a retry works.
      if (err instanceof ApiError && err.status === 403) {
        void this.session?.revalidate();
      }
    } finally {
      this._pending = false;
      this._desired = null;
      this.requestUpdate();
    }
  }

  render() {
    // No heart for anonymous visitors — the detail pages stay public.
    if (!this._isAuthed()) return html``;

    const busy = this._desired !== null || this._pending;
    return html`
      <button
        type="button"
        class=${buttonClass({
          icon: true,
          variant: "ghost",
          extra: `favorite-toggle${busy ? " is-pending" : ""}`,
        })}
        data-favorited=${this._favorited ? "true" : "false"}
        aria-pressed=${this._favorited ? "true" : "false"}
        aria-busy=${busy ? "true" : "false"}
        aria-label=${
          this._favorited ? el.favorites.removeAria : el.favorites.addAria
        }
        ?disabled=${this._pending || !this._statusLoaded}
        @click=${this._onToggle}
      >
        <span class="heart-glyph" aria-hidden="true"
          >${this._favorited ? icons.heartFilled : icons.heart}</span
        >
      </button>
      ${
        this._error
          ? html`<p class="favorite-error" role="alert">${this._error}</p>`
          : ""
      }
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "favorite-toggle": FavoriteToggle;
  }
}
