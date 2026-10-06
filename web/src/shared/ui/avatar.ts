/**
 * Avatar chip template — renders a user ref's avatar with a deliberate
 * fallback chain (owner decision):
 *
 *   avatarUrl present → image
 *   username without avatar → neutral circle with the first letter
 *   no user at all → neutral empty circle
 *
 * The circle is always present so the meta rows keep a stable shape
 * regardless of user state. Decorative: alt=""/aria-hidden — the username
 * text renders next to it where the design needs it.
 *
 * Styles live in shared/ui/avatar.css, imported by the chip's consumers
 * (`content-card`, the shell header, the users page).
 */

import { html, type TemplateResult } from "lit";

import { initialOf } from "@shared/utils/format.js";

/**
 * Minimal structural user reference the chip needs. Feature wire DTOs
 * (BlogPostUserRef / ProjectUserRef) are structurally assignable — the
 * shared component does not import feature types.
 */
export interface AvatarUserRef {
  username: string;
  avatarUrl: string | null;
}

/** One avatar chip for a user ref (or a neutral circle when null). */
export function avatarTemplate(ref: AvatarUserRef | null): TemplateResult {
  if (ref?.avatarUrl) {
    return html`<img class="avatar" src=${ref.avatarUrl} alt="" />`;
  }
  if (ref?.username) {
    const initial = initialOf(ref.username);
    return html`<span class="avatar avatar-initial" aria-hidden="true"
      >${initial}</span
    >`;
  }
  return html`<span class="avatar avatar-empty" aria-hidden="true"></span>`;
}
