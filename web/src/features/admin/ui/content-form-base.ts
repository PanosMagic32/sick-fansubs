/**
 * Shared skeleton of the two admin content forms — the blog post form and
 * the project form.
 *
 * Create and edit are the same machine for both kinds: guard the route
 * (anonymous → /sign-in, below moderator → forbidden, create additionally
 * admin+), latch the first authenticated load, load the row into the form's
 * fields, run one of the four actions (`save`, `publish`, `unpublish`,
 * `archive`) behind an in-flight latch, map a failed write (412 reloads the
 * fresh revision and shows the stale copy, 403 heals a stale CSRF through a
 * silent session revalidate, a validation failure answers on the field
 * line), and delete behind the confirm.
 *
 * The subclass owns what the kind IS: its extra row field (the blog
 * subtitle, the project slug) and its extra form fields, the write body and
 * API calls, the success/deleted copy, the post-save navigation, and the
 * whole <form> markup with its native submit button.
 */

import { consume } from "@lit/context";
import { html, LitElement, type PropertyValues } from "lit";
import { state } from "lit/decorators.js";

import { navigate, redirect } from "@core/router.js";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { ApiError, ValidationError } from "@shared/api/client.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el, violationMessage } from "@shared/catalog/el.js";
import { mapError } from "@shared/utils/format.js";
import { isContentCreatorRole, isStaffRole } from "@shared/utils/roles.js";

import type { DownloadRows, DownloadRowValue } from "./download-rows.js";
import type { MediaUpload } from "./media-upload.js";

type PageStatus = "loading" | "error" | "forbidden" | "ready" | "notFound";

/**
 * The form's actions. Each one names the state it produces, so the editor
 * never has to translate a control into an outcome: `save` keeps the row
 * where it is (create: draft), `publish` publishes, `unpublish` pulls a
 * published row back to draft, `archive` takes it off the public site
 * without deleting it.
 */
export type FormAction = "save" | "publish" | "unpublish" | "archive";

/** The row states both admin kinds share (the Go status values). */
export type ContentStatus = "draft" | "published" | "archived";

export abstract class ContentFormBase<
  TRow extends {
    title: string;
    description: string;
    status: ContentStatus;
    revision: number;
  },
  TBody,
> extends LitElement {
  @consume({ context: sessionContext, subscribe: true })
  protected session?: SessionContext;

  @state() protected status: PageStatus = "loading";
  @state() protected errorMessage = "";

  // Form fields shared by both kinds.
  @state() protected _title = "";
  @state() protected _description = "";
  /** The row's state (edit) or the state a plain save would land in
   * (create: a draft). Never a free-floating form field — the action
   * buttons carry the transition. */
  @state() protected _status: ContentStatus = "draft";
  @state() protected _fieldError = "";
  /** The client-side required-title error, wired to the title input (the
   * field-level shape); every other field error stays on the page line. */
  @state() protected _titleError = "";

  /** The loaded row in edit mode (revision + stored path live here). */
  protected _row: TRow | null = null;

  @state() protected _pending = false;
  /** Which action is in flight — drives the button's pending label. */
  @state() protected _pendingAction: FormAction | null = null;
  @state() protected _success = "";
  /** The field-level submit error line (cleared on input). */
  @state() protected _error = "";

  private _abort: AbortController | null = null;
  private _redirected = false;
  private _started = false;
  /** Stored download rows waiting for the widget to render; updated()
   * consumes them once the element exists. */
  private _pendingRowLoad: DownloadRowValue[] | null = null;

  // ── Subclass configuration ────────────────────────────────────────────

  /** The row id the route carries; "" means create mode. */
  protected abstract get _rowId(): string;

  /** Fetches the edit row through the kind's data-access call. */
  protected abstract _fetchRow(id: string, signal: AbortSignal): Promise<TRow>;

  /** Maps the stored download rows into the widget's value shape. */
  protected abstract _downloadRows(row: TRow): DownloadRowValue[];

  /** Builds the write body for the action's target state, or null after
   * setting the field error when a client pre-check fails. */
  protected abstract _writeBody(status: ContentStatus): TBody | null;

  /** Writes the body — update in edit mode, create otherwise — and performs
   * the action's outcome (navigation, success line). Throws on a failed
   * call; the base maps the failure. */
  protected abstract _save(
    action: FormAction,
    status: ContentStatus,
    body: TBody,
  ): Promise<void>;

  /** The kind's delete call, behind the shared confirm. */
  protected abstract _deleteRow(id: string, revision: number): Promise<void>;

  /** The kind's staff list, where archive/create-draft and delete land. */
  protected abstract get _listPath(): string;

  /** The success line a plain save shows. */
  protected abstract get _savedLabel(): string;

  /** The success line a completed delete shows. */
  protected abstract get _deletedLabel(): string;

  /** Copies the kind's extra row fields (the blog subtitle, the project
   * slug) — the base owns the fields every content row shares. */
  protected _applyKindFields(_row: TRow): void {}

  // ── Lifecycle ─────────────────────────────────────────────────────────

  connectedCallback() {
    super.connectedCallback();
    this._guard();
  }

  disconnectedCallback() {
    this._abort?.abort();
    super.disconnectedCallback();
  }

  updated(changed: PropertyValues) {
    super.updated(changed);
    this._guard();
    // The latch is gated on the guard's own predicate: a below-floor viewer
    // must not load a row (the staff-detail GET would 403) and must never
    // render the create form for one cycle before the forbidden state
    // settles. The sibling staff base pins the same no-request floor.
    if (!this._started && this._allowed()) {
      this._started = true;
      if (this._rowId) {
        void this._loadRow();
      } else {
        this.status = "ready";
      }
    }
    if (this._pendingRowLoad !== null) {
      const rows = this._pendingRowLoad;
      this._pendingRowLoad = null;
      // The widget renders with the ready state — the post-render hook loads
      // the stored rows once the element exists (never updateComplete.then).
      this._downloadWidget()?.load(rows);
    }
  }

  /** In edit mode the route carries a row id; create mode renders the empty
   * form. */
  protected get _isEdit(): boolean {
    return this._rowId !== "";
  }

  /** Delete is admin+ — a moderator edit never renders the delete button, so
   * the form cannot request a capability the session lacks (the server
   * enforces too). */
  protected get _canDelete(): boolean {
    const s = this.session?.state;
    if (!s || s.status !== "authenticated") return false;
    return isContentCreatorRole(s.user.role);
  }

  /** The message a 412 shows — overridable because the copy names the kind's
   * noun ("the blog post" vs "the project"). */
  protected get _preconditionMessage(): string {
    return el.problems["/problems/precondition-failed"];
  }

  /** The rendered <download-rows> widget (null before it renders). */
  protected _downloadWidget(): DownloadRows | null {
    return (
      this.shadowRoot?.querySelector<DownloadRows>("download-rows") ?? null
    );
  }

  /** The rendered <media-upload> widget (null before it renders). */
  protected _mediaWidget(): MediaUpload | null {
    return this.shadowRoot?.querySelector<MediaUpload>("media-upload") ?? null;
  }

  /** The route's guard predicate, recomputed from the live session: an
   * authenticated staff member, plus the create route's admin+ requirement
   * (the server enforces the same floor). */
  private _allowed(): boolean {
    const s = this.session?.state;
    if (s?.status !== "authenticated") return false;
    if (!isStaffRole(s.user.role)) return false;
    return this._isEdit || isContentCreatorRole(s.user.role);
  }

  private _guard() {
    if (this._redirected) return;
    const s = this.session?.state;
    if (!s || s.status === "initializing") return;
    if (s.status === "anonymous") {
      this._redirected = true;
      redirect("/sign-in");
      return;
    }
    if (s.status === "error") {
      this.status = "error";
      this.errorMessage = el.ui.serverConnectionFailed;
      return;
    }
    if (!this._allowed()) {
      this.status = "forbidden";
      return;
    }
  }

  /** Loads the edit row: the shared abort/error skeleton, with the field
   * copy and the download rows left to the kind. */
  protected async _loadRow() {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this.status = "loading";
    this.errorMessage = "";

    try {
      const row = await this._fetchRow(this._rowId, requestSignal(controller));
      if (controller.signal.aborted) return;
      this._row = row;
      this._title = row.title;
      this._description = row.description;
      this._status = row.status;
      this._applyKindFields(row);
      this.status = "ready";
      // The widget renders with the ready state; updated() loads the stored
      // rows once the element exists.
      this._pendingRowLoad = this._downloadRows(row);
    } catch (err) {
      if (controller.signal.aborted) return;
      if (err instanceof ApiError && err.status === 404) {
        this.status = "notFound";
        return;
      }
      if (err instanceof ApiError && err.status === 403) {
        this.status = "forbidden";
        return;
      }
      this.errorMessage = mapError(err);
      this.status = "error";
    }
  }

  /** The label for an action button in its idle or pending form. */
  protected _buttonLabel(action: FormAction): string {
    const pending = this._pendingAction === action;
    switch (action) {
      case "publish":
        return pending ? el.admin.actionPublishPending : el.admin.actionPublish;
      case "unpublish":
        return pending
          ? el.admin.actionUnpublishPending
          : el.admin.actionUnpublish;
      case "archive":
        return pending ? el.admin.actionArchivePending : el.admin.actionArchive;
      default:
        if (pending) return el.admin.actionSavePending;
        return this._isEdit ? el.admin.actionSave : el.admin.actionSaveDraft;
    }
  }

  /**
   * The live state chip: what the row IS in edit mode, and what a plain save
   * lands as in create mode (a draft). The action buttons name the
   * transitions, so the chip stays a statement of fact — and it is refreshed
   * from every server response.
   */
  protected _stateChipTemplate() {
    const label =
      this._status === "published"
        ? el.admin.statusPublished
        : this._status === "archived"
          ? el.admin.statusArchived
          : el.admin.statusDraft;
    return html`
      <span class="state-chip" data-status=${this._status}>
        <span class="state-chip-prefix">${el.admin.stateChipPrefix}</span>
        ${label}
      </span>
    `;
  }

  /** Enter submits the SAFE action — a plain save. */
  protected _onSubmit(e: Event) {
    e.preventDefault();
    void this._onAction("save");
  }

  /** The state a given action submits. */
  protected _statusFor(action: FormAction): ContentStatus {
    switch (action) {
      case "publish":
        return "published";
      case "archive":
        return "archived";
      case "unpublish":
        return "draft";
      default:
        // Create has nothing to keep: a plain save lands as a draft.
        return this._isEdit ? this._status : "draft";
    }
  }

  protected async _onAction(action: FormAction) {
    if (this._pending) return;
    this._fieldError = "";
    this._titleError = "";
    this._error = "";

    const title = this._title.trim();
    if (!title) {
      this._titleError = violationMessage("required");
      // Focus the offending control once the error line is in the DOM —
      // the auth-form shape (aria-invalid + aria-describedby + focus).
      void this.updateComplete.then(() => {
        this.shadowRoot
          ?.querySelector<HTMLInputElement>("#content-title")
          ?.focus();
      });
      return;
    }

    // A save during an in-flight upload would write the PREVIOUS thumbnail
    // while the status line says the new file is still going up — the exact
    // silent-substitution trap the action buttons exist to remove. Refuse
    // until it lands.
    if (this._mediaWidget()?.isUploading()) {
      this._fieldError = el.admin.uploadInFlight;
      return;
    }

    // The state the action produces is what gets written — the request
    // never carries a state the editor did not choose. The chip is NOT
    // moved here: it stays a statement of fact, and only a server response
    // may change what it claims.
    const status = this._statusFor(action);
    const body = this._writeBody(status);
    if (!body) return;

    this._pending = true;
    this._pendingAction = action;
    try {
      await this._save(action, status, body);
    } catch (err) {
      if (err instanceof ApiError && err.status === 412) {
        // Someone else edited: reload the fresh row so the form holds the
        // current revision + values.
        this._success = "";
        void this._loadRow();
        this._error = this._preconditionMessage;
        return;
      }
      if (err instanceof ApiError && err.status === 403) {
        // A stale CSRF token heals through a silent session revalidate.
        void this.session?.revalidate();
      }
      if (err instanceof ValidationError) {
        const first = err.violations[0];
        this._fieldError = first
          ? violationMessage(first.code)
          : el.problems["/problems/validation"];
      } else {
        this._error = mapError(err);
      }
    } finally {
      this._pending = false;
      this._pendingAction = null;
    }
  }

  protected async _onDelete() {
    if (this._pending || !this._row) return;
    if (!window.confirm(el.admin.deleteConfirm)) return;
    this._pending = true;
    this._error = "";
    try {
      await this._deleteRow(this._rowId, this._row.revision);
      this._success = this._deletedLabel;
      navigate(this._listPath);
    } catch (err) {
      if (err instanceof ApiError && err.status === 412) {
        void this._loadRow();
        this._error = this._preconditionMessage;
      } else {
        this._error = mapError(err);
      }
    } finally {
      this._pending = false;
    }
  }
}
