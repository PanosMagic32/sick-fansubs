/**
 * Notification items — the per-kind feed row of <notifications-page>: the
 * entry button with its read/unread look, the per-kind line, the delete
 * control, and the per-item click/delete flows.
 *
 * A reactive section controller: the page owns the list, the guard, the URL
 * paging and the toolbar; this controller owns the item-scoped pending state,
 * the navigation targets and the templates. They render into the page's
 * shadow root — one shared tree is the contract, the row CSS classes and the
 * row queries live in it — and every state change asks the host for a
 * re-render.
 *
 * Item behavior by kind: account audit items mark-read with NO navigation;
 * comment_reply/comment/heart items mark-read + navigate to the content deep
 * link (`/blog/{contentId}#comment-{commentId}` or the projects form — the
 * thread's focus contract); content_updated/new_content/comment_removed items
 * mark-read + navigate to the content page itself, no fragment;
 * draft_activity items mark-read + navigate to the STAFF editor
 * (`/admin/blog/{contentId}` or `/admin/projects/{contentId}`), because the
 * content they name is unpublished.
 *
 * The host contract carries the list mutations the flows need
 * (`_markLocalRead`/`_removeItem`), the shared error banner (`_readError`),
 * the badge event (`_dispatchChanged`) and the guard's redirect latch
 * (`_redirected`).
 */

import {
  html,
  nothing,
  type ReactiveController,
  type ReactiveControllerHost,
} from "lit";

import { navigate } from "@core/router.js";
import { el } from "@shared/catalog/el.js";
import { formatRelativeTime, mapError } from "@shared/utils/format.js";
import { roleLabel } from "@shared/utils/roles.js";
import { icons } from "@shared/ui/icons.js";

import {
  deleteNotification,
  isComment,
  isCommentRemoved,
  isContentUpdated,
  isDraftActivity,
  isHeart,
  isNewContent,
  markNotificationRead,
} from "../data-access/notifications-api.js";
import type {
  AccountEventKind,
  AccountNotificationItem,
  CommentNotificationItem,
  CommentRemovedNotificationItem,
  ContentUpdatedNotificationItem,
  DraftActivityNotificationItem,
  HeartNotificationItem,
  NewContentNotificationItem,
  NotificationItem,
} from "../data-access/types.js";

/**
 * Host contract for the items section: the page-owned state the per-item
 * flows read or write, and the list operations they mutate through.
 */
export interface NotificationItemsHost extends ReactiveControllerHost {
  /** The page's shared action error (a failed mark-read, delete, read-all or
   * clear-read) — it renders as the banner above the list. */
  _readError: string;
  /** True once the guard redirected to /sign-in (a dead session) — the click
   * skips its navigation then. */
  readonly _redirected: boolean;
  /** Flip ONE row to read in place — the server confirmed the mark-read. */
  _markLocalRead(id: string): void;
  /** Drop ONE row after the server's 204. */
  _removeItem(id: string): void;
  /** Announce the read change so the header bell refetches its badge. */
  _dispatchChanged(): void;
}

/**
 * The item-scoped pending state, the navigation targets, and the per-kind row
 * templates. The page renders `template` for every item in its list.
 */
export class NotificationItemsSection implements ReactiveController {
  /** Ids with a mark-read POST in flight (click dedupe — the button is
   * disabled while one runs, so a double press cannot double-navigate). */
  private _markingIds = new Set<string>();

  /** Ids with a delete DELETE in flight (one at a time per row). */
  private _deletingIds = new Set<string>();

  private readonly _host: NotificationItemsHost;

  constructor(host: NotificationItemsHost) {
    this._host = host;
    host.addController(this);
  }

  /** The section owns no listeners or timers; the hook keeps the class
   * structurally assignable to the all-optional ReactiveController
   * interface. */
  hostConnected() {}

  /* ── Navigation targets ───────────────────────────────────────────── */

  /** The comment navigation target — the content deep link the thread
   * resolves via its `#comment-<id>` focus contract; the heart kind navigates
   * to the hearted comment the same way. */
  private _commentURL(
    item: CommentNotificationItem | HeartNotificationItem,
  ): string {
    const base = item.contentKind === "projects" ? "/projects" : "/blog";
    return `${base}/${item.contentId}#comment-${item.commentId}`;
  }

  /** The content_updated / new_content / comment_removed navigation target —
   * the content page itself, no comment deep link (content_updated and
   * new_content carry no commentId; comment_removed's comment no longer
   * exists, so its fragment would only land on the deleted-target notice).
   * draft_activity deliberately does NOT come through here: its content is
   * unpublished, so the public route has nothing to show (see
   * `_staffEditorURL`). */
  private _contentURL(
    item:
      | ContentUpdatedNotificationItem
      | NewContentNotificationItem
      | CommentRemovedNotificationItem,
  ): string {
    const base = item.contentKind === "projects" ? "/projects" : "/blog";
    return `${base}/${item.contentId}`;
  }

  /** The draft_activity navigation target — the STAFF editor, not the public
   * page: the content the kind names is unpublished, so `/blog/{id}` and
   * `/projects/{id}` would only mask it with a not-found state. The routes
   * are `admin-blog-edit`/`admin-projects-edit`; a viewer whose role dropped
   * below the staff floor gets the editor's own forbidden state, which needs
   * no handling here. */
  private _staffEditorURL(item: DraftActivityNotificationItem): string {
    const base =
      item.contentKind === "projects" ? "/admin/projects" : "/admin/blog";
    return `${base}/${item.contentId}`;
  }

  /* ── Per-item flows ───────────────────────────────────────────────── */

  /** Item click: account items mark-read with NO navigation (there is no
   * user-detail endpoint to click through to yet);
   * comment_reply/comment/heart items mark-read + navigate to the content
   * deep link; content_updated / new_content / comment_removed items
   * mark-read + navigate to the content page (the removed comment is gone,
   * so no fragment); draft_activity items mark-read + navigate to the staff
   * editor (see `_staffEditorURL`). The navigation waits for the mark-read
   * POST: a failure keeps the user here with the error banner and a
   * retryable item.
   *
   * The confirmed read flips the row LOCALLY, like "mark all": an account row
   * is the one kind whose click does not navigate, so without this the row
   * the user just opened would keep its unread look until a reload.
   */
  private async _onItemClick(item: NotificationItem) {
    if (this._markingIds.has(item.id)) return;
    this._markingIds.add(item.id);
    this._host._readError = "";
    this.refresh();
    try {
      await markNotificationRead(item.id);
      this._host._markLocalRead(item.id);
      this._host._dispatchChanged();
      // Skip the jump when the 401 kicked off the guard's /sign-in
      // redirect (a dead session) — the user is leaving anyway.
      if (!this._host._redirected) {
        if (isComment(item) || isHeart(item)) {
          navigate(this._commentURL(item));
        } else if (
          isContentUpdated(item) ||
          isNewContent(item) ||
          isCommentRemoved(item)
        ) {
          navigate(this._contentURL(item));
        } else if (isDraftActivity(item)) {
          navigate(this._staffEditorURL(item));
        }
      }
    } catch (err) {
      this._host._readError = mapError(err);
    } finally {
      this._markingIds.delete(item.id);
      this.refresh();
    }
  }

  /** Remove ONE feed entry. The row leaves the list on the server's 204 —
   * every row renders this control; the server deletes an event row or
   * dismisses a visible ledger row for the viewer. The badge event fires
   * because a removed UNREAD row changes the count (a read one does not, and
   * that costs one extra count fetch — accepted: one code path, not two). */
  private async _onDelete(item: NotificationItem) {
    if (this._deletingIds.has(item.id)) return;
    this._deletingIds.add(item.id);
    this._host._readError = "";
    this.refresh();
    try {
      await deleteNotification(item.id);
      this._host._removeItem(item.id);
      this._host._dispatchChanged();
    } catch (err) {
      this._host._readError = mapError(err);
    } finally {
      this._deletingIds.delete(item.id);
      this.refresh();
    }
  }

  /* ── Per-kind lines ───────────────────────────────────────────────── */

  private _verbFor(event: AccountEventKind): string {
    switch (event) {
      case "password_reset":
        return el.notifications.verbPasswordReset;
      case "role_changed":
        return el.notifications.verbRoleChanged;
      case "user_suspended":
        return el.notifications.verbUserSuspended;
      case "user_reactivated":
        return el.notifications.verbUserReactivated;
      case "user_deleted":
        return el.notifications.verbUserDeleted;
    }
  }

  /** The account-event item (mark-read, no navigation). */
  private _accountItemTemplate(item: AccountNotificationItem) {
    const actor = item.actorUsername ?? el.notifications.systemActor;
    const target = item.targetUsername ?? el.notifications.missingTarget;
    const role = roleLabel(item.targetRole);
    return html`
      <span class="notification-text">
        <strong class="notification-actor">${actor}</strong>
        ${this._verbFor(item.kind)}
        <strong class="notification-target">${target}</strong>
        ${role ? html`<span class="notification-role">${role}</span>` : ""}
      </span>
      <span class="notification-meta">
        <span class="notification-result notification-result-${item.result}"
          >${
            item.result === "failure"
              ? el.notifications.resultFailure
              : el.notifications.resultSuccess
          }</span
        >
        <time class="notification-time" datetime=${item.createdAt}
          >${formatRelativeTime(item.createdAt)}</time
        >
      </span>
    `;
  }

  /** The comment items: actor + the kind's verb + the content title (generic
   * label when the content is gone) — the click navigates to the comment's
   * deep link. */
  private _commentItemTemplate(item: CommentNotificationItem) {
    const actor = item.actorUsername ?? el.notifications.systemActor;
    const title = item.contentTitle ?? el.notifications.genericContent;
    const verb =
      item.kind === "comment_reply"
        ? el.notifications.verbCommentReply
        : el.notifications.verbComment;
    return html`
      <span class="notification-text">
        <strong class="notification-actor">${actor}</strong>
        ${verb}
        <strong class="notification-content-title">${title}</strong>
      </span>
      <span class="notification-meta">
        <time class="notification-time" datetime=${item.createdAt}
          >${formatRelativeTime(item.createdAt)}</time
        >
      </span>
    `;
  }

  /** The content-level items: actor + the kind's verb + the content title
   * (generic label when the content is gone) — the click navigates to the
   * content page itself. */
  private _contentItemTemplate(
    item: ContentUpdatedNotificationItem | NewContentNotificationItem,
  ) {
    const actor = item.actorUsername ?? el.notifications.systemActor;
    const title = item.contentTitle ?? el.notifications.genericContent;
    const verb =
      item.kind === "content_updated"
        ? el.notifications.verbContentUpdated
        : el.notifications.verbNewContent;
    return html`
      <span class="notification-text">
        <strong class="notification-actor">${actor}</strong>
        ${verb}
        <strong class="notification-content-title">${title}</strong>
      </span>
      <span class="notification-meta">
        <time class="notification-time" datetime=${item.createdAt}
          >${formatRelativeTime(item.createdAt)}</time
        >
      </span>
    `;
  }

  /** The heart item: the heart sentence form — "Το σχόλιό σου στο {title}
   * αρέσει σε {actor}" — the click navigates to the hearted comment's deep
   * link. */
  private _heartItemTemplate(item: HeartNotificationItem) {
    const actor = item.actorUsername ?? el.notifications.systemActor;
    const title = item.contentTitle ?? el.notifications.genericContent;
    return html`
      <span class="notification-text">
        ${el.notifications.verbHeartOpen}
        <strong class="notification-content-title">${title}</strong>
        ${el.notifications.verbHeartClose}
        <strong class="notification-actor">${actor}</strong>
      </span>
      <span class="notification-meta">
        <time class="notification-time" datetime=${item.createdAt}
          >${formatRelativeTime(item.createdAt)}</time
        >
      </span>
    `;
  }

  /** The comment_removed item: the actorless sentence — "Το σχόλιό σου στο
   * {title} αφαιρέθηκε." — no actor element at all, because the moderator
   * is never named. The click navigates to the content page (the comment is
   * gone). */
  private _commentRemovedItemTemplate(item: CommentRemovedNotificationItem) {
    const title = item.contentTitle ?? el.notifications.genericContent;
    return html`
      <span class="notification-text">
        ${el.notifications.verbCommentRemovedOpen}
        <strong class="notification-content-title">${title}</strong>
        ${el.notifications.verbCommentRemovedClose}
      </span>
      <span class="notification-meta">
        <time class="notification-time" datetime=${item.createdAt}
          >${formatRelativeTime(item.createdAt)}</time
        >
      </span>
    `;
  }

  /** The draft_activity item: actor + the verb its `draftAction` picks + the
   * content title (generic label when the content is gone) — the
   * content_updated line shape. The click lands on the staff editor instead
   * of the public page (see `_staffEditorURL`).
   *
   * Returns null when the action is one this client cannot word (the wire
   * pins created/updated/unpublished): `template` then renders no item at
   * all. A wordless or wrong-verb sentence would misstate what happened; the
   * service worker makes the same refusal when it raises no notification —
   * both drop the row, neither guesses. */
  private _draftActivityItemTemplate(item: DraftActivityNotificationItem) {
    const actor = item.actorUsername ?? el.notifications.systemActor;
    const title = item.contentTitle ?? el.notifications.genericContent;
    const verb = this._draftVerb(item.draftAction);
    if (verb === null) {
      console.warn("notifications: unknown draft action", item.draftAction);
      return null;
    }
    return html`
      <span class="notification-text">
        <strong class="notification-actor">${actor}</strong>
        ${verb}
        <strong class="notification-content-title">${title}</strong>
      </span>
      <span class="notification-meta">
        <time class="notification-time" datetime=${item.createdAt}
          >${formatRelativeTime(item.createdAt)}</time
        >
      </span>
    `;
  }

  /** The draft action → catalog verb. An action this client does not know
   * returns null — the signal to drop the item (see
   * `_draftActivityItemTemplate`). */
  private _draftVerb(action: string): string | null {
    switch (action) {
      case "created":
        return el.notifications.verbDraftCreated;
      case "updated":
        return el.notifications.verbDraftUpdated;
      case "unpublished":
        return el.notifications.verbDraftUnpublished;
      default:
        return null;
    }
  }

  /* ── The row ──────────────────────────────────────────────────────── */

  /** One feed row: the entry button (its read/unread look and per-kind line)
   * plus the delete control every row carries. */
  template(item: NotificationItem) {
    const marking = this._markingIds.has(item.id);
    // The delete control's three bindings (label, title, disabled) read ONE
    // local apiece, so they can never disagree about the pending state.
    const deleting = this._deletingIds.has(item.id);
    const deleteLabel = deleting
      ? el.notifications.deleteOnePending
      : el.notifications.deleteOne;
    // The draft_activity body is built before the literal so its refusal of
    // an unknown action can drop the whole item, wrapper included — an empty
    // clickable row is no better than a broken line.
    const draftBody = isDraftActivity(item)
      ? this._draftActivityItemTemplate(item)
      : null;
    if (isDraftActivity(item) && draftBody === null) {
      return nothing;
    }
    return html`
      <li class="notification-row has-delete">
        <button
          type="button"
          class="notification-item${item.read ? " is-read" : " is-unread"}"
          ?disabled=${marking}
          @click=${() => this._onItemClick(item)}
        >
          ${
            // The read state is otherwise a visual class; unread rows carry it
            // in the accessible name too.
            item.read
              ? ""
              : html`<span class="sr-only">${el.notifications.unreadRow}</span>`
          }
          ${
            isComment(item)
              ? this._commentItemTemplate(item)
              : isHeart(item)
                ? this._heartItemTemplate(item)
                : isCommentRemoved(item)
                  ? this._commentRemovedItemTemplate(item)
                  : isContentUpdated(item) || isNewContent(item)
                    ? this._contentItemTemplate(item)
                    : isDraftActivity(item)
                      ? draftBody
                      : this._accountItemTemplate(item)
          }
        </button>
        <button
          type="button"
          class="button button--icon button--ghost notification-delete"
          aria-label=${deleteLabel}
          title=${deleteLabel}
          ?disabled=${deleting}
          @click=${() => this._onDelete(item)}
        >
          ${icons.trash}
        </button>
      </li>
    `;
  }

  /** The item-scoped state is plain now — every mutation asks for a render. */
  private refresh() {
    this._host.requestUpdate();
  }
}
