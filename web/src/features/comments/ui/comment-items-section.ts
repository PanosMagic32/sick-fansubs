/**
 * Comment items — the per-comment rendering and per-comment mutations of
 * <comments-thread>: the meta line, the staff badge, the action row (hearts,
 * reply, edit, delete), the inline reply window, and its append page.
 *
 * A reactive section controller: the thread owns the list, the sort, the URL
 * sync, the count, and the deep-link arrival; this controller owns the
 * per-comment pending/error state and the templates. They render into the
 * thread's shadow root — one shared tree is the contract (the CSS classes
 * and the `#comment-<id>` anchors live in it) — and the host's `focusId`
 * drives the target chip and the highlight.
 *
 * List mutations go through the host's `_patchComment`/`_removeComment`, and
 * every state change requests the host's re-render. The reply append owns one
 * AbortController per parent; `abortReplyPages` drops them ALL and the thread
 * calls it on every list reload, so a stale page can never append onto a
 * fresh window.
 */

import {
  html,
  nothing,
  type ReactiveController,
  type ReactiveControllerHost,
} from "lit";

import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import {
  formatRelativeTime,
  initialOf,
  mapError,
} from "@shared/utils/format.js";
import { isStaffRole, roleLabel } from "@shared/utils/roles.js";
import { icons } from "@shared/ui/icons.js";
import { ToggleDebouncer } from "@shared/utils/toggle-debouncer.js";
import type { SessionContext } from "@features/auth/data-access/session.js";

import {
  REPLY_PAGE_SIZE,
  deleteComment,
  listReplies,
  removeHeart,
  setHeart,
} from "../data-access/comments-api.js";
import type {
  CommentAuthor,
  CommentItem,
  CommentKind,
} from "../data-access/types.js";
import { commentBodyTemplate } from "./emoticons.js";
import type { CommentComposersSection } from "./comment-composers-section.js";

/**
 * Host contract for the items section: the thread state the per-comment
 * rendering reads and the list operations it mutates through.
 */
export interface CommentItemsHost extends ReactiveControllerHost {
  kind: CommentKind;
  contentId: string;
  session?: SessionContext;
  /** The `#comment-<id>` deep-link target (the target chip + highlight). */
  readonly focusId: string;
  /** The composers section — each comment renders its edit form and its
   * inline reply composer from there (the narrow surface it exposes). */
  readonly composers: Pick<
    CommentComposersSection,
    "openReply" | "openEdit" | "editTemplate" | "replyComposerTemplate"
  >;
  _findComment(id: string): CommentItem | null;
  _patchComment(id: string, patch: (c: CommentItem) => CommentItem): void;
  _removeComment(id: string): void;
  _loadCount(): void;
}

/**
 * The per-comment template tree plus the heart/delete/reply-append flows.
 * The host renders `commentTemplate` for every top-level item.
 */
export class CommentItemsSection implements ReactiveController {
  /** The action error banner the thread renders above the list (a delete
   * failure). */
  private _actionError = "";

  /** The current action error (read by the thread's state template). */
  get actionError(): string {
    return this._actionError;
  }

  // Per-comment mutation state (hearts: the confirmed-update machine;
  // deletes: in-flight set).
  private _heartPending = new Set<string>();
  private _heartErrors = new Map<string, string>();
  private _deleting = new Set<string>();

  /** Per-comment desired heart target while the debounce window is open
   * (comment id → desired hearted). Absence = idle for that comment. */
  private _heartDesired = new Map<string, boolean>();

  /** The shared trailing-edge debouncer, one window per comment id. */
  private readonly _debouncer = new ToggleDebouncer();

  // Per-parent reply pagination: which items are appending, and the
  // per-item failure notice.
  private _replyLoading = new Set<string>();
  private _replyErrors = new Map<string, string>();

  /** In-flight reply-page appends, one per parent. A list reload aborts them
   * ALL: a stale reply page must never append onto a fresh window (it would
   * duplicate replies already in the new window and regress the cursor). */
  private _replyAborts = new Map<string, AbortController>();

  private readonly _host: CommentItemsHost;

  constructor(host: CommentItemsHost) {
    this._host = host;
    host.addController(this);
  }

  hostDisconnected() {
    this._debouncer.cancel();
    this._heartDesired.clear(); // drop every open-window target — a
    // re-added instance must not inherit stale is-pending states
    this.abortReplyPages();
  }

  /** Abort every in-flight reply append and drop its per-item state. Called
   * by the thread on every list reload. */
  abortReplyPages() {
    for (const controller of this._replyAborts.values()) controller.abort();
    this._replyAborts.clear();
    this._replyLoading.clear();
    this._replyErrors.clear();
    this.refresh();
  }

  private async _onDelete(c: CommentItem) {
    if (this._deleting.has(c.id)) return;
    if (!window.confirm(el.comments.deleteConfirm)) return;
    this._deleting.add(c.id);
    this._actionError = "";
    this.refresh();
    try {
      await deleteComment(this._host.kind, this._host.contentId, c.id);
      // Hard cascade: a top-level delete drops its replies with it.
      this._host._removeComment(c.id);
      void this._host._loadCount();
    } catch (err) {
      this._actionError = mapError(err);
      this.refresh();
    } finally {
      this._deleting.delete(c.id);
      this.refresh();
    }
  }

  /** The first click opens the debounce window for this comment: the
   * desired target flips per click (last wins); delivery fires when the
   * stream settles. */
  private _onHeart(c: CommentItem) {
    if (this._heartPending.has(c.id)) return;
    this._heartErrors.delete(c.id);
    const current = this._heartDesired.get(c.id) ?? c.hearted;
    this._heartDesired.set(c.id, !current);
    this._debouncer.schedule(c.id, () => {
      void this._deliverHeart(c.id);
    });
    this.refresh();
  }

  /** The window for one comment expired: skip net-zero bursts (even click
   * count), else send ONE mutation for the final desired target. */
  private async _deliverHeart(id: string) {
    const item = this._host._findComment(id);
    if (!item) {
      // Paged out or deleted while the window was open — drop the desire.
      this._heartDesired.delete(id);
      this.refresh();
      return;
    }
    const desired = this._heartDesired.get(id) ?? item.hearted;
    if (desired === item.hearted) {
      this._heartDesired.delete(id);
      this.refresh();
      return;
    }
    this._heartPending.add(id);
    this.refresh();
    try {
      if (desired) {
        await setHeart(this._host.kind, this._host.contentId, id);
      } else {
        await removeHeart(this._host.kind, this._host.contentId, id);
      }
      // Confirmed update — flip only on success.
      this._host._patchComment(id, (it) => ({
        ...it,
        hearted: desired,
        heartsCount: it.heartsCount + (it.hearted ? -1 : 1),
      }));
    } catch (err) {
      this._heartErrors.set(id, mapError(err));
      this.refresh();
    } finally {
      this._heartPending.delete(id);
      this._heartDesired.delete(id);
      this.refresh();
    }
  }

  /** Append the next reply page of one top-level item. The item's
   * `repliesEndCursor` is the reader's continuation — the server mints it
   * from the inline window's last reply, so the client never builds a
   * cursor itself. */
  private async _loadMoreReplies(parent: CommentItem) {
    const after = parent.repliesEndCursor;
    if (!after || this._replyLoading.has(parent.id)) return;
    const controller = new AbortController();
    this._replyAborts.set(parent.id, controller);
    this._replyLoading.add(parent.id);
    this._replyErrors.delete(parent.id);
    this.refresh();
    try {
      const page = await listReplies(
        this._host.kind,
        this._host.contentId,
        parent.id,
        { limit: REPLY_PAGE_SIZE, after },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted) return;
      this._appendReplies(
        parent.id,
        after,
        page.items,
        page.pageInfo.endCursor ?? undefined,
      );
    } catch (err) {
      if (controller.signal.aborted) return;
      this._replyErrors.set(parent.id, mapError(err));
      this.refresh();
    } finally {
      this._replyAborts.delete(parent.id);
      this._replyLoading.delete(parent.id);
      this.refresh();
    }
  }

  /** Append one reply page in place; a null end cursor ends the window's
   * continuation, which hides the button. The `after` compare-and-swap
   * drops a page whose window has been replaced meanwhile (a reload already
   * aborted it; this is the belt-and-braces guard). */
  private _appendReplies(
    parentId: string,
    after: string,
    more: CommentItem[],
    nextCursor: string | undefined,
  ) {
    this._host._patchComment(parentId, (top) =>
      top.repliesEndCursor === after
        ? {
            ...top,
            replies: [...(top.replies ?? []), ...more],
            repliesEndCursor: nextCursor,
          }
        : top,
    );
  }

  /* ── Session-derived predicates ─────────────────────────────────── */

  private _isAuthed(): boolean {
    return this._host.session?.state.status === "authenticated";
  }

  private _isOwnComment(c: CommentItem): boolean {
    const s = this._host.session?.state;
    return s?.status === "authenticated" && s.user.id === c.author.id;
  }

  private _canEdit(c: CommentItem): boolean {
    return this._isOwnComment(c);
  }

  private _canDelete(c: CommentItem): boolean {
    const s = this._host.session?.state;
    if (s?.status !== "authenticated") return false;
    return s.user.id === c.author.id || isStaffRole(s.user.role);
  }

  /* ── Templates ──────────────────────────────────────────────────── */

  private _avatarTemplate(author: CommentAuthor) {
    if (author.avatarUrl) {
      return html`<img class="ct-avatar" src=${author.avatarUrl} alt="" />`;
    }
    return html`<span class="ct-avatar ct-avatar-initial" aria-hidden="true"
      >${initialOf(author.username)}</span
    >`;
  }

  private _actionsTemplate(c: CommentItem) {
    const pending = this._heartPending.has(c.id);
    const windowOpen = this._heartDesired.has(c.id);
    const heartError = this._heartErrors.get(c.id);
    // The delete's three bindings (label, title, disabled) read ONE local, so
    // they can never disagree about the pending state.
    const deleting = this._deleting.has(c.id);
    const deleteLabel = deleting
      ? el.comments.deletePending
      : el.comments.delete;
    return html`
      <div class="ct-actions">
        ${
          this._isAuthed()
            ? this._isOwnComment(c)
              ? // No heart element at all on own comments — self-hearts are
                // forbidden, the server rejects them and the UI never offers
                // the affordance.
                ""
              : html`<button
                  type="button"
                  class="button button--icon button--ghost ct-action ct-heart${c.hearted ? " is-hearted" : ""}${windowOpen || pending ? " is-pending" : ""}"
                  aria-pressed=${c.hearted ? "true" : "false"}
                  aria-busy=${windowOpen || pending ? "true" : "false"}
                  aria-label=${
                    c.hearted
                      ? el.comments.heartRemoveAria
                      : el.comments.heartAddAria
                  }
                  ?disabled=${pending}
                  @click=${() => this._onHeart(c)}
                >
                  <span class="ct-heart-glyph" aria-hidden="true"
                    >${c.hearted ? icons.heartFilled : icons.heart}</span
                  >
                  ${c.heartsCount}
                </button>`
            : html`<span class="ct-hearts">
                <span class="ct-heart-glyph" aria-hidden="true"
                  >${icons.heartFilled}</span
                >
                ${c.heartsCount}
              </span>`
        }
        ${
          c.replies !== undefined
            ? html`<button
                type="button"
                class="button button--icon button--ghost ct-action"
                aria-label=${el.comments.reply}
                title=${el.comments.reply}
                @click=${() => this._host.composers.openReply(c.id)}
              >
                ${icons.reply}
              </button>`
            : ""
        }
        ${
          this._canEdit(c)
            ? html`<button
                type="button"
                class="button button--icon button--ghost ct-action"
                aria-label=${el.comments.edit}
                title=${el.comments.edit}
                @click=${() => this._host.composers.openEdit(c)}
              >
                ${icons.edit}
              </button>`
            : ""
        }
        ${
          this._canDelete(c)
            ? html`<button
                type="button"
                class="button button--icon button--ghost ct-action ct-delete"
                aria-label=${deleteLabel}
                title=${deleteLabel}
                ?disabled=${deleting}
                @click=${() => this._onDelete(c)}
              >
                ${icons.trash}
              </button>`
            : ""
        }
      </div>
      ${
        heartError
          ? html`<p class="ct-form-error" role="alert">${heartError}</p>`
          : ""
      }
    `;
  }

  private _metaTemplate(c: CommentItem) {
    return html`
      <span class="ct-name">${c.author.username}</span>
      ${this._badgeTemplate(c.author)}
      ${
        this._host.focusId === c.id
          ? html`<span class="ct-target-chip">${el.comments.targetChip}</span>`
          : ""
      }
      <time class="ct-time" .datetime=${c.createdAt}
        >${formatRelativeTime(c.createdAt)}</time
      >
      ${
        c.updatedAt > c.createdAt
          ? html`<span class="ct-edited">· ${el.comments.edited}</span>`
          : ""
      }
    `;
  }

  /**
   * The staff badge. The badge's BORDER color reads the derived staffRole,
   * and the role name also joins the badge's TEXT for assistive tech — a
   * visually hidden suffix, because an `aria-label` on a plain `<span>` is
   * ignored (its role is `generic`, which cannot be named). The visible
   * «Ομάδα» stays first, so the label-in-name rule holds, and the role is
   * never carried by color alone. Role copy comes from the shared
   * `roleLabel` map, which falls back to the catalogue's `roleUnknown`
   * rather than leaking a raw identifier; an absent role degrades to the
   * plain badge.
   */
  private _badgeTemplate(a: CommentAuthor) {
    if (!a.isStaff) return "";
    const role = a.staffRole ? roleLabel(a.staffRole) : null;
    return html`<span
      class="ct-badge${a.staffRole ? ` ct-badge--${a.staffRole}` : ""}"
      title=${role ?? nothing}
      >${el.comments.staffBadge}${
        role ? html`<span class="sr-only">: ${role}</span>` : ""
      }</span
    >`;
  }

  /** The replies block plus its append affordance. The button renders only
   * while `repliesEndCursor` says more exist after the window. */
  private _repliesTemplate(c: CommentItem) {
    const replies = c.replies ?? [];
    if (replies.length === 0 && !c.repliesEndCursor) return "";
    return html`
      <div class="ct-replies">
        ${replies.map((r) => this._replyTemplate(r))}
        ${this._repliesMoreTemplate(c)}
      </div>
    `;
  }

  private _repliesMoreTemplate(c: CommentItem) {
    if (!c.repliesEndCursor) return "";
    const pending = this._replyLoading.has(c.id);
    const error = this._replyErrors.get(c.id);
    return html`
      <button
        type="button"
        class="button button--secondary button--sm ct-more ct-more-replies"
        ?disabled=${pending}
        @click=${() => this._loadMoreReplies(c)}
      >
        ${pending ? el.comments.loadingMore : this._repliesMoreLabel(c)}
      </button>
      ${error ? html`<p class="ct-form-error" role="alert">${error}</p>` : ""}
    `;
  }

  /** The total reply count rides the label — the item's inline window may be
   * a mid-page focus window, and the total is the honest number there. */
  private _repliesMoreLabel(c: CommentItem): string {
    const total = c.replyCount ?? 0;
    return total > 0
      ? `${el.comments.loadMoreReplies} (${total})`
      : el.comments.loadMoreReplies;
  }

  private _replyTemplate(r: CommentItem) {
    return html`
      <div
        class="ct-comment ct-reply${this._host.focusId === r.id ? " is-target" : ""}"
        id="comment-${r.id}"
      >
        ${this._avatarTemplate(r.author)}
        <div class="ct-body">
          <p class="ct-meta">${this._metaTemplate(r)}</p>
          <p class="ct-text">${commentBodyTemplate(r.body)}</p>
          ${this._actionsTemplate(r)} ${this._host.composers.editTemplate(r)}
        </div>
      </div>
    `;
  }

  /** One top-level comment with its actions, edit form, reply composer and
   * reply window. */
  commentTemplate(c: CommentItem) {
    return html`
      <div
        class="ct-comment${this._host.focusId === c.id ? " is-target" : ""}"
        id="comment-${c.id}"
      >
        ${this._avatarTemplate(c.author)}
        <div class="ct-body">
          <p class="ct-meta">${this._metaTemplate(c)}</p>
          <p class="ct-text">${commentBodyTemplate(c.body)}</p>
          ${this._actionsTemplate(c)} ${this._host.composers.editTemplate(c)}
          ${this._host.composers.replyComposerTemplate(c.id)}
          ${this._repliesTemplate(c)}
        </div>
      </div>
    `;
  }

  /** The per-comment state is plain now — every mutation asks for a render. */
  private refresh() {
    this._host.requestUpdate();
  }
}
