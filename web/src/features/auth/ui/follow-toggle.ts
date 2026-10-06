/**
 * Follow toggle — the bell button on the
 * blog/project detail pages, driving the content_follows rows.
 *
 * Attributes: kind ("blog-posts" | "projects") and content-id (the opaque
 * content identifier). The element owns the whole interaction:
 *  - renders NOTHING for anonymous visitors (server authorization is the
 *    real gate; the frontend only hides the affordance) and for an empty
 *    content-id,
 *  - loads the follow status when it first becomes authenticated for the
 *    current kind:id pair (the _loadedKey latch stops revalidate's session
 *    re-set from refetching),
 *  - toggles with CONFIRMED updates: pending disables the button, success
 *    flips the bell, failure keeps the state and shows the Greek error
 *    inline (role="alert"). No optimistic flip to roll back. Unlike the
 *    favorite heart, a follow is NOT a like — the shared debounce covers
 *    likes only, so clicks simply no-op while
 *    a request is in flight,
 *  - a 403 (stale in-memory CSRF token after a cross-tab re-sign-in) heals
 *    through a silent session revalidate, same as the favorite toggle.
 *
 * The bell is the shared SVG glyph pair (icons.bell / icons.bellFilled) —
 * same geometry, same box, so the bell never changes size when it flips
 * (the heart pair's contract). Decorative (aria-hidden); the accessible
 * name comes from the catalog aria-label pair
 * (el.follows.followAria/unfollowAria), aria-pressed reports the state.
 * Follows drive no public counters, so the element dispatches no event.
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
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { buttonClass } from "@shared/ui/controls.js";

import {
  addFollow,
  followStatus,
  removeFollow,
  type FollowKind,
} from "../data-access/follows-api.js";
import styles from "./follow-toggle.css?inline";

@customElement("follow-toggle")
export class FollowToggle extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  // No stacked @state — @consume requests updates itself.
  // Optional: outside a <session-provider> the field stays undefined.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The followable content kind (the URL segment name). */
  @property() kind: FollowKind = "blog-posts";

  /** The opaque content identifier. */
  @property({ attribute: "content-id" }) contentId = "";

  @state() private _following = false;
  @state() private _statusLoaded = false;
  @state() private _pending = false;
  @state() private _error = "";

  /** The "kind:id" pair the status was loaded for — reloading only happens
   * for a NEW pair or a re-authentication, never on session re-renders. */
  private _loadedKey = "";

  /** In-flight status request — aborted on teardown or a new pair. */
  private _abort: AbortController | null = null;

  disconnectedCallback() {
    this._abort?.abort();
    this._abort = null;
    super.disconnectedCallback();
  }

  updated(changed: PropertyValues) {
    super.updated(changed);
    const key = `${this.kind}:${this.contentId}`;
    if (this._isAuthed() && this.contentId) {
      if (this._loadedKey !== key) {
        // A new pair: drop the previous pair's state so the refetch window
        // shows the neutral disabled bell, never the old pair's bell.
        this._following = false;
        this._statusLoaded = false;
        this._loadedKey = key;
        void this._loadStatus();
      }
    } else {
      // Signed out (or no content id) — drop the latch so a future
      // re-authentication reloads instead of showing a previous user's
      // follow state.
      this._loadedKey = "";
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
      const res = await followStatus(this.kind, this.contentId, {
        signal: controller.signal,
      });
      if (controller.signal.aborted) return;
      this._following = res.following;
      this._statusLoaded = true;
    } catch (err) {
      if (controller.signal.aborted) return;
      this._error = mapError(err);
      this._statusLoaded = true; // outline bell + the error explains
    }
  }

  private async _onToggle() {
    if (this._pending || !this._isAuthed() || !this._statusLoaded) return;
    this._error = "";
    this._pending = true;
    this.requestUpdate();
    const target = !this._following;
    try {
      if (target) {
        await addFollow(this.kind, this.contentId);
      } else {
        await removeFollow(this.kind, this.contentId);
      }
      // Confirmed success only — no event: follows drive no public
      // counters (unlike the favorite heart's count-sync event).
      this._following = target;
    } catch (err) {
      this._error = mapError(err);
      // A 403 is the stale-CSRF ambiguity (cross-tab re-sign-in) — the
      // silent revalidate re-syncs session + token so a retry works.
      if (err instanceof ApiError && err.status === 403) {
        void this.session?.revalidate();
      }
    } finally {
      this._pending = false;
      this.requestUpdate();
    }
  }

  render() {
    // No bell for anonymous visitors (the detail pages stay public) and
    // none without a content id.
    if (!this._isAuthed() || !this.contentId) return html``;

    return html`
      <button
        type="button"
        class=${buttonClass({
          icon: true,
          variant: "ghost",
          extra: "follow-toggle",
        })}
        data-following=${this._following ? "true" : "false"}
        aria-pressed=${this._following ? "true" : "false"}
        aria-busy=${this._pending ? "true" : "false"}
        aria-label=${
          this._following ? el.follows.unfollowAria : el.follows.followAria
        }
        ?disabled=${this._pending || !this._statusLoaded}
        @click=${this._onToggle}
      >
        <span class="bell-glyph" aria-hidden="true"
          >${this._following ? icons.bellFilled : icons.bell}</span
        >
      </button>
      ${
        this._error
          ? html`<p class="follow-error" role="alert">${this._error}</p>`
          : ""
      }
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "follow-toggle": FollowToggle;
  }
}
