/**
 * The account profile's avatar widget template — the served image (or the
 * initials circle), the file picker, the upload control, and the success and
 * error notes. It lives beside the section so the section module stays under
 * the size ceiling; the section owns the state and handlers it receives.
 */

import { html, type TemplateResult } from "lit";

import { el } from "@shared/catalog/el.js";
import { initialOf } from "@shared/utils/format.js";

import type { UserProfile } from "../data-access/types.js";

export interface AvatarTemplateView {
  profile: UserProfile;
  /** The local object URL while a file awaits confirmation. */
  preview: string | null;
  pending: boolean;
  hasFile: boolean;
  success: boolean;
  error: string;
}

export interface AvatarTemplateHandlers {
  onChoose(e: Event): void;
  onUpload(): void;
}

export function avatarTemplate(
  view: AvatarTemplateView,
  handlers: AvatarTemplateHandlers,
): TemplateResult {
  const { profile, preview } = view;
  return html`
    <div class="avatar-block">
      ${
        preview || profile.avatarUrl
          ? html`<img
              class="profile-avatar"
              src=${preview ?? profile.avatarUrl}
              alt=""
            />`
          : html`<div class="avatar avatar-initial" aria-hidden="true">
              ${initialOf(profile.username)}
            </div>`
      }
      <div class="avatar-controls">
        <input
          type="file"
          accept="image/jpeg,image/png,image/webp"
          aria-label=${
            profile.avatarUrl
              ? el.account.avatarReplace
              : el.account.avatarChoose
          }
          ?disabled=${view.pending}
          @change=${handlers.onChoose}
        />
        ${
          view.hasFile
            ? html`<button
                type="button"
                class="button button--secondary button--sm avatar-upload-btn"
                ?disabled=${view.pending}
                @click=${handlers.onUpload}
              >
                ${
                  view.pending
                    ? el.account.avatarUploading
                    : el.account.avatarUpload
                }
              </button>`
            : ""
        }
      </div>
      ${
        view.success
          ? html`<p class="avatar-success" tabindex="-1">
              ${el.account.avatarSuccess}
            </p>`
          : ""
      }
      ${
        view.error
          ? html`<p class="avatar-error" role="alert">${view.error}</p>`
          : ""
      }
    </div>
  `;
}
