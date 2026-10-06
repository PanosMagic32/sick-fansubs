/**
 * Account security section for the account page.
 *
 * The password-change form — fields, per-field client checks, the shared
 * submit-error mapping with the wrong-current-password override, the 429
 * cooldown, the focus moves, and the success handoff to the page's guard —
 * as a reactive controller, so the page keeps ONE shadow root (the tab
 * panels' aria-controls contract and the page's stylesheet require it).
 * The in-flight flag and the action banner are page-shared with the devices
 * panel; this section reads and writes them through the host accessors.
 */

import {
  type ReactiveController,
  type ReactiveControllerHost,
  html,
} from "lit";

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
} from "@shared/api/client.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import { formField } from "@shared/ui/form-field.js";
import "@shared/ui/error-banner.js";

import type { SessionContext } from "../data-access/session.js";
import {
  focusFirstInvalid,
  mapSubmitError,
  RetryCooldownController,
} from "./submit-errors.js";

/** Password-change form fields (the confirm field is client-only — the same
 *  rationale as the register form's confirmPassword note). */
type Field = "currentPassword" | "newPassword" | "confirmNewPassword";

type FieldErrors = Partial<Record<Field, string>>;

function isServerField(
  field: string,
): field is Exclude<Field, "confirmNewPassword"> {
  return field === "currentPassword" || field === "newPassword";
}

/** The host surface the security section needs from the account page. */
export interface AccountSecuritySectionHost extends ReactiveControllerHost {
  /** The page's session context — changePassword and the revalidate heal use it. */
  readonly sessionContext: SessionContext | undefined;
  /** The page-shared in-flight flag (also used by the devices panel). */
  pending: boolean;
  /** The page-shared action banner (also used by the devices panel). */
  actionError: string;
  /** The page's shadow root — the form focuses its first invalid field. */
  readonly shadowRoot: ShadowRoot | null;
  /** Hand the successful password change to the page's guard. */
  markPasswordChanged(): void;
}

export class AccountSecuritySection implements ReactiveController {
  private readonly _host: AccountSecuritySectionHost;
  /**
   * 429 Retry-After cooldown for the PASSWORD FORM only — owns the
   * ticking + disconnect cancel. Sign-out actions are not rate-limited and
   * stay usable while this runs.
   */
  private readonly _cooldown: RetryCooldownController;

  private _currentPassword = "";
  private _newPassword = "";
  private _confirmNewPassword = "";
  private _fieldErrors: FieldErrors = {};

  constructor(host: AccountSecuritySectionHost) {
    this._host = host;
    host.addController(this);
    this._cooldown = new RetryCooldownController(host);
  }

  /** The section has no teardown work — the cooldown controller cancels its
   *  own interval. The no-op hook itself is load-bearing: without a public
   *  member, TS's weak-type rule rejects the class as a ReactiveController
   *  (all of whose members are optional). */
  hostDisconnected() {}

  /** The security tabpanel's body: the password form and its error banner. */
  template() {
    return html`
      <form @submit=${this._onPasswordSubmit} novalidate>
        ${formField({
          id: "account-current-password",
          label: el.ui.currentPassword,
          type: "password",
          value: this._currentPassword,
          error: this._fieldErrors.currentPassword,
          pending: this._host.pending,
          autocomplete: "current-password",
          onInput: (v) => this._onFieldInput("currentPassword", v),
        })}
        ${formField({
          id: "account-new-password",
          label: el.ui.newPassword,
          type: "password",
          value: this._newPassword,
          error: this._fieldErrors.newPassword,
          pending: this._host.pending,
          autocomplete: "new-password",
          // Convenience hint only: the server enforces 72 BYTES (bcrypt),
          // while maxlength counts characters — multibyte Greek text can
          // still be rejected with a 422 (same rationale as the register
          // form).
          maxlength: 72,
          onInput: (v) => this._onFieldInput("newPassword", v),
        })}
        ${formField({
          id: "account-confirm-new-password",
          label: el.ui.confirmPassword,
          type: "password",
          value: this._confirmNewPassword,
          error: this._fieldErrors.confirmNewPassword,
          pending: this._host.pending,
          autocomplete: "new-password",
          maxlength: 72,
          onInput: (v) => this._onFieldInput("confirmNewPassword", v),
        })}
        <button
          type="submit"
          class="button button--primary"
          ?disabled=${this._host.pending || this._cooldown.left > 0}
        >
          ${
            this._cooldown.left > 0
              ? retryAfterMessage(this._cooldown.left)
              : el.auth.changePassword
          }
        </button>
      </form>
      ${
        this._host.actionError
          ? html`<error-banner
              id="account-error"
              .message=${this._host.actionError}
            ></error-banner>`
          : ""
      }
    `;
  }

  private _onPasswordSubmit = async (e: Event) => {
    e.preventDefault();
    // The disabled button blocks pointer submits during the 429 window,
    // but implicit submission (Enter in a field) can still fire the event —
    // the guard closes that path so no request slips through mid-window.
    if (this._host.pending || this._cooldown.left > 0) return;

    const session = this._host.sessionContext;
    if (!session) {
      this._host.actionError = el.ui.error;
      this._refresh();
      return;
    }

    // Client-side checks. The server remains the authority for the password
    // rules — these only catch empty/mismatch fields.
    const errors: FieldErrors = {};
    if (!this._currentPassword) errors.currentPassword = el.violations.required;
    if (!this._newPassword) errors.newPassword = el.violations.required;
    if (!this._confirmNewPassword) {
      errors.confirmNewPassword = el.violations.required;
    } else if (this._newPassword !== this._confirmNewPassword) {
      errors.confirmNewPassword = el.auth.passwordsDoNotMatch;
    }

    this._fieldErrors = errors;
    this._host.actionError = "";
    this._refresh();
    if (Object.keys(errors).length > 0) {
      await this._host.updateComplete;
      this._focusFirstInvalid();
      return;
    }

    this._host.pending = true;
    let focusAfterError = false;
    try {
      // suppress401Callback is handled inside changePassword (client.ts):
      // a 401 here means "wrong current password", not session expiry — the
      // global 401→anonymous transition is suppressed.
      await session.changePassword(this._currentPassword, this._newPassword);
      // Success: the provider transitioned to anonymous (all sessions were
      // revoked). markPasswordChanged keeps the guard from redirecting so
      // the success view renders.
      this._host.markPasswordChanged();
      // Move focus to the success message: the form (and the focused
      // submit button) is replaced by the success view.
      await this._host.updateComplete;
      (
        this._host.shadowRoot?.querySelector(".success") as HTMLElement | null
      )?.focus();
    } catch (err) {
      this.applySubmitError(err);
      // The change-password 401 is ambiguous (wrong password OR dead
      // session) and the global 401 transition is suppressed — silently
      // revalidate so a genuinely revoked session flips anonymous and the
      // guard redirects. Also heals "connection failure or deadline after
      // the server already committed" (password changed, session dead). A
      // 403 is the same ambiguity on the CSRF axis (stale in-memory token
      // after a cross-tab re-sign-in) — revalidate re-syncs it.
      // Deliberately NOT run for ValidationError/429: those imply a live,
      // valid session and the extra round-trip would just re-assert
      // identical state.
      if (
        (err instanceof ApiError &&
          (err.status === 401 || err.status === 403)) ||
        err instanceof NetworkError ||
        err instanceof RequestTimeoutError
      ) {
        void this._host.sessionContext?.revalidate();
      }
      focusAfterError = true;
    } finally {
      this._host.pending = false;
    }
    if (focusAfterError) {
      await this._host.updateComplete;
      this._focusFirstInvalid();
    }
  };

  private _onFieldInput(field: Field, value: string) {
    if (field === "currentPassword") this._currentPassword = value;
    else if (field === "newPassword") this._newPassword = value;
    else this._confirmNewPassword = value;

    // Clear this field's error and the general banner as the user edits.
    // Editing the new password also clears a mismatch error on the confirm
    // field (same rationale as the register form).
    if (
      this._fieldErrors[field] ||
      this._host.actionError ||
      (field === "newPassword" && this._fieldErrors.confirmNewPassword)
    ) {
      const errors = { ...this._fieldErrors };
      delete errors[field];
      if (field === "newPassword") delete errors.confirmNewPassword;
      this._fieldErrors = errors;
      this._host.actionError = "";
    }
    this._refresh();
  }

  /**
   * Map a submit failure through the page's shared submit-error mapper: the
   * wrong-current-password 401's special case (sign-in's invalid-credentials
   * problem type maps to the current-password FIELD error), the page banner,
   * and the Retry-After cooldown. Public because the sessions section
   * routes sign-out failures through the SAME mapping.
   */
  applySubmitError(err: unknown) {
    const { fields, banner, retryAfterSeconds } = mapSubmitError(err, {
      serverField: isServerField,
      specialApiError: (apiErr) =>
        apiErr.type === "/problems/auth/invalid-credentials"
          ? {
              fields: { currentPassword: el.auth.wrongCurrentPassword },
              banner: "",
            }
          : null,
    });
    this._fieldErrors = fields;
    this._host.actionError = banner;
    this._cooldown.start(retryAfterSeconds);
    this._refresh();
  }

  /** Move focus to the first field with an error, if any. */
  private _focusFirstInvalid() {
    focusFirstInvalid(
      this._host.shadowRoot,
      {
        currentPassword: "account-current-password",
        newPassword: "account-new-password",
        confirmNewPassword: "account-confirm-new-password",
      },
      this._fieldErrors,
    );
  }

  /** Request a host re-render after a state change. */
  private _refresh() {
    this._host.requestUpdate();
  }
}
