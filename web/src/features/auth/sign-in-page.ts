/**
 * Sign-in page — native form with server authentication.
 *
 * States:
 *   initializing  — loading spinner (session bootstrap not yet complete)
 *   anonymous     — sign-in form
 *   authenticated — redirect to home (already signed in)
 *   error         — bootstrap failed; shows message + retry button
 *
 * After successful sign-in, the session state machine transitions to
 * authenticated and this component detects the change in updated() and
 * redirects to "/".
 */

import { consume } from "@lit/context";
import { LitElement, html, unsafeCSS, type PropertyValues } from "lit";
import { customElement, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { redirect } from "@core/router.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";

import styles from "./sign-in-page.css?inline";
import authFormStyles from "./ui/form.css?inline";
import { formField } from "@shared/ui/form-field.js";
import { RESET_SUCCESS_STORAGE_KEY } from "./reset-page.js";
import { VERIFY_SUCCESS_STORAGE_KEY } from "./verify-page.js";
import {
  focusFirstInvalid,
  mapSubmitError,
  RetryCooldownController,
} from "./ui/submit-errors.js";

/** Server-known form fields. */
type Field = "identifier" | "password";

/** Per-field error messages keyed by field name. */
type FieldErrors = Partial<Record<Field, string>>;

/** Server fields that may appear in 422 violations. */
function isServerField(field: string): field is Field {
  return field === "identifier" || field === "password";
}

/** Input ids per field — shared by both focus calls. */
const FIELD_IDS: Record<Field, string> = {
  identifier: "signin-identifier",
  password: "signin-password",
};

@customElement("sign-in-page")
export class SignInPage extends LitElement {
  // No stacked @state — @consume requests updates itself.
  // Optional: outside a <session-provider> the field stays undefined, so
  // the `!this.session` guards below are reachable and required.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  @state() private _identifier = "";
  @state() private _password = "";
  @state() private _fieldErrors: FieldErrors = {};
  @state() private _error = "";
  @state() private _pending = false;
  /** Post-reset success banner — sessionStorage transport. */
  @state() private _resetSuccess = false;
  /** Post-verify success banner — same transport, same one-shot. */
  @state() private _verifySuccess = false;
  /** 429 Retry-After cooldown — owns the ticking + disconnect cancel. */
  private readonly _cooldown = new RetryCooldownController(this);

  /** Tracks whether we already redirected to prevent loops. */
  private _redirected = false;

  /** One-shot: the post-reset/post-verify note was focused after its first
   * render (a hard load renders the spinner first, so the focus waits). */
  private _successNoteFocused = false;

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
    // Consume the post-reset success banner: read once, remove
    // immediately — a refresh must not re-show it.
    try {
      if (sessionStorage.getItem(RESET_SUCCESS_STORAGE_KEY)) {
        sessionStorage.removeItem(RESET_SUCCESS_STORAGE_KEY);
        this._resetSuccess = true;
      }
    } catch {
      // Storage unavailable — no banner, form still works.
    }
    // Consume the post-verify success banner: same one-shot shape.
    try {
      if (sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY)) {
        sessionStorage.removeItem(VERIFY_SUCCESS_STORAGE_KEY);
        this._verifySuccess = true;
      }
    } catch {
      // Storage unavailable — no banner, form still works.
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
    // The reset/verify note mounts with its text; focus it once it exists
    // so the status region is announced (the hard-load spinner renders first).
    if (!this._successNoteFocused) {
      const note =
        this.shadowRoot?.querySelector<HTMLElement>(".reset-success");
      if (note) {
        this._successNoteFocused = true;
        note.focus();
      }
    }
  }

  // ── Event handlers ─────────────────────────────────────────────

  private async _onSubmit(e: Event) {
    e.preventDefault();
    // The disabled button blocks pointer submits during the 429 window,
    // but implicit submission (Enter in a field) can still fire the event —
    // the guard closes that path so no request slips through mid-window.
    if (this._pending || this._cooldown.left > 0) return;

    // Guard against rendering outside the session provider tree.
    if (!this.session) {
      this._error = el.ui.error;
      return;
    }

    // Client-side checks. The server remains the authority for format and
    // length rules — these only catch empty fields.
    const errors: FieldErrors = {};
    if (!this._identifier.trim()) errors.identifier = el.violations.required;
    if (!this._password) errors.password = el.violations.required;

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
      await this.session.signIn(this._identifier.trim(), this._password);
      // On success, updated() will detect the state change and redirect.
    } catch (err) {
      this._mapSubmitError(err);
      focusAfterError = true;
    } finally {
      this._pending = false;
    }
    // Focus after _pending flips false: inputs re-enable on re-render and
    // a disabled input cannot receive focus.
    if (focusAfterError) {
      await this.updateComplete;
      focusFirstInvalid(this.shadowRoot, FIELD_IDS, this._fieldErrors);
    }
  }

  private _onFieldInput(field: Field, value: string) {
    if (field === "identifier") this._identifier = value;
    else this._password = value;
    // Clear this field's error and the general banner as the user edits.
    if (this._fieldErrors[field] || this._error) {
      const errors = { ...this._fieldErrors };
      delete errors[field];
      this._fieldErrors = errors;
      this._error = "";
    }
  }

  // ── Helpers ────────────────────────────────────────────────────

  private _redirect() {
    this._redirected = true;
    // Guard redirect: replaceState (not pushState) so the auth page never
    // stays in history — Back must not ping-pong through /sign-in.
    // A flagged session goes straight to the
    // change-password form on /account — gated endpoints 403 until
    // the password changes.
    const s = this.session?.state;
    redirect(
      s?.status === "authenticated" && s.mustChangePassword ? "/account" : "/",
    );
  }

  /**
   * Thin page hook around the shared submit-error mapper: sign-in's
   * page-specific behavior is the already-authenticated 403 re-bootstrap,
   * plus the 429 submit cooldown for Retry-After windows.
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
        <h1 class="sr-only">${el.auth.signInTitle}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Already authenticated — briefly show redirect message.
    if (this.session?.state.status === "authenticated") {
      return html`
        <h1 class="sr-only">${el.auth.signInTitle}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Bootstrap failed — show error with retry.
    if (this.session && this.session.state.status === "error") {
      // Local captures: property narrowing does not flow into the template
      // closures below.
      const session = this.session;
      const message = this.session.state.message;
      return html`
        <section
          class="auth-card sign-in-page"
          aria-labelledby="signin-heading"
        >
          <h1 id="signin-heading">${el.auth.signInTitle}</h1>
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

    // Anonymous — show the sign-in form.
    return html`
      <section class="auth-card sign-in-page" aria-labelledby="signin-heading">
        <h1 id="signin-heading">${el.auth.signInTitle}</h1>

        <form @submit=${this._onSubmit} novalidate>
          ${
            this._verifySuccess
              ? html`<p class="reset-success" role="status" tabindex="-1">
                  ${el.auth.verifySuccess}
                </p>`
              : this._resetSuccess
                ? html`<p class="reset-success" role="status" tabindex="-1">
                    ${el.auth.resetSuccess}
                  </p>`
                : ""
          }
          ${formField({
            id: "signin-identifier",
            label: el.auth.identifierLabel,
            type: "text",
            value: this._identifier,
            error: this._fieldErrors.identifier,
            pending: this._pending,
            autocomplete: "username",
            // Mobile keyboards capitalize the first letter by default —
            // usernames/emails are ASCII and lowercase-mapped server-side.
            autocapitalize: "none",
            spellcheck: false,
            maxlength: 256,
            onInput: (v) => this._onFieldInput("identifier", v),
          })}
          ${formField({
            id: "signin-password",
            label: el.ui.password,
            type: "password",
            value: this._password,
            error: this._fieldErrors.password,
            pending: this._pending,
            autocomplete: "current-password",
            // maxlength approximates the server's 128-byte limit in
            // characters (Go counts UTF-8 bytes). Convenience hint only.
            maxlength: 128,
            onInput: (v) => this._onFieldInput("password", v),
          })}
          ${
            this._error
              ? html`<error-banner
                  id="signin-error"
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
                ? el.auth.signInPending
                : this._cooldown.left > 0
                  ? retryAfterMessage(this._cooldown.left)
                  : el.auth.signInButton
            }
          </button>
        </form>

        <p class="footer-link">
          <a href="/register">${el.auth.noAccount}</a>
        </p>
        <p class="footer-link">
          <a href="/auth/forgot">${el.auth.forgotLink}</a>
        </p>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sign-in-page": SignInPage;
  }
}
