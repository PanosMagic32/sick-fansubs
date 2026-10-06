/**
 * Comment composers — the top-level composer, the inline reply composer,
 * and the author-only edit form of <comments-thread>.
 *
 * A reactive section controller: the thread owns the list, the sort, the URL
 * sync, and the deep-link arrival; this controller owns the composer fields,
 * their handlers, and their templates. The templates render into the
 * thread's shadow root — one shared tree is the contract (the CSS classes,
 * the `#comment-<id>` anchors, and the deep-link scroll all live in it).
 *
 * A create/reply/edit calls back into the thread's list operations through
 * the host: the new target id goes through `_jumpTo` (replaceState, reload
 * with focus, count refetch), the edit result through `_patchComment`. Every
 * state change requests the host's re-render.
 */

import {
  html,
  type ReactiveController,
  type ReactiveControllerHost,
} from "lit";

import { ValidationError } from "@shared/api/client.js";
import { el, violationMessage } from "@shared/catalog/el.js";
import { initialOf, mapError } from "@shared/utils/format.js";
import type { SessionContext } from "@features/auth/data-access/session.js";

import {
  createComment,
  replyToComment,
  updateComment,
} from "../data-access/comments-api.js";
import type { CommentItem, CommentKind } from "../data-access/types.js";

/**
 * Host contract for the composers section: the thread state the composers
 * read and the list operations they call back into.
 */
export interface CommentComposersHost extends ReactiveControllerHost {
  kind: CommentKind;
  contentId: string;
  session?: SessionContext;
  _patchComment(id: string, patch: (c: CommentItem) => CommentItem): void;
  _jumpTo(focusId: string | null): void;
}

/**
 * The three comment forms and their one-at-a-time state. The thread renders
 * `composerTemplate`, and the items section renders `editTemplate` and
 * `replyComposerTemplate` inside each comment.
 */
export class CommentComposersSection implements ReactiveController {
  // Composer state.
  private _body = "";
  private _submitting = false;
  private _composerError = "";

  // Reply composer state (one open at a time, YouTube-style).
  private _replyTargetId = "";
  private _replyBody = "";
  private _replySubmitting = false;
  private _replyError = "";

  // Edit state (author only).
  private _editId = "";
  private _editBody = "";
  private _editSubmitting = false;
  private _editError = "";

  private readonly _host: CommentComposersHost;

  constructor(host: CommentComposersHost) {
    this._host = host;
    host.addController(this);
  }

  /** The composers own no listeners or timers; the hook exists so the class
   * stays assignable to Lit's all-optional ReactiveController interface. */
  hostConnected() {}

  composerTemplate() {
    const s = this._host.session?.state;
    if (s?.status !== "authenticated") {
      return html`
        <div class="ct-composer">
          <span class="ct-avatar ct-avatar-empty" aria-hidden="true"></span>
          <p class="ct-signin-prompt">
            ${el.comments.signInPrompt}
            <a href="/sign-in">${el.comments.signInLink}</a>
          </p>
        </div>
      `;
    }
    const username = s.user.username;
    return html`
      <div class="ct-composer">
        <span class="ct-avatar ct-avatar-initial" aria-hidden="true"
          >${initialOf(username)}</span
        >
        <div class="ct-composer-box">
          <!-- The composer opens with the comment byline shape (.ct-meta),
               so the name reads as the draft's author, not a control label. -->
          <p class="ct-meta">
            <span class="ct-name">${username}</span>
          </p>
          <textarea
            class="ct-textarea"
            rows="3"
            .value=${this._body}
            placeholder=${el.comments.composerPlaceholder}
            aria-label=${el.comments.composerLabel}
            @input=${this._onComposerInput}
          ></textarea>
          ${
            this._composerError
              ? html`<p class="ct-form-error" role="alert">
                  ${this._composerError}
                </p>`
              : ""
          }
          <div class="ct-composer-foot">
            <button
              type="button"
              class="button button--primary ct-submit"
              ?disabled=${this._submitting}
              @click=${this._submitComment}
            >
              ${
                this._submitting
                  ? el.comments.submitPending
                  : el.comments.submit
              }
            </button>
          </div>
        </div>
      </div>
    `;
  }

  private _onComposerInput = (e: Event) => {
    this._body = (e.target as HTMLTextAreaElement).value;
    this.refresh();
  };

  /** Bound: Lit invokes a directly-bound listener with the HOST as `this`. */
  private _submitComment = async () => {
    const body = this._body.trim();
    if (!body || this._submitting) return;
    this._submitting = true;
    this._composerError = "";
    this.refresh();
    try {
      const res = await createComment(
        this._host.kind,
        this._host.contentId,
        body,
      );
      this._body = "";
      this._replyTargetId = "";
      this.refresh();
      this._host._jumpTo(res.focusId);
    } catch (err) {
      this._composerError = this._formError(err);
      this.refresh();
    } finally {
      this._submitting = false;
      this.refresh();
    }
  };

  /** Open (or toggle shut) the inline reply composer of one comment. */
  openReply(id: string) {
    // Toggle: clicking the same comment's reply action closes the composer.
    this._replyTargetId = this._replyTargetId === id ? "" : id;
    this._replyBody = "";
    this._replyError = "";
    this.refresh();
  }

  private _onReplyInput = (e: Event) => {
    this._replyBody = (e.target as HTMLTextAreaElement).value;
    this.refresh();
  };

  /** Bound for the same reason as `_submitComment`. */
  private _submitReply = async () => {
    const body = this._replyBody.trim();
    const parentId = this._replyTargetId;
    if (!body || !parentId || this._replySubmitting) return;
    this._replySubmitting = true;
    this._replyError = "";
    this.refresh();
    try {
      const res = await replyToComment(
        this._host.kind,
        this._host.contentId,
        parentId,
        body,
      );
      this._replyTargetId = "";
      this.refresh();
      this._host._jumpTo(res.focusId);
    } catch (err) {
      this._replyError = this._formError(err);
      this.refresh();
    } finally {
      this._replySubmitting = false;
      this.refresh();
    }
  };

  /** Open the author-only edit form for one comment. */
  openEdit(c: CommentItem) {
    this._editId = c.id;
    this._editBody = c.body;
    this._editError = "";
    this.refresh();
  }

  private _closeEdit = () => {
    this._editId = "";
    this._editBody = "";
    this._editError = "";
    this.refresh();
  };

  private _onEditInput = (e: Event) => {
    this._editBody = (e.target as HTMLTextAreaElement).value;
    this.refresh();
  };

  /** Bound for the same reason as `_submitComment`. */
  private _submitEdit = async () => {
    const body = this._editBody.trim();
    if (!body || !this._editId || this._editSubmitting) return;
    this._editSubmitting = true;
    this._editError = "";
    this.refresh();
    try {
      const res = await updateComment(
        this._host.kind,
        this._host.contentId,
        this._editId,
        body,
      );
      // The server returns the refreshed item; only the edited fields are
      // consumed here (the local item keeps its own window/cursor state).
      this._host._patchComment(this._editId, (c) => ({
        ...c,
        body: res.body,
        updatedAt: res.updatedAt,
      }));
      this._closeEdit();
    } catch (err) {
      this._editError = this._formError(err);
      this.refresh();
    } finally {
      this._editSubmitting = false;
      this.refresh();
    }
  };

  /** The edit form of one comment; empty unless that comment is open. */
  editTemplate(c: CommentItem) {
    if (this._editId !== c.id) return "";
    return html`
      <div class="ct-editor">
        <textarea
          class="ct-textarea"
          rows="3"
          .value=${this._editBody}
          aria-label=${el.comments.editLabel}
          @input=${this._onEditInput}
        ></textarea>
        ${
          this._editError
            ? html`<p class="ct-form-error" role="alert">${this._editError}</p>`
            : ""
        }
        <div class="ct-editor-actions">
          <button
            type="button"
            class="button button--ghost ct-action"
            @click=${this._closeEdit}
          >
            ${el.comments.cancel}
          </button>
          <button
            type="button"
            class="button button--primary ct-submit"
            ?disabled=${this._editSubmitting}
            @click=${this._submitEdit}
          >
            ${this._editSubmitting ? el.comments.savePending : el.comments.save}
          </button>
        </div>
      </div>
    `;
  }

  /** The inline reply composer of one comment; empty unless it is open. */
  replyComposerTemplate(parentId: string) {
    if (this._replyTargetId !== parentId) return "";
    if (this._host.session?.state.status !== "authenticated") {
      return html`
        <p class="ct-signin-prompt">
          ${el.comments.signInPrompt}
          <a href="/sign-in">${el.comments.signInLink}</a>
        </p>
      `;
    }
    return html`
      <div class="ct-reply-composer">
        <textarea
          class="ct-textarea"
          rows="2"
          .value=${this._replyBody}
          placeholder=${el.comments.replyPlaceholder}
          aria-label=${el.comments.replyLabel}
          @input=${this._onReplyInput}
        ></textarea>
        ${
          this._replyError
            ? html`<p class="ct-form-error" role="alert">
                ${this._replyError}
              </p>`
            : ""
        }
        <div class="ct-editor-actions">
          <button
            type="button"
            class="button button--ghost ct-action"
            @click=${() => this.openReply(parentId)}
          >
            ${el.comments.cancel}
          </button>
          <button
            type="button"
            class="button button--primary ct-submit"
            ?disabled=${this._replySubmitting}
            @click=${this._submitReply}
          >
            ${
              this._replySubmitting
                ? el.comments.replySubmitPending
                : el.comments.replySubmit
            }
          </button>
        </div>
      </div>
    `;
  }

  /** A comment form failure: the body violation code when the server 422s
   * (required/maxLength/invalidFormat), else the shared mapper. */
  private _formError(err: unknown): string {
    if (err instanceof ValidationError) {
      const v =
        err.violations?.find((x) => x.field === "body") ?? err.violations?.[0];
      if (v) return violationMessage(v.code);
    }
    return mapError(err);
  }

  /** The composer fields are plain now — every mutation asks for a render. */
  private refresh() {
    this._host.requestUpdate();
  }
}
