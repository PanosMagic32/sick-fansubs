/**
 * Registration page — native form creating a new account.
 *
 * States:
 *   initializing  — loading spinner (session bootstrap not yet complete)
 *   anonymous     — registration form
 *   authenticated — redirect to home (already signed in)
 *   error         — bootstrap failed; shows message + retry button
 *
 * Client-side checks: non-empty fields and password/confirmPassword match.
 * Only username/email/password are sent to the server — confirmPassword is
 * client-only (the server's strict JSON boundary rejects unknown fields).
 *
 * After successful registration the session state machine transitions to
 * authenticated and this component redirects to "/account" from updated()
 * (the unverified banner + resend live there).
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

import styles from "./register-page.css?inline";
import authFormStyles from "./ui/form.css?inline";
import { formField } from "@shared/ui/form-field.js";
import {
  focusFirstInvalid,
  mapSubmitError,
  RetryCooldownController,
} from "./ui/submit-errors.js";

/** Server-known form fields (confirmPassword is client-only). */
type Field = "username" | "email" | "password" | "confirmPassword";

/** Per-field error messages keyed by field name. */
type FieldErrors = Partial<Record<Field, string>>;

/** Server fields that may appear in 422 violations. */
function isServerField(
  field: string,
): field is Exclude<Field, "confirmPassword"> {
  return field === "username" || field === "email" || field === "password";
}

/** Field + code → the message that names the failed rule. Undefined
 * falls back to the shared generic code message — a server code this form
 * does not know must never leak raw. */
function registerViolationMessage(
  field: string,
  code: string,
): string | undefined {
  switch (`${field}:${code}`) {
    case "username:maxLength":
      return el.auth.registerUsernameLength;
    case "username:minLength":
      return el.auth.registerUsernameLength;
    case "username:invalidFormat":
      return el.auth.registerUsernameFormat;
    case "username:alreadyTaken":
      return el.auth.registerUsernameTaken;
    case "email:invalidFormat":
      return el.auth.registerEmailFormat;
    case "email:alreadyTaken":
      return el.auth.registerEmailTaken;
    case "password:minLength":
      return el.auth.registerPasswordMin;
    case "password:maxLength":
      return el.auth.registerPasswordMax;
    default:
      return undefined;
  }
}

/** sessionStorage key for the post-registration notice on /account
 * (the register page navigates there; the account page
 * consumes the key ONCE and renders the honest "check your inbox" copy). */
export const REGISTER_SUCCESS_STORAGE_KEY = "sf-register-success";

/** Input ids per field — shared by both focus calls. */
const FIELD_IDS: Record<Field, string> = {
  username: "register-username",
  email: "register-email",
  password: "register-password",
  confirmPassword: "register-confirm-password",
};

@customElement("register-page")
export class RegisterPage extends LitElement {
  // No stacked @state — @consume requests updates itself.
  // Optional: outside a <session-provider> the field stays undefined, so
  // the `!this.session` guards below are reachable and required.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  @state() private _username = "";
  @state() private _email = "";
  @state() private _password = "";
  @state() private _confirmPassword = "";
  @state() private _fieldErrors: FieldErrors = {};
  @state() private _error = "";
  @state() private _pending = false;
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
    // but implicit submission (Enter in a field) can still fire the event —
    // the guard closes that path so no request slips through mid-window.
    if (this._pending || this._cooldown.left > 0) return;

    // Guard against rendering outside the session provider tree.
    if (!this.session) {
      this._error = el.ui.error;
      return;
    }

    // Client-side checks. The server remains the authority for format and
    // length rules — these only catch empty/mismatch.
    const errors: FieldErrors = {};
    if (!this._username.trim()) errors.username = el.violations.required;
    if (!this._email.trim()) errors.email = el.violations.required;
    if (!this._password) errors.password = el.violations.required;
    if (!this._confirmPassword) {
      errors.confirmPassword = el.violations.required;
    } else if (this._password !== this._confirmPassword) {
      errors.confirmPassword = el.auth.passwordsDoNotMatch;
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
      await this.session.register(
        this._username.trim(),
        this._email.trim(),
        this._password,
      );
      // Hand the "verification email" notice to the landing page
      // through sessionStorage (the reset-page banner transport). The copy
      // stays honest: the server may have failed the send.
      try {
        sessionStorage.setItem(REGISTER_SUCCESS_STORAGE_KEY, "1");
      } catch {
        // Storage unavailable (private mode) — the notice is lost, the
        // navigation still works.
      }
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
    switch (field) {
      case "username":
        this._username = value;
        break;
      case "email":
        this._email = value;
        break;
      case "password":
        this._password = value;
        break;
      case "confirmPassword":
        this._confirmPassword = value;
        break;
    }
    // Clear this field's error and the general banner as the user edits.
    // Editing the password also clears a mismatch error on confirmPassword:
    // the match check only runs on submit, so a stale mismatch message may
    // no longer apply once the password itself changes.
    if (
      this._fieldErrors[field] ||
      this._error ||
      (field === "password" && this._fieldErrors.confirmPassword)
    ) {
      const errors = { ...this._fieldErrors };
      delete errors[field];
      if (field === "password") delete errors.confirmPassword;
      this._fieldErrors = errors;
      this._error = "";
    }
  }

  // ── Helpers ────────────────────────────────────────────────────

  private _redirect() {
    this._redirected = true;
    // Guard redirect: replaceState (not pushState) so the auth page never
    // stays in history — Back must not ping-pong through /register.
    // A fresh registration lands on /account — the page that
    // shows the unverified banner + resend button.
    redirect("/account");
  }

  /**
   * Thin page hook around the shared submit-error mapper: register's
   * page-specific behavior is the already-authenticated 403 re-bootstrap,
   * plus the 429 submit cooldown for Retry-After windows.
   */
  private _mapSubmitError(err: unknown) {
    const { fields, banner, retryAfterSeconds } = mapSubmitError(err, {
      serverField: isServerField,
      violation: registerViolationMessage,
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
        <h1 class="sr-only">${el.auth.registerTitle}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Already authenticated — briefly show redirect message.
    if (this.session?.state.status === "authenticated") {
      return html`
        <h1 class="sr-only">${el.auth.registerTitle}</h1>
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
          class="auth-card register-page"
          aria-labelledby="register-heading"
        >
          <h1 id="register-heading">${el.auth.registerTitle}</h1>
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

    // Anonymous — show the registration form.
    return html`
      <section
        class="auth-card register-page"
        aria-labelledby="register-heading"
      >
        <h1 id="register-heading">${el.auth.registerTitle}</h1>

        <form @submit=${this._onSubmit} novalidate>
          ${formField({
            id: "register-username",
            label: el.ui.username,
            type: "text",
            value: this._username,
            error: this._fieldErrors.username,
            pending: this._pending,
            autocomplete: "username",
            // Mobile keyboards capitalize the first letter by default —
            // usernames are ASCII, so disable both.
            autocapitalize: "none",
            spellcheck: false,
            maxlength: 32,
            onInput: (v) => this._onFieldInput("username", v),
          })}
          ${formField({
            id: "register-email",
            label: el.ui.email,
            type: "email",
            value: this._email,
            error: this._fieldErrors.email,
            // The email is the forgot-password recovery identifier — a
            // wrong address means no self-service reset (the
            // recovery-warning rationale).
            hint: el.auth.emailRecoveryHint,
            pending: this._pending,
            autocomplete: "email",
            // Emails are ASCII; same keyboard rationale.
            autocapitalize: "none",
            spellcheck: false,
            maxlength: 254,
            onInput: (v) => this._onFieldInput("email", v),
          })}
          ${formField({
            id: "register-password",
            label: el.ui.password,
            type: "password",
            value: this._password,
            error: this._fieldErrors.password,
            pending: this._pending,
            autocomplete: "new-password",
            // maxlength approximates the server's 72-BYTE bcrypt limit in
            // characters. The server enforces the byte count (UTF-8 Greek
            // characters are 2 bytes), so this is a convenience hint only.
            maxlength: 72,
            onInput: (v) => this._onFieldInput("password", v),
          })}
          ${formField({
            id: "register-confirm-password",
            label: el.ui.confirmPassword,
            type: "password",
            value: this._confirmPassword,
            error: this._fieldErrors.confirmPassword,
            pending: this._pending,
            autocomplete: "new-password",
            maxlength: 72,
            onInput: (v) => this._onFieldInput("confirmPassword", v),
          })}
          ${
            this._error
              ? html`<error-banner
                  id="register-error"
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
                ? el.auth.registerPending
                : this._cooldown.left > 0
                  ? retryAfterMessage(this._cooldown.left)
                  : el.auth.registerButton
            }
          </button>
        </form>

        <p class="footer-link">
          <a href="/sign-in">${el.auth.alreadyHaveAccount}</a>
        </p>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "register-page": RegisterPage;
  }
}
