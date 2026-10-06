/**
 * Forgot-password page — username/email form that requests
 * a reset link by email.
 *
 * The server answers 200 {} whether or not the account exists (enumeration
 * resistance), so the success state is a GENERIC catalog message — this
 * page never learns whether an email went out.
 *
 * States:
 *   initializing  — loading spinner (session bootstrap not yet complete)
 *   authenticated — redirect (the same stale-client reconcile as sign-in)
 *   error         — bootstrap failed; message + retry button
 *   sent          — generic success copy + back-to-sign-in link
 *   anonymous     — the form
 */

import { consume } from "@lit/context";
import { LitElement, html, unsafeCSS, type PropertyValues } from "lit";
import { customElement, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { forgotPassword } from "@features/auth/data-access/auth-api.js";
import { redirect } from "@core/router.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";

import styles from "./forgot-page.css?inline";
import authFormStyles from "./ui/form.css?inline";
import { formField } from "@shared/ui/form-field.js";
import {
  focusFirstInvalid,
  mapSubmitError,
  RetryCooldownController,
} from "./ui/submit-errors.js";

/** Server-known form fields. */
type Field = "identifier";

/** Per-field error messages keyed by field name. */
type FieldErrors = Partial<Record<Field, string>>;

/** Server fields that may appear in 422 violations. */
function isServerField(field: string): field is Field {
  return field === "identifier";
}

/** Input ids per field — shared by both focus calls. */
const FIELD_IDS: Record<Field, string> = {
  identifier: "forgot-identifier",
};

@customElement("forgot-password-page")
export class ForgotPasswordPage extends LitElement {
  // No stacked @state — @consume requests updates itself.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  @state() private _identifier = "";
  @state() private _fieldErrors: FieldErrors = {};
  @state() private _error = "";
  @state() private _pending = false;
  /** True after a successful submit — the generic success copy shows. */
  @state() private _sent = false;
  /** 429 Retry-After cooldown — owns the ticking + disconnect cancel. */
  private readonly _cooldown = new RetryCooldownController(this);

  /** Tracks whether we already redirected to prevent loops. */
  private _redirected = false;

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
    if (this.session?.state.status === "authenticated") {
      this._redirect();
    }
  }

  updated(changed: PropertyValues) {
    super.updated(changed);
    if (
      this.session?.state.status === "authenticated" &&
      !this._pending &&
      !this._redirected
    ) {
      this._redirect();
    }
  }

  // ── Event handlers ─────────────────────────────────────────────

  private async _onSubmit(e: Event) {
    e.preventDefault();
    // The disabled button blocks pointer submits during the 429 window,
    // but implicit submission (Enter) can still fire the event — the
    // guard closes that path.
    if (this._pending || this._cooldown.left > 0) return;

    // Client-side check only. The server remains the authority for format
    // and length rules.
    const errors: FieldErrors = {};
    if (!this._identifier.trim()) errors.identifier = el.violations.required;

    this._fieldErrors = errors;
    this._error = "";
    if (Object.keys(errors).length > 0) {
      await this.updateComplete;
      focusFirstInvalid(this.shadowRoot, FIELD_IDS, this._fieldErrors);
      return;
    }

    this._pending = true;
    try {
      await forgotPassword({ identifier: this._identifier.trim() });
      this._sent = true;
    } catch (err) {
      this._mapSubmitError(err);
    } finally {
      this._pending = false;
    }
    // The form is replaced by the success note — move focus there so the
    // live region is announced and the keyboard position survives.
    await this.updateComplete;
    if (this._sent) {
      (
        this.shadowRoot?.querySelector(".success-note") as HTMLElement | null
      )?.focus();
    }
  }

  private _onFieldInput(value: string) {
    this._identifier = value;
    // Clear this field's error and the general banner as the user edits.
    if (this._fieldErrors.identifier || this._error) {
      this._fieldErrors = {};
      this._error = "";
    }
  }

  // ── Helpers ────────────────────────────────────────────────────

  private _redirect() {
    this._redirected = true;
    // Guard redirect: replaceState (not pushState) so the auth page never
    // stays in history. A flagged session goes straight
    // to the change-password form on /account.
    const s = this.session?.state;
    redirect(
      s?.status === "authenticated" && s.mustChangePassword ? "/account" : "/",
    );
  }

  /**
   * Thin page hook around the shared submit-error mapper: the
   * already-authenticated 403 re-bootstraps (the state change then
   * redirects), plus the 429 submit cooldown for Retry-After windows.
   */
  private _mapSubmitError(err: unknown) {
    const { fields, banner, retryAfterSeconds } = mapSubmitError(err, {
      serverField: isServerField,
      onAlreadyAuthenticated: () => {
        void this.session?.retryBootstrap();
      },
    });
    this._fieldErrors = fields;
    this._error = banner;
    this._cooldown.start(retryAfterSeconds);
  }

  // ── Render ─────────────────────────────────────────────────────

  render() {
    // Bootstrap not yet complete — show spinner.
    if (this.session?.state.status === "initializing") {
      return html`
        <h1 class="sr-only">${el.auth.forgotTitle}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Already authenticated — briefly show redirect message.
    if (this.session?.state.status === "authenticated") {
      return html`
        <h1 class="sr-only">${el.auth.forgotTitle}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Bootstrap failed — show error with retry.
    if (this.session && this.session.state.status === "error") {
      const session = this.session;
      const message = this.session.state.message;
      return html`
        <section class="auth-card forgot-page" aria-labelledby="forgot-heading">
          <h1 id="forgot-heading">${el.auth.forgotTitle}</h1>
          <error-banner .message=${message}></error-banner>
          <button
            type="button"
            class="button button--primary retry-btn"
            @click=${() => session.retryBootstrap()}
          >
            ${el.ui.retry}
          </button>
        </section>
      `;
    }

    // Submitted — the generic success copy (the page never learns whether
    // an email went out; enumeration resistance lives on the server).
    if (this._sent) {
      return html`
        <section class="auth-card forgot-page" aria-labelledby="forgot-heading">
          <h1 id="forgot-heading">${el.auth.forgotTitle}</h1>
          <p class="success-note" role="status" tabindex="-1">
            ${el.auth.forgotSuccess}
          </p>
          <p class="footer-link">
            <a href="/sign-in">${el.auth.backToSignIn}</a>
          </p>
        </section>
      `;
    }

    // Anonymous — show the form.
    return html`
      <section class="auth-card forgot-page" aria-labelledby="forgot-heading">
        <h1 id="forgot-heading">${el.auth.forgotTitle}</h1>

        <form @submit=${this._onSubmit} novalidate>
          ${formField({
            id: "forgot-identifier",
            label: el.auth.identifierLabel,
            type: "text",
            value: this._identifier,
            error: this._fieldErrors.identifier,
            pending: this._pending,
            autocomplete: "username",
            autocapitalize: "none",
            spellcheck: false,
            maxlength: 256,
            onInput: (v) => this._onFieldInput(v),
          })}
          ${
            this._error
              ? html`<error-banner
                  id="forgot-error"
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
                ? el.auth.forgotPending
                : this._cooldown.left > 0
                  ? retryAfterMessage(this._cooldown.left)
                  : el.auth.forgotButton
            }
          </button>
        </form>

        <p class="footer-link">
          <a href="/sign-in">${el.auth.backToSignIn}</a>
        </p>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "forgot-password-page": ForgotPasswordPage;
  }
}
