/**
 * Account profile section for the account page.
 *
 * The page keeps ONE shadow root — the section tabs' aria-controls and the
 * page's stylesheet require it — so the profile panel (load/retry, the
 * verification banners and resend, the email-change form, and the avatar
 * widget) lives here as a reactive controller. `template()` is the panel
 * body the page splices inside its tabpanel; the controller owns its state,
 * its handlers, and its templates, and every state change requests a host
 * update.
 */

import {
  type ReactiveController,
  type ReactiveControllerHost,
  html,
} from "lit";

import { ApiError } from "@shared/api/client.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el, retryAfterMessage } from "@shared/catalog/el.js";
import { AVATAR_CHANGED_EVENT } from "@shared/events.js";
import { formField } from "@shared/ui/form-field.js";
import { formatDateOnly, initialOf, mapError } from "@shared/utils/format.js";
import { roleLabel } from "@shared/utils/roles.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/loading-spinner.js";

import { resendVerification } from "../data-access/auth-api.js";
import {
  changeEmail,
  fetchCurrentUserProfile,
  uploadAvatar,
} from "../data-access/profile-api.js";
import type { UserProfile } from "../data-access/types.js";
import { REGISTER_SUCCESS_STORAGE_KEY } from "../register-page.js";
import { VERIFY_SUCCESS_STORAGE_KEY } from "../verify-page.js";
import {
  focusFirstInvalid,
  mapSubmitError,
  RetryCooldownController,
} from "./submit-errors.js";

/** The host surface the profile section needs from the account page. */
export interface AccountProfileSectionHost extends ReactiveControllerHost {
  /** True while the session state machine reports an authenticated session. */
  readonly authed: boolean;
  /** The page's shadow root — the email form focuses its first invalid field. */
  readonly shadowRoot: ShadowRoot | null;
}

/** Input ids for the email-change widget — shared by both focus calls. */
const EMAIL_FIELD_IDS: Record<"email" | "currentPassword", string> = {
  email: "account-new-email",
  currentPassword: "account-email-current-password",
};

export class AccountProfileSection implements ReactiveController {
  private readonly _host: AccountProfileSectionHost;
  /**
   * 429 cooldown for the VERIFICATION surfaces (resend + email change — the
   * shared 1/30min bucket) — a SEPARATE controller from the password form's:
   * a 30-minute verification window must never disable the password change.
   */
  private readonly _verifyCooldown: RetryCooldownController;

  /** Loaded profile; null while pending or failed. */
  private _profile: UserProfile | null = null;
  /** Profile load failure banner (Greek copy via mapError). */
  private _profileError = "";

  /** Set once the first load attempt completed — the host update hook must
   * not refetch on every session re-render (revalidate re-sets the state
   * object); the retry button calls `_loadProfile` directly. */
  private _profileFetched = false;

  /** In-flight profile request — aborted on teardown. */
  private _profileAbort: AbortController | null = null;

  /** Post-verify success banner — sessionStorage transport, the
   * sign-in-page one-shot shape. */
  private _verifySuccess = false;
  /** Post-registration notice — same transport: the register page navigates
   * here and this section consumes the key once. */
  private _registerSuccess = false;
  /** Resend button state (the 1/30min bucket's 429 cooldown is separate
   * from the password form's — a 30-min window must not lock the password
   * change). */
  private _resendPending = false;
  private _resendError = "";
  private _resendSuccess = false;
  /** Email-change widget state. */
  private _emailFormOpen = false;
  private _newEmail = "";
  private _emailCurrentPassword = "";
  private _emailFieldErrors: Partial<
    Record<"email" | "currentPassword", string>
  > = {};
  private _emailPending = false;
  private _emailChangeError = "";
  private _emailChanged = false;

  /** The chosen file awaiting upload (null when nothing chosen). */
  private _avatarFile: File | null = null;
  /** The preview URL: the local object URL while unconfirmed, else the
   * profile's served URL. */
  private _avatarPreview: string | null = null;
  private _avatarPending = false;
  private _avatarError = "";
  private _avatarSuccess = false;

  /** The local object URL — revoked on replace/teardown (the media-upload
   * widget contract, mirrored here). */
  private _avatarLocalURL: string | null = null;

  constructor(host: AccountProfileSectionHost) {
    this._host = host;
    host.addController(this);
    this._verifyCooldown = new RetryCooldownController(host);
  }

  hostConnected() {
    // Consume the post-verify success banner: read once, remove
    // immediately — a refresh must not re-show it (the sign-in-page shape).
    try {
      if (sessionStorage.getItem(VERIFY_SUCCESS_STORAGE_KEY)) {
        sessionStorage.removeItem(VERIFY_SUCCESS_STORAGE_KEY);
        this._verifySuccess = true;
      }
    } catch {
      // Storage unavailable — no banner, the page still works.
    }
    // Consume the post-registration notice: same one-shot shape.
    try {
      if (sessionStorage.getItem(REGISTER_SUCCESS_STORAGE_KEY)) {
        sessionStorage.removeItem(REGISTER_SUCCESS_STORAGE_KEY);
        this._registerSuccess = true;
      }
    } catch {
      // Storage unavailable — no notice, the page still works.
    }
    this._refresh();
  }

  hostDisconnected() {
    // Cancel an in-flight profile load so it never sets state on a
    // disconnected element (same discipline as the session bootstrap), and
    // revoke the local avatar preview URL (never leaked past the page's
    // lifetime — the media-upload widget contract).
    this._profileAbort?.abort();
    this._profileAbort = null;
    this._revokeAvatarLocal();
  }

  hostUpdated() {
    // Fetch the profile once the session is authenticated. The
    // _profileFetched latch keeps revalidate's re-set of the session state
    // object (which re-renders without a status change) from re-firing the
    // load.
    if (this._host.authed && !this._profileFetched) {
      this._profileFetched = true;
      void this._loadProfile();
    }
  }

  /** The profile tabpanel's body. */
  template() {
    return this._profileTemplate();
  }

  /**
   * Load the current user's profile (GET /api/v1/users/me).
   *
   * A 401 here means a genuinely dead session (no suppression): the
   * shared client's global 401 transition flips the session anonymous and
   * the guard redirects; the generic banner renders only for the frame
   * before the redirect lands.
   */
  private async _loadProfile() {
    if (!this._host.authed) {
      return;
    }

    this._profileAbort?.abort();
    const controller = new AbortController();
    this._profileAbort = controller;
    this._profileError = "";
    this._refresh();

    try {
      const profile = await fetchCurrentUserProfile({
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return; // torn down or superseded
      this._profile = profile;
    } catch (err) {
      if (controller.signal.aborted) return;
      this._profileError = mapError(err);
    }
    this._refresh();
  }

  /** Retry the profile load after a failure. */
  private _onProfileRetry = () => {
    this._profileError = "";
    this._refresh();
    void this._loadProfile();
  };

  /** Resend the verification email (the shared 1/30min bucket governs). */
  private _onResend = async () => {
    if (this._resendPending || this._verifyCooldown.left > 0) return;
    this._resendPending = true;
    this._resendError = "";
    this._resendSuccess = false;
    this._refresh();
    try {
      await resendVerification();
      this._resendSuccess = true;
    } catch (err) {
      const { banner, retryAfterSeconds } = mapSubmitError(err, {
        // The resend has no form fields — every violation is a banner.
        serverField: (_field: string): _field is "none" => false,
      });
      this._resendError = banner;
      this._verifyCooldown.start(retryAfterSeconds);
    } finally {
      this._resendPending = false;
    }
    this._refresh();
  };

  /** Toggle the email-change widget. */
  private _onToggleEmailForm = () => {
    this._emailFormOpen = !this._emailFormOpen;
    this._emailChangeError = "";
    this._emailChanged = false;
    this._refresh();
  };

  private _onEmailFieldInput(
    field: "email" | "currentPassword",
    value: string,
  ) {
    if (field === "email") this._newEmail = value;
    else this._emailCurrentPassword = value;
    if (this._emailFieldErrors[field] || this._emailChangeError) {
      const errors = { ...this._emailFieldErrors };
      delete errors[field];
      this._emailFieldErrors = errors;
      this._emailChangeError = "";
    }
    this._refresh();
  }

  /** Submit the email change (current-password re-authentication). */
  private _onEmailChangeSubmit = async (e: Event) => {
    e.preventDefault();
    if (this._emailPending || this._verifyCooldown.left > 0) return;

    // Client-side checks. The server remains the authority for format and
    // the password (the registration precedent).
    const errors: Partial<Record<"email" | "currentPassword", string>> = {};
    if (!this._newEmail.trim()) errors.email = el.violations.required;
    if (!this._emailCurrentPassword) {
      errors.currentPassword = el.violations.required;
    }
    this._emailFieldErrors = errors;
    this._emailChangeError = "";
    this._refresh();
    if (Object.keys(errors).length > 0) {
      await this._host.updateComplete;
      focusFirstInvalid(
        this._host.shadowRoot,
        EMAIL_FIELD_IDS,
        this._emailFieldErrors,
      );
      return;
    }

    this._emailPending = true;
    try {
      const profile = await changeEmail({
        email: this._newEmail.trim(),
        currentPassword: this._emailCurrentPassword,
      });
      this._profile = profile;
      this._emailFormOpen = false;
      this._newEmail = "";
      this._emailCurrentPassword = "";
      this._emailChanged = true;
    } catch (err) {
      const { fields, banner, retryAfterSeconds } = mapSubmitError(err, {
        serverField: (f): f is "email" | "currentPassword" =>
          f === "email" || f === "currentPassword",
      });
      this._emailFieldErrors = fields;
      this._emailChangeError = banner;
      this._verifyCooldown.start(retryAfterSeconds);
    } finally {
      this._emailPending = false;
    }
    this._refresh();
    // Focus after _emailPending flips false (a disabled input cannot take
    // focus) — the sign-in-page shape.
    if (Object.keys(this._emailFieldErrors).length > 0) {
      await this._host.updateComplete;
      focusFirstInvalid(
        this._host.shadowRoot,
        EMAIL_FIELD_IDS,
        this._emailFieldErrors,
      );
    }
  };

  private _revokeAvatarLocal() {
    if (this._avatarLocalURL) {
      URL.revokeObjectURL(this._avatarLocalURL);
      this._avatarLocalURL = null;
    }
  }

  /** File pick: supersede the previous local preview with the chosen
   * file's object URL immediately (the pick previews the local file).
   * The CONFIRMED avatar in `_profile` is untouched until a new upload
   * SUCCEEDS — a failed upload keeps the old confirmed avatar while the
   * local preview stays visible for a retry. */
  private _onAvatarChoose = (e: Event) => {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0] ?? null;
    this._revokeAvatarLocal();
    this._avatarError = "";
    this._avatarSuccess = false;
    this._avatarFile = file;
    if (file) {
      this._avatarLocalURL = URL.createObjectURL(file);
      this._avatarPreview = this._avatarLocalURL;
    } else {
      this._avatarPreview = this._profile?.avatarUrl ?? null;
    }
    this._refresh();
  };

  /** Upload the chosen file — one step (the server processes the 200×200
   * PNG and updates the reference; the response is the refreshed
   * profile). On success the preview swaps to the served URL and the
   * window event tells the header chip to refetch. */
  private _onAvatarUpload = async () => {
    if (this._avatarPending || !this._avatarFile) return;
    this._avatarPending = true;
    this._avatarError = "";
    this._avatarSuccess = false;
    this._refresh();
    try {
      // No client timeout — the media-upload widget precedent (the server
      // bounds the body; the page's teardown cannot orphan the confirmed
      // update because the response only lands on success).
      const profile = await uploadAvatar(this._avatarFile);
      this._revokeAvatarLocal();
      this._avatarFile = null;
      this._profile = profile;
      this._avatarPreview = profile.avatarUrl;
      this._avatarSuccess = true;
      window.dispatchEvent(new CustomEvent(AVATAR_CHANGED_EVENT));
    } catch (err) {
      if (err instanceof ApiError && err.status === 413) {
        this._avatarError = el.account.avatarTooLarge;
      } else if (err instanceof ApiError && err.status === 422) {
        this._avatarError = el.account.avatarInvalid;
      } else {
        this._avatarError = mapError(err);
      }
    } finally {
      this._avatarPending = false;
    }
    this._refresh();
    // The upload button disappears on success (no chosen file left) — move
    // focus to the status note so it is announced and the position survives.
    if (this._avatarSuccess) {
      await this._host.updateComplete;
      (
        this._host.shadowRoot?.querySelector(
          ".avatar-success",
        ) as HTMLElement | null
      )?.focus();
    }
  };

  /**
   * Profile section states: pending spinner, error + retry, and the facts
   * list. The avatar renders the served image when set (the
   * server emits the absolute URL), else the
   * initials circle, plus the self-service upload widget (pick → local
   * preview → upload; confirmed updates only).
   */
  private _profileTemplate() {
    if (this._profileError) {
      return html`
        <error-banner .message=${this._profileError}></error-banner>
        <button
          type="button"
          class="button button--primary retry-btn"
          @click=${this._onProfileRetry}
        >
          ${el.ui.retry}
        </button>
      `;
    }

    if (!this._profile) {
      // First load in flight (or a retry reset) — spinner.
      return html`<loading-spinner></loading-spinner>`;
    }

    const p = this._profile;
    return html`
      ${
        this._registerSuccess
          ? html`<p class="verify-success" role="status">
              ${el.account.registrationSuccess}
            </p>`
          : ""
      }
      ${
        this._verifySuccess
          ? html`<p class="verify-success" role="status">
              ${el.auth.verifySuccess}
            </p>`
          : ""
      }
      ${
        p.emailVerified
          ? ""
          : html`
              <div class="verify-banner" role="status">
                <p>
                  ${el.account.emailUnverifiedLabel}
                  ${el.account.emailUnverifiedHint}
                </p>
                ${
                  this._resendSuccess
                    ? html`<p class="verify-success">
                        ${el.account.resendSuccess}
                      </p>`
                    : ""
                }
                ${
                  this._resendError
                    ? html`<error-banner
                        .message=${this._resendError}
                      ></error-banner>`
                    : ""
                }
                <button
                  type="button"
                  class="button button--secondary button--sm"
                  ?disabled=${this._resendPending || this._verifyCooldown.left > 0}
                  @click=${this._onResend}
                >
                  ${
                    this._resendPending
                      ? el.account.resendVerificationPending
                      : this._verifyCooldown.left > 0
                        ? retryAfterMessage(this._verifyCooldown.left)
                        : el.account.resendVerification
                  }
                </button>
              </div>
            `
      }
      <div class="profile">
        ${this._avatarTemplate(p)}
        <dl class="profile-facts">
          <div class="fact">
            <dt>${el.ui.username}</dt>
            <dd>${p.username}</dd>
          </div>
          <div class="fact">
            <dt>${el.ui.email}</dt>
            <dd>
              ${p.email}
              <span class="email-status">
                ${p.emailVerified ? el.account.emailVerifiedLabel : el.account.emailUnverifiedLabel}
              </span>
            </dd>
          </div>
          <div class="fact">
            <dt>${el.account.memberSince}</dt>
            <dd>${formatDateOnly(p.createdAt)}</dd>
          </div>
          <div class="fact">
            <dt>${el.account.roleLabel}</dt>
            <dd>${roleLabel(p.role)}</dd>
          </div>
        </dl>
      </div>

      <!-- Install control and the push settings live in the
           notifications panel — they are one story with
           the notification opt-in, not a profile detail. -->

      <div class="email-change">
        ${
          this._emailChanged
            ? html`<p class="verify-success" role="status">
                ${el.account.changeEmailSuccess}
              </p>`
            : ""
        }
        ${
          this._emailFormOpen
            ? html`
                <form @submit=${this._onEmailChangeSubmit} novalidate>
                  ${formField({
                    id: EMAIL_FIELD_IDS.email,
                    label: el.ui.email,
                    type: "email",
                    value: this._newEmail,
                    error: this._emailFieldErrors.email,
                    // Same recovery warning as registration — a wrong
                    // NEW address also kills self-service reset.
                    hint: el.auth.emailRecoveryHint,
                    pending: this._emailPending,
                    autocomplete: "email",
                    autocapitalize: "none",
                    spellcheck: false,
                    maxlength: 254,
                    onInput: (v) => this._onEmailFieldInput("email", v),
                  })}
                  ${formField({
                    id: EMAIL_FIELD_IDS.currentPassword,
                    label: el.ui.password,
                    type: "password",
                    value: this._emailCurrentPassword,
                    error: this._emailFieldErrors.currentPassword,
                    pending: this._emailPending,
                    autocomplete: "current-password",
                    maxlength: 128,
                    onInput: (v) =>
                      this._onEmailFieldInput("currentPassword", v),
                  })}
                  ${
                    this._emailChangeError
                      ? html`<error-banner
                          .message=${this._emailChangeError}
                        ></error-banner>`
                      : ""
                  }
                  <div class="email-change-actions">
                    <button
                      type="submit"
                      class="button button--primary"
                      ?disabled=${this._emailPending || this._verifyCooldown.left > 0}
                    >
                      ${
                        this._emailPending
                          ? el.account.changeEmailPending
                          : this._verifyCooldown.left > 0
                            ? retryAfterMessage(this._verifyCooldown.left)
                            : el.account.changeEmailButton
                      }
                    </button>
                    <button
                      type="button"
                      class="button button--secondary secondary-btn"
                      ?disabled=${this._emailPending}
                      @click=${this._onToggleEmailForm}
                    >
                      ${el.account.changeEmailCancel}
                    </button>
                  </div>
                </form>
              `
            : html`
                <button
                  type="button"
                  class="button button--secondary secondary-btn"
                  @click=${this._onToggleEmailForm}
                >
                  ${el.account.changeEmailButton}
                </button>
              `
        }
      </div>
    `;
  }

  /**
   * The profile avatar + upload widget. The visible image is the local
   * object URL while a file awaits confirmation, else the served avatar
   * (or the initials circle). Decorative img (alt="") — the username
   * renders in the facts list next to it.
   */
  private _avatarTemplate(p: UserProfile) {
    return html`
      <div class="avatar-block">
        ${
          this._avatarPreview || p.avatarUrl
            ? html`<img
                class="profile-avatar"
                src=${this._avatarPreview ?? p.avatarUrl}
                alt=""
              />`
            : html`<div class="avatar avatar-initial" aria-hidden="true">
                ${initialOf(p.username)}
              </div>`
        }
        <div class="avatar-controls">
          <input
            type="file"
            accept="image/jpeg,image/png,image/webp"
            aria-label=${
              p.avatarUrl ? el.account.avatarReplace : el.account.avatarChoose
            }
            ?disabled=${this._avatarPending}
            @change=${this._onAvatarChoose}
          />
          ${
            this._avatarFile
              ? html`<button
                  type="button"
                  class="button button--secondary button--sm avatar-upload-btn"
                  ?disabled=${this._avatarPending}
                  @click=${this._onAvatarUpload}
                >
                  ${
                    this._avatarPending
                      ? el.account.avatarUploading
                      : el.account.avatarUpload
                  }
                </button>`
              : ""
          }
        </div>
        ${
          this._avatarSuccess
            ? html`<p class="avatar-success" role="status" tabindex="-1">
                ${el.account.avatarSuccess}
              </p>`
            : ""
        }
        ${
          this._avatarError
            ? html`<p class="avatar-error" role="alert">
                ${this._avatarError}
              </p>`
            : ""
        }
      </div>
    `;
  }

  /** Request a host re-render after a state change. */
  private _refresh() {
    this._host.requestUpdate();
  }
}
