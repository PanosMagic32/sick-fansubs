/**
 * Reset-password page — consumes the emailed reset token
 * and sets a new password.
 *
 * The token arrives in the URL query (?token=…) and is read ONCE, then the
 * query is stripped via replaceState so refresh/back-forward can never
 * replay a consumed link (and the token never lingers in the address bar).
 *
 * Deliberately NOT auth-guarded: the emailed link must work while a live
 * session exists too (the server applies CSRF whenever a session resolves —
 * the shared transport attaches the token automatically via needsCSRF). A
 * successful reset revokes every session; the page revalidates so the
 * stale authenticated state collapses, then redirects to /sign-in with a
 * success banner transported through sessionStorage (the `goto(pathname)`
 * search-dropping gotcha rules out a query param).
 *
 * The page is EXEMPT from the app-shell's forced-change redirect: a
 * self-chosen password is a legitimate exit from the forced-change gate.
 */

import { consume } from "@lit/context";
import { LitElement, html, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { resetPassword } from "@features/auth/data-access/auth-api.js";
import { redirect } from "@core/router.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/error-banner.js";

import styles from "./reset-page.css?inline";
import authFormStyles from "./ui/form.css?inline";
import { formField } from "@shared/ui/form-field.js";
import {
  focusFirstInvalid,
  mapSubmitError,
  RetryCooldownController,
} from "./ui/submit-errors.js";

/** sessionStorage key for the post-reset success banner on /sign-in. */
export const RESET_SUCCESS_STORAGE_KEY = "sf-reset-success";

/** Server-known form fields. `confirm` is client-side ONLY — the field
 * never reaches the wire (the registration precedent),
 * so `isServerField` accepts `password` alone while the page-local error
 * map can still hold a confirm error. */
type Field = "password" | "confirm";

/** Per-field error messages keyed by field name. */
type FieldErrors = Partial<Record<Field, string>>;

/** Server fields that may appear in 422 violations. */
function isServerField(field: string): field is Field {
  return field === "password";
}

/** Input ids per field — shared by both focus calls. */
const FIELD_IDS: Record<Field, string> = {
  password: "reset-password",
  confirm: "reset-confirm",
};

@customElement("reset-password-page")
export class ResetPasswordPage extends LitElement {
  // Optional: outside a <session-provider> the field stays undefined.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The token read from the URL on connect ("" = missing → invalid state). */
  @state() private _token = "";
  @state() private _password = "";
  @state() private _confirm = "";
  @state() private _fieldErrors: FieldErrors = {};
  @state() private _error = "";
  @state() private _pending = false;
  /** 429 Retry-After cooldown — owns the ticking + disconnect cancel. */
  private readonly _cooldown = new RetryCooldownController(this);

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
    // back/forward must not replay a consumed link (the
    // history/URL patch rule pairs this replaceState with the read).
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
  }

  protected updated() {
    // The invalid verdict mounts with its text; focus it once it exists so
    // the terminal state is announced (the house status shape).
    if (!this._token && !this._invalidFocused) {
      this._invalidFocused = true;
      this.shadowRoot?.querySelector<HTMLElement>(".invalid-note")?.focus();
    }
  }

  // ── Event handlers ─────────────────────────────────────────────

  private async _onSubmit(e: Event) {
    e.preventDefault();
    if (this._pending || this._cooldown.left > 0) return;

    // Client-side checks. The server remains the authority for the
    // password policy; confirmPassword never reaches the wire (the
    // registration precedent). A mismatch BLOCKS the
    // submit — posting would reset the account with the unconfirmed
    // password (the register-page field-error pattern).
    const errors: FieldErrors = {};
    if (!this._password) {
      errors.password = el.violations.required;
    } else if (this._password !== this._confirm) {
      errors.confirm = el.auth.passwordsDoNotMatch;
    }

    this._fieldErrors = errors;
    this._error = "";
    if (Object.keys(errors).length > 0) {
      await this.updateComplete;
      focusFirstInvalid(this.shadowRoot, FIELD_IDS, this._fieldErrors);
      return;
    }

    this._pending = true;
    let focusAfterError = false;
    try {
      await resetPassword({ token: this._token, password: this._password });
      // Every session for the account died server-side — collapse any
      // stale authenticated state, then hand the success banner to the
      // sign-in page through sessionStorage.
      try {
        sessionStorage.setItem(RESET_SUCCESS_STORAGE_KEY, "1");
      } catch {
        // Storage unavailable (private mode) — the banner is lost, the
        // redirect still works.
      }
      await this.session?.revalidate();
      redirect("/sign-in");
    } catch (err) {
      this._mapSubmitError(err);
      focusAfterError = true;
    } finally {
      this._pending = false;
    }
    // Focus after _pending flips false: inputs re-enable on re-render and
    // a disabled input cannot receive focus (the sign-in-page shape).
    if (focusAfterError) {
      await this.updateComplete;
      focusFirstInvalid(this.shadowRoot, FIELD_IDS, this._fieldErrors);
    }
  }

  private _onFieldInput(field: Field, value: string) {
    if (field === "password") this._password = value;
    else this._confirm = value;
    if (this._fieldErrors[field] || this._error) {
      const errors = { ...this._fieldErrors };
      delete errors[field];
      this._fieldErrors = errors;
      this._error = "";
    }
  }

  // ── Helpers ────────────────────────────────────────────────────

  /**
   * Thin page hook around the shared submit-error mapper. The token-invalid
   * 422 is an ApiError problem — the catalog's
   * /problems/auth/reset-token-invalid key turns it into the Greek banner
   * automatically; no special case needed here.
   */
  private _mapSubmitError(err: unknown) {
    const { fields, banner, retryAfterSeconds } = mapSubmitError(err, {
      serverField: isServerField,
    });
    this._fieldErrors = fields;
    this._error = banner;
    this._cooldown.start(retryAfterSeconds);
  }

  // ── Render ─────────────────────────────────────────────────────

  render() {
    // Missing token — the link was opened without (or after) its token.
    // Generic copy: nothing here distinguishes which case fired.
    if (!this._token) {
      return html`
        <section class="auth-card reset-page" aria-labelledby="reset-heading">
          <h1 id="reset-heading">${el.auth.resetTitle}</h1>
          <p class="invalid-note" role="status" tabindex="-1">
            ${el.auth.resetTokenInvalid}
          </p>
          <p class="footer-link">
            <a href="/auth/forgot">${el.auth.forgotTitle}</a>
          </p>
        </section>
      `;
    }

    return html`
      <section class="auth-card reset-page" aria-labelledby="reset-heading">
        <h1 id="reset-heading">${el.auth.resetTitle}</h1>
        <p class="intro-note">${el.auth.resetIntro}</p>

        <form @submit=${this._onSubmit} novalidate>
          ${formField({
            id: "reset-password",
            label: el.ui.newPassword,
            type: "password",
            value: this._password,
            error: this._fieldErrors.password,
            pending: this._pending,
            autocomplete: "new-password",
            // maxlength approximates the server's 128-byte bound (Go
            // counts UTF-8 bytes). Convenience hint only.
            maxlength: 128,
            onInput: (v) => this._onFieldInput("password", v),
          })}
          ${formField({
            id: "reset-confirm",
            label: el.ui.confirmPassword,
            type: "password",
            value: this._confirm,
            error: this._fieldErrors.confirm,
            pending: this._pending,
            autocomplete: "new-password",
            maxlength: 128,
            onInput: (v) => this._onFieldInput("confirm", v),
          })}
          ${
            this._error
              ? html`<error-banner
                  id="reset-error"
                  .message=${this._error}
                ></error-banner>`
              : ""
          }

          <button
            type="submit"
            class="button button--primary"
            ?disabled=${this._pending || this._cooldown.left > 0}
          >
            ${
              this._pending
                ? el.auth.resetPending
                : this._cooldown.left > 0
                  ? retryAfterMessage(this._cooldown.left)
                  : el.auth.resetButton
            }
          </button>
        </form>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "reset-password-page": ResetPasswordPage;
  }
}
