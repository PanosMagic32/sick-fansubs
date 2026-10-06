/**
 * Email-verification page — consumes the emailed
 * verification token.
 *
 * The token arrives in the URL query (?token=…) and is read ONCE, then the
 * query is stripped via replaceState so refresh/back-forward can never
 * replay a consumed link (the reset-page pattern). The page auto-submits
 * once the session context has settled — the link IS the action, there is
 * nothing to type, but a session-bearing cold load needs bootstrap's CSRF
 * token before the POST (see updated()).
 *
 * Deliberately NOT auth-guarded: the emailed link must work while a live
 * session exists too (the server applies CSRF whenever a session resolves —
 * the shared transport attaches the token automatically via needsCSRF).
 * The page is EXEMPT from the app-shell's forced-change redirect (the
 * reset-page precedent — a self-service flow must never strand on the
 * forced-change gate).
 *
 * Success redirects immediately (the reset-page shape): a signed-in user
 * lands on /account with the success banner through sessionStorage; an
 * anonymous user lands on /sign-in with the same banner. The 429 cooldown
 * offers a manual retry button with the ticking label.
 */

import { consume } from "@lit/context";
import { LitElement, html, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { verifyEmail } from "@features/auth/data-access/auth-api.js";
import { redirect } from "@core/router.js";
import { ApiError } from "@shared/api/client.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/error-banner.js";

import styles from "./verify-page.css?inline";
import authFormStyles from "./ui/form.css?inline";
import { mapSubmitError, RetryCooldownController } from "./ui/submit-errors.js";

/** sessionStorage key for the post-verify success banner on /account and
 * /sign-in (both consume it once — read + remove). */
export const VERIFY_SUCCESS_STORAGE_KEY = "sf-verify-success";

@customElement("verify-email-page")
export class VerifyEmailPage extends LitElement {
  // Optional: outside a <session-provider> the field stays undefined.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The token read from the URL on connect ("" = missing → invalid state). */
  @state() private _token = "";
  /** "pending" | "invalid" | "rate-limited" | "failed" — success redirects away. */
  @state() private _status: "pending" | "invalid" | "rate-limited" | "failed" =
    "pending";
  @state() private _error = "";
  /** 429 Retry-After cooldown — owns the ticking + disconnect cancel. */
  private readonly _cooldown = new RetryCooldownController(this);

  /** Auto-submit latch — the link fires exactly one request per mount. */
  private _submitted = false;

  /** One-shot: the invalid-token verdict was focused after its first render. */
  private _invalidFocused = false;

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(authFormStyles),
    unsafeCSS(styles),
  ];

  // ── Lifecycle ──────────────────────────────────────────────────

  connectedCallback() {
    super.connectedCallback();
    // Read the token once and strip the query immediately: refresh and
    // back/forward must not replay a consumed link (the reset-page
    // history/URL patch rule — this replaceState pairs with the read).
    const token =
      new URLSearchParams(window.location.search).get("token") ?? "";
    if (token) {
      const url = new URL(window.location.href);
      // Delete only the token — other query params (and any hash) survive.
      url.searchParams.delete("token");
      history.replaceState(
        history.state,
        "",
        url.pathname + url.search + url.hash,
      );
    }
    this._token = token;
    if (!token) {
      this._status = "invalid";
    }
  }

  /**
   * Auto-submit once the session context knows its state. A cold load of
   * an emailed link carries the session cookie but no in-memory CSRF token
   * until bootstrap resolves; posting earlier would omit the header the
   * server requires whenever a session exists (403 — with the token still
   * valid). Outside a provider the field stays undefined and the submit
   * proceeds immediately.
   */
  protected updated() {
    // The invalid verdict mounts with its text; focus it once it exists so
    // the terminal state is announced (the house status shape).
    if (this._status === "invalid" && !this._invalidFocused) {
      this._invalidFocused = true;
      this.shadowRoot?.querySelector<HTMLElement>(".invalid-note")?.focus();
    }
    if (this._submitted || !this._token || this._status !== "pending") {
      return;
    }
    if (this.session?.state.status === "initializing") return;
    void this._submit();
  }

  // ── Actions ────────────────────────────────────────────────────

  private async _submit() {
    if (this._submitted || this._cooldown.left > 0) return;
    this._submitted = true;
    this._status = "pending";
    this._error = "";
    try {
      await verifyEmail({ token: this._token });
      // The account is verified. Hand the success banner through
      // sessionStorage and redirect (the reset-page shape).
      try {
        sessionStorage.setItem(VERIFY_SUCCESS_STORAGE_KEY, "1");
      } catch {
        // Storage unavailable (private mode) — the banner is lost, the
        // redirect still works.
      }
      await this.session?.revalidate();
      redirect(
        this.session?.state.status === "authenticated"
          ? "/account"
          : "/sign-in",
      );
    } catch (err) {
      const { banner, retryAfterSeconds } = mapSubmitError(err, {
        // No form fields — every violation is a banner.
        serverField: (_field: string): _field is "none" => false,
      });
      if (retryAfterSeconds !== undefined && retryAfterSeconds > 0) {
        // 429 — a manual retry with the ticking cooldown label.
        this._status = "rate-limited";
        this._cooldown.start(retryAfterSeconds);
        this._submitted = false; // the retry button re-arms the submit
      } else if (
        err instanceof ApiError &&
        err.status === 422 &&
        err.type === "/problems/auth/verification-token-invalid"
      ) {
        // Terminal: the token is bad/expired/used — nothing to retry.
        this._status = "invalid";
      } else {
        // Transient (500, network, timeout, stale-CSRF): the token is
        // still valid — the mapped banner plus a retry, not "invalid".
        this._status = "failed";
        this._submitted = false;
        // A 403 means the server saw a session the client's in-memory
        // state/CSRF did not: re-bootstrap so the retry can succeed (the
        // other CSRF'd surfaces' 403 heal).
        if (err instanceof ApiError && err.status === 403) {
          void this.session?.retryBootstrap();
        }
      }
      this._error = banner;
    }
  }

  /** Manual retry after the 429 window. */
  private _onRetry() {
    void this._submit();
  }

  // ── Render ─────────────────────────────────────────────────────

  render() {
    const heading = html`<h1 id="verify-heading">${el.auth.verifyTitle}</h1>`;

    if (this._status === "invalid") {
      return html`
        <section class="auth-card verify-page" aria-labelledby="verify-heading">
          ${heading}
          <p class="invalid-note" role="status" tabindex="-1">
            ${el.auth.verifyTokenInvalid}
          </p>
          <p class="footer-link">
            <a href="/sign-in">${el.auth.backToSignIn}</a>
          </p>
        </section>
      `;
    }

    if (this._status === "rate-limited") {
      return html`
        <section class="auth-card verify-page" aria-labelledby="verify-heading">
          ${heading}
          <error-banner .message=${this._error}></error-banner>
          <button
            type="button"
            class="button button--primary retry-btn"
            ?disabled=${this._cooldown.left > 0}
            @click=${this._onRetry}
          >
            ${
              this._cooldown.left > 0
                ? retryAfterMessage(this._cooldown.left)
                : el.ui.retry
            }
          </button>
        </section>
      `;
    }

    if (this._status === "failed") {
      return html`
        <section class="auth-card verify-page" aria-labelledby="verify-heading">
          ${heading}
          <error-banner .message=${this._error}></error-banner>
          <button
            type="button"
            class="button button--primary retry-btn"
            @click=${this._onRetry}
          >
            ${el.ui.retry}
          </button>
        </section>
      `;
    }

    // Pending — the auto-submit is in flight.
    return html`
      <section class="auth-card verify-page" aria-labelledby="verify-heading">
        ${heading}
        <p class="intro-note">${el.auth.verifyPending}</p>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "verify-email-page": VerifyEmailPage;
  }
}
