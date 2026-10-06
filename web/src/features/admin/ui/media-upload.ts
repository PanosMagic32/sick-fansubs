/**
 * Media upload widget — the upload-first flow, embedded in the blog/project
 * create-and-edit forms.
 *
 * Contract:
 *  - renders NOTHING for anyone who is not admin/super-admin (client-side
 *    visibility only — the server enforces the real authorization),
 *  - file pick → local object-URL preview → the upload STARTS AT ONCE, no
 *    second press: only an uploaded file becomes the submitted path,
 *  - getValue() returns the last successful {path, url} (null when none) —
 *    the form submits `path` as thumbnailPath, never hand-built,
 *  - isUploading() is true while a chosen file is in flight. The create/edit
 *    forms refuse to submit then, because the value still holds the PREVIOUS
 *    thumbnail: a save during the upload would quietly write that one over
 *    the file the editor just picked (the same silent-substitution class
 *    the auto-upload removes),
 *  - the remove control deletes the file THIS widget uploaded (DELETE
 *    /api/v1/media/{file}) and reverts to null, so the form falls
 *    back to the row's stored thumbnail. The delete is best-effort: a
 *    refusal (409 — the file got referenced meanwhile) or a failure keeps
 *    the file and the 24 h sweep reclaims it if it stays unreferenced; the
 *    widget never surfaces a cleanup error over the editor's work,
 *  - reset() clears file/input/preview/value,
 *  - dispatches "media-upload" (CustomEvent, bubbles+composed) on success.
 *
 * The visible status line is always on screen: "uploaded", "not uploaded",
 * or "uploading", so the upload state and the form's save state cannot be
 * confused.
 *
 * Object URLs are revoked on replace/remove/teardown (never leaked past the
 * widget's lifetime).
 */

import { consume } from "@lit/context";
import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { ApiError } from "@shared/api/client.js";
import { el } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";
import { isContentCreatorRole } from "@shared/utils/roles.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

import {
  deleteMedia,
  mediaFileFromURL,
  uploadMedia,
  type MediaUploadResult,
} from "../data-access/media-api.js";
import styles from "./media-upload.css?inline";

@customElement("media-upload")
export class MediaUpload extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The preview URL: local object URL while uploading, the processed
   * served URL after success. */
  @state() private _previewURL: string | null = null;

  @state() private _pending = false;
  @state() private _error = "";

  /** The last successful server result. */
  private _value: MediaUploadResult | null = null;

  /** The local preview URL — revoked on replace/remove/teardown. */
  private _localURL: string | null = null;

  /** The upload in flight — aborted on teardown or on a newer choice. */
  private _uploadAbort: AbortController | null = null;

  disconnectedCallback() {
    this._uploadAbort?.abort();
    this._uploadAbort = null;
    this._revokeLocal();
    super.disconnectedCallback();
  }

  /** The value the create/edit form submits (never hand-built). */
  getValue(): MediaUploadResult | null {
    return this._value;
  }

  /** True while a chosen file is uploading — the forms' submit gate. */
  isUploading(): boolean {
    return this._pending;
  }

  /** Clear the widget: file, input, previews, value, messages. */
  reset(): void {
    this._uploadAbort?.abort();
    this._uploadAbort = null;
    this._revokeLocal();
    this._previewURL = null;
    this._value = null;
    this._pending = false;
    this._error = "";
    const input =
      this.shadowRoot?.querySelector<HTMLInputElement>("input[type=file]");
    if (input) input.value = "";
  }

  private _isStaff(): boolean {
    const s = this.session?.state;
    if (!s || s.status !== "authenticated") return false;
    return isContentCreatorRole(s.user.role);
  }

  private _revokeLocal() {
    if (this._localURL) {
      URL.revokeObjectURL(this._localURL);
      this._localURL = null;
    }
  }

  private _onChoose(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0] ?? null;
    // A new choice supersedes the previous upload: the local preview takes
    // over, and the previous VALUE stays until the new upload confirms
    // (confirmed updates — a failed replacement keeps the old thumbnail).
    this._revokeLocal();
    this._error = "";
    if (file) {
      this._localURL = URL.createObjectURL(file);
      this._previewURL = this._localURL;
      // Auto-upload: picking IS uploading. Nothing to press, and
      // no way to submit a chosen-but-unuploaded file.
      void this._upload(file);
    } else {
      this._previewURL = this._value?.url ?? null;
    }
  }

  private async _upload(file: File) {
    this._uploadAbort?.abort();
    const controller = new AbortController();
    this._uploadAbort = controller;
    this._pending = true;
    this._error = "";
    try {
      const result = await uploadMedia(file, { signal: controller.signal });
      if (controller.signal.aborted) return;
      // Swap the local preview for the processed, served image.
      this._revokeLocal();
      this._value = result;
      this._previewURL = result.url;
      this.dispatchEvent(
        new CustomEvent("media-upload", {
          detail: result,
          bubbles: true,
          composed: true,
        }),
      );
    } catch (err) {
      if (controller.signal.aborted) return;
      if (err instanceof ApiError && err.status === 413) {
        this._error = el.admin.uploadTooLarge;
      } else if (err instanceof ApiError && err.status === 422) {
        this._error = el.admin.uploadInvalid;
      } else {
        this._error = mapError(err);
      }
      // A failed replacement keeps the previous confirmed value (with none,
      // the value stays null): the form keeps the stored thumbnail, and the
      // status line goes back to stating what is actually uploaded.
      this._revokeLocal();
      this._previewURL = this._value?.url ?? null;
    } finally {
      if (!controller.signal.aborted) this._pending = false;
    }
  }

  /**
   * Remove the uploaded file: the speculative delete of
   * a file THIS widget uploaded. Referenced files (409) and failures are
   * silent — the sweep is the backstop, and a cleanup step must never
   * surface an error over the editor's work.
   */
  private async _onRemove() {
    const uploaded = this._value;
    this.reset();
    const file = uploaded ? mediaFileFromURL(uploaded.url) : "";
    if (!file) return;
    try {
      await deleteMedia(file);
    } catch {
      /* best-effort: the 24 h sweep reclaims an unreferenced file */
    }
  }

  /** The visible upload-state line. */
  private _statusText(): string {
    if (this._pending) return el.admin.uploadStatusUploading;
    if (this._value) return el.admin.uploadStatusUploaded;
    return el.admin.uploadStatusNone;
  }

  render() {
    if (!this._isStaff()) return html``;

    return html`
      <div class="media-upload">
        ${
          this._previewURL
            ? html`<img class="media-preview" src=${this._previewURL} alt="" />`
            : ""
        }
        <p
          class="media-status${this._value ? " is-uploaded" : ""}"
          role="status"
        >
          ${this._statusText()}
        </p>
        <input
          type="file"
          accept="image/jpeg,image/png,image/webp"
          aria-label=${
            this._value ? el.admin.uploadReplace : el.admin.uploadChoose
          }
          ?disabled=${this._pending}
          @change=${this._onChoose}
        />
        <div class="media-actions">
          ${
            this._value
              ? html`<button
                  type="button"
                  class="button button--danger media-remove-button"
                  ?disabled=${this._pending}
                  @click=${this._onRemove}
                >
                  ${el.admin.uploadRemove}
                </button>`
              : ""
          }
        </div>
        ${
          this._error
            ? html`<p class="media-error" role="alert">${this._error}</p>`
            : ""
        }
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "media-upload": MediaUpload;
  }
}
