/**
 * Member section of the admin users page: the member card, its staged-edit
 * panel, and the row actions (staged save, password reset, delete) as a Lit
 * reactive controller.
 *
 * The section renders into the PAGE's shadow root — the page's stylesheet and
 * the member selectors require one root — so `template(item)` returns the card
 * the page places per row, and the reset/action state below is the page's to
 * render above the list. The page keeps the list, the filters, and the paging.
 */

import {
  type ReactiveController,
  type ReactiveControllerHost,
  html,
  nothing,
} from "lit";

import type { SessionState } from "@features/auth/data-access/session.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { avatarTemplate } from "@shared/ui/avatar.js";
import { icons } from "@shared/ui/icons.js";
import { formatDate, mapError } from "@shared/utils/format.js";
import { roleLabel } from "@shared/utils/roles.js";

import {
  changeUserRole,
  deleteUser,
  reactivateUser,
  resetUserPassword,
  suspendUser,
  type StaffUser,
  type UserRole,
  type UserStatus,
} from "../data-access/users-admin-api.js";
import {
  canChangeStatusRow,
  canManageRow,
  roleOptions,
} from "../utils/member-rules.js";

/** Accepted status filter values, in display order. */
export const STATUSES: UserStatus[] = ["active", "suspended"];

export { ROLES } from "../utils/member-rules.js";

/** A member's staged edits — the fields the panel may change, set only while
 * they differ from the loaded row (the push-settings draft model). */
interface MemberDraft {
  role?: UserRole;
  status?: UserStatus;
}

/** What the save would send: only the fields that differ from the loaded row,
 * or null when the panel holds no pending change. */
function memberChanges(
  item: StaffUser,
  draft: MemberDraft | undefined,
): MemberDraft | null {
  if (!draft) return null;
  const changes: MemberDraft = {};
  if (draft.role && draft.role !== item.role) changes.role = draft.role;
  if (draft.status && draft.status !== item.status) {
    changes.status = draft.status;
  }
  return changes.role || changes.status ? changes : null;
}

/** The host surface the member section needs from the admin users page. */
export interface MemberSectionHost extends ReactiveControllerHost {
  /** The page's session state — the capability mirrors read the actor role. */
  readonly sessionState: SessionState | undefined;
  /** The page's shadow root — the reset dialog's steps query it. */
  readonly shadowRoot: ShadowRoot | null;
  /** Replace one loaded row with the server's projection (matched by id). */
  replaceMember(updated: StaffUser): void;
  /** Remove one loaded row; the page owns the empty-page step. */
  removeMember(id: string): void;
}

export class MemberSection implements ReactiveController {
  private readonly _host: MemberSectionHost;

  /** The member whose panel is open (empty = none). */
  private _openMemberId = "";

  /** Staged edits per member id, keyed by the loaded row's id — one
   * member's panel is open at a time, and nothing here mutates the row: the
   * chips and the save summary render the STAGED values, so what the panel
   * would send is always visible before the request. */
  private _drafts = new Map<string, MemberDraft>();

  /** Bumped on every list load; a late success or error effect from an older
   * generation is discarded, so a stale response can never paint a dialog,
   * banner, or success/error state over a fresh list. */
  private _generation = 0;

  /** Each row action's in-flight id (null when idle) and the AbortController
   * its request was issued with — teardown aborts every one of them. */
  private _savingId: string | null = null;
  private _resettingId: string | null = null;
  private _deletingId: string | null = null;
  private _resetAbort: AbortController | null = null;
  private _deleteAbort: AbortController | null = null;
  private _saveAbort: AbortController | null = null;

  /** The one-time temp password the page's reset dialog shows (null =
   * closed); `copied` is true after its copy button wrote it out. */
  resetResult: { username: string; password: string } | null = null;
  copied = false;

  /** A failed reset / a failed save or deletion — the page renders them
   * above the list (empty = none). */
  resetError = "";
  opError = "";

  constructor(host: MemberSectionHost) {
    this._host = host;
    host.addController(this);
  }

  hostDisconnected() {
    this._resetAbort?.abort();
    this._deleteAbort?.abort();
    this._saveAbort?.abort();
  }

  /** Drop the panel state when the page loads a new list: a page change
   * closes the one-time password dialog — the plaintext must not outlive the
   * context that produced it — and drops every staged edit with the open
   * panel: these rows are about to be replaced, so a draft keyed by an id
   * from the previous page must never survive onto a fresh list. The
   * generation bump discards every in-flight action's row effect on arrival. */
  resetForLoad() {
    this._generation++;
    this.resetResult = null;
    this.resetError = "";
    this.opError = "";
    this._savingId = null;
    this._resettingId = null;
    this._deletingId = null;
    this._openMemberId = "";
    this._drafts = new Map();
    this._refresh();
  }

  /** The status label the page's filter options and the panel/chips share. */
  statusLabel(status: UserStatus): string {
    return status === "suspended"
      ? el.admin.statusSuspended
      : el.admin.statusActive;
  }

  /**
   * The one lifecycle every row mutation shares: serialize per-kind actions,
   * run the caller's confirm gate, hold the action's AbortController, map
   * failures into the action's error slot, and clear the busy id on the way
   * out. The busy-id guard runs BEFORE the gate, so a confirm dialog never
   * pops while an action of the same kind is in flight.
   */
  private async _runRowAction<T>(action: {
    itemId: string;
    /** Read the in-flight id field (null when idle). */
    busyId: () => string | null;
    /** Write the in-flight id field. */
    setBusyId: (id: string | null) => void;
    /** Hold the action's AbortController (teardown aborts it). */
    holdAbort: (controller: AbortController) => void;
    /** Reset the action's error slot before the call. */
    clearError: () => void;
    /** Write the mapped failure into the action's error slot. */
    setError: (message: string) => void;
    /** Optional pre-flight gate — the confirm dialogs. False = no call. */
    gate?: () => boolean;
    /** The API call, given the composed (page + 15s timeout) signal. */
    run: (signal: AbortSignal) => Promise<T>;
    /** The row effect on success, after the abort check. */
    onSuccess?: (result: T) => void;
  }): Promise<void> {
    if (action.busyId()) return;
    if (action.gate && !action.gate()) return;
    action.setBusyId(action.itemId);
    action.clearError();
    const controller = new AbortController();
    action.holdAbort(controller);
    const generation = this._generation;
    try {
      const result = await action.run(requestSignal(controller));
      if (controller.signal.aborted || generation !== this._generation) return;
      action.onSuccess?.(result);
    } catch (err) {
      if (controller.signal.aborted || generation !== this._generation) return;
      action.setError(mapError(err));
    } finally {
      if (
        generation === this._generation &&
        action.busyId() === action.itemId
      ) {
        action.setBusyId(null);
      }
    }
  }

  /** The peer-matrix mirror that opens the panel and its actions. */
  private _canManageRow(item: StaffUser): boolean {
    return canManageRow(this._host.sessionState, item);
  }

  private async _onResetClick(item: StaffUser) {
    await this._runRowAction({
      itemId: item.id,
      busyId: () => this._resettingId,
      setBusyId: (id) => {
        this._resettingId = id;
        this._refresh();
      },
      holdAbort: (controller) => {
        this._resetAbort = controller;
      },
      clearError: () => {
        this.resetError = "";
        this.resetResult = null;
        this._refresh();
      },
      setError: (message) => {
        this.resetError = message;
        this._refresh();
      },
      gate: () => window.confirm(el.admin.resetConfirm),
      run: (signal) => resetUserPassword(item.id, { signal }),
      onSuccess: (result) => {
        this.resetResult = {
          username: item.username,
          password: result.password,
        };
        this.copied = false;
        this._refresh();
      },
    });
  }

  /** The reset dialog's copy control (the page binds it directly). */
  readonly onCopyReset = async () => {
    if (!this.resetResult) return;
    try {
      await navigator.clipboard.writeText(this.resetResult.password);
      this.copied = true;
      this._refresh();
    } catch {
      // Clipboard unavailable (permissions, insecure context): the code
      // element is user-selectable as the fallback.
    }
  };

  /** Close the one-time password dialog: close the ELEMENT first so the
   * platform runs its close steps (focus returns to the reset control),
   * then clear the state — the page's render drops the element, the
   * plaintext with it. Escape routes here through `cancel`, the button
   * through its click (the page binds it directly). */
  readonly onCloseReset = () => {
    const dialog = this._host.shadowRoot?.querySelector<HTMLDialogElement>(
      "dialog.reset-dialog",
    );
    if (dialog?.open) dialog.close();
    this.resetResult = null;
    this.copied = false;
    this._refresh();
  };

  private _roleOptions(): UserRole[] {
    return roleOptions(this._host.sessionState);
  }

  private _onStageRole(item: StaffUser, e: Event) {
    this._stageDraft(item.id, {
      role: (e.target as HTMLSelectElement).value as UserRole,
    });
  }

  private _onStageStatus(item: StaffUser, e: Event) {
    this._stageDraft(item.id, {
      status: (e.target as HTMLSelectElement).value as UserStatus,
    });
  }

  /** Stage one field's edit — a copy, so Lit sees a new Map (the draft is
   * section state: mutating in place would not re-render). */
  private _stageDraft(id: string, patch: MemberDraft) {
    const next = new Map(this._drafts);
    next.set(id, { ...(next.get(id) ?? {}), ...patch });
    this._drafts = next;
    this._refresh();
  }

  /** Drop every staged edit for one member (the panel's «Άκυρο»). */
  private _onCancelDraft(item: StaffUser) {
    const next = new Map(this._drafts);
    next.delete(item.id);
    this._drafts = next;
    this._refresh();
  }

  private _onToggleMember(item: StaffUser) {
    const previous = this._openMemberId;
    this._openMemberId = previous === item.id ? "" : item.id;
    // A draft lives and dies with its panel: the panel is the only place a
    // staged edit is visible, so a closed card must not keep reading a value
    // the loaded row does not have. Switching members closes the previous
    // panel, so the outgoing draft goes with it.
    if (previous && previous !== this._openMemberId) {
      const next = new Map(this._drafts);
      next.delete(previous);
      this._drafts = next;
    }
    this._refresh();
  }

  /**
   * Apply what the server confirmed and clear exactly the staged fields that
   * landed. A two-leg save (role, then status) can land its first leg before
   * the second fails, so the panel must not pretend nothing happened: the row
   * takes the server's projection, the applied field drops out of the draft,
   * and the failed one stays staged for a retry (the push-settings draft
   * rule). The panel stays open either way — a failure is reported above the
   * list, and its draft is still there to retry.
   */
  private _settleSave(
    item: StaffUser,
    updated: StaffUser,
    applied: MemberDraft,
  ) {
    if (!applied.role && !applied.status) return;
    // The server is authoritative (email included only when the viewer's role
    // allows it).
    this._host.replaceMember(updated);
    const next = new Map(this._drafts);
    const rest: MemberDraft = { ...(next.get(item.id) ?? {}) };
    if (applied.role) delete rest.role;
    if (applied.status) delete rest.status;
    if (rest.role || rest.status) next.set(item.id, rest);
    else next.delete(item.id);
    this._drafts = next;
    this._refresh();
  }

  /**
   * The panel's ONE save: only the fields that differ from the loaded row are
   * sent, each to its own endpoint (the two changes are two wire operations —
   * `role` is a PATCH, the status a suspend/reactivate POST).
   *
   * Suspending logs the target out and is confirmed; reactivating stays
   * immediate. Because the status is a staged select, the confirmation hangs
   * off the SAVE — it fires only when the save would transition
   * active → suspended, and only once the busy guard lets the save run.
   */
  private async _onSaveMember(item: StaffUser) {
    const changes = memberChanges(item, this._drafts.get(item.id));
    if (!changes) return;
    // Both legs land in `applied`/`row` as they complete, so a partial failure
    // still knows what the server accepted.
    const applied: MemberDraft = {};
    const row = { value: item };
    await this._runRowAction<StaffUser>({
      itemId: item.id,
      busyId: () => this._savingId,
      setBusyId: (id) => {
        this._savingId = id;
        this._refresh();
      },
      holdAbort: (controller) => {
        this._saveAbort = controller;
      },
      clearError: () => {
        this.opError = "";
        this._refresh();
      },
      setError: (message) => {
        this.opError = message;
        this._refresh();
      },
      // The confirmation only fires once the save would actually run: the
      // busy-id guard runs before the gate, so a second save never pops a
      // dialog it then discards.
      gate: () =>
        changes.status !== "suspended" ||
        window.confirm(el.admin.suspendConfirm),
      run: async (signal) => {
        try {
          if (changes.role) {
            row.value = await changeUserRole(item.id, changes.role, {
              signal,
            });
            applied.role = changes.role;
          }
          if (changes.status) {
            row.value =
              changes.status === "active"
                ? await reactivateUser(item.id, { signal })
                : await suspendUser(item.id, { signal });
            applied.status = changes.status;
          }
          return row.value;
        } catch (err) {
          // Keep whatever leg the server confirmed before this failure.
          this._settleSave(item, row.value, applied);
          throw err;
        }
      },
      onSuccess: (updated) => this._settleSave(item, updated, applied),
    });
  }

  private async _onDeleteClick(item: StaffUser) {
    await this._runRowAction({
      itemId: item.id,
      busyId: () => this._deletingId,
      setBusyId: (id) => {
        this._deletingId = id;
        this._refresh();
      },
      holdAbort: (controller) => {
        this._deleteAbort = controller;
      },
      clearError: () => {
        this.opError = "";
        this._refresh();
      },
      setError: (message) => {
        this.opError = message;
        this._refresh();
      },
      gate: () => window.confirm(el.admin.deleteUserConfirm),
      run: (signal) => deleteUser(item.id, { signal }),
      onSuccess: () => {
        // The deleted member's panel and draft go with the row: an empty
        // panel over a vanished member would keep a staged edit no save can
        // reach.
        const next = new Map(this._drafts);
        next.delete(item.id);
        this._drafts = next;
        if (this._openMemberId === item.id) this._openMemberId = "";
        this._host.removeMember(item.id);
        this._refresh();
      },
    });
  }

  /** The status-change mirror (the one capability below the admin floor).
   * The three peers — `_canManageRow`, this, and `_roleOptions` — drive ONE
   * shell: a row that grants neither capability renders a static head with
   * no panel, so a moderator never gets a chevron that opens an empty panel. */
  private _canChangeStatusRow(item: StaffUser): boolean {
    return canChangeStatusRow(this._host.sessionState, item);
  }

  /**
   * The always-visible head: avatar, identity, chips, chevron. The chips read
   * the STAGED value when the panel holds an edit, so the collapsed card
   * already answers what a save would send. A row the viewer may not change at
   * all renders the same head WITHOUT the button (no chevron, no panel) — a
   * moderator must never get a control that opens an empty panel.
   *
   * The avatar is the account's real image when the staff DTO carries one
   * (`avatarUrl`) and the shared initials circle when the account has none —
   * `avatarTemplate` owns that fallback chain.
   */
  template(item: StaffUser) {
    const expandable =
      this._canManageRow(item) || this._canChangeStatusRow(item);
    const open = expandable && this._openMemberId === item.id;
    const head = html`
      <span class="member-avatar"
        >${avatarTemplate({
          username: item.username,
          avatarUrl: item.avatarUrl,
        })}</span
      >
      <span class="member-who">
        <span class="member-name">${item.username}</span>
        ${
          item.email
            ? html`<span class="member-secondary">${item.email}</span>`
            : nothing
        }
        <span class="member-meta"
          >${
            item.emailVerified
              ? el.admin.emailVerified
              : el.admin.emailUnverified
          }
          · ${formatDate(item.createdAt)}</span
        >
        ${this._memberChipsTemplate(item)}
      </span>
      ${
        expandable
          ? html`<span class="member-chevron">${icons.chevronDown}</span>`
          : nothing
      }
    `;
    return html`<li class="member-card" data-open=${open ? "true" : "false"}>
      ${
        expandable
          ? html`<button
              type="button"
              class="member-head"
              aria-expanded=${open ? "true" : "false"}
              @click=${() => this._onToggleMember(item)}
            >
              ${head}
            </button>`
          : html`<div class="member-head member-head--static">${head}</div>`
      }
      ${open ? this._memberPanelTemplate(item) : nothing}
    </li>`;
  }

  /** Role + status chips — staged values wear the dashed border. */
  private _memberChipsTemplate(item: StaffUser) {
    const draft = this._drafts.get(item.id);
    const role = draft?.role ?? item.role;
    const status = draft?.status ?? item.status;
    return html`<span class="member-chips">
      <span
        class="member-chip member-chip--role${
          role !== item.role ? " member-chip--staged" : ""
        }"
        >${roleLabel(role)}</span
      >
      <span
        class="member-chip ${
          status === "active" ? "member-chip--active" : "member-chip--suspended"
        }${status !== item.status ? " member-chip--staged" : ""}"
        >${this.statusLabel(status)}</span
      >
    </span>`;
  }

  /**
   * One member's panel: the staged selects this viewer may set, ONE save (it
   * appears only while something differs), and the immediate icon actions.
   */
  private _memberPanelTemplate(item: StaffUser) {
    const editableRole = this._canManageRow(item);
    const editableStatus = this._canChangeStatusRow(item);
    const draft = this._drafts.get(item.id);
    const changes = memberChanges(item, draft);
    const saving = this._savingId === item.id;
    return html`<div class="member-panel">
      <div class="member-controls">
        ${
          editableRole
            ? html`<label class="member-field">
                <span>${el.admin.roleSelectLabel}</span>
                <select
                  data-value=${draft?.role ?? item.role}
                  ?disabled=${saving}
                  @change=${(e: Event) => this._onStageRole(item, e)}
                >
                  ${this._roleOptions().map(
                    (r) => html`<option value=${r}>${roleLabel(r)}</option>`,
                  )}
                </select>
              </label>`
            : nothing
        }
        ${
          editableStatus
            ? html`<label class="member-field">
                <span>${el.admin.filterStatusLabel}</span>
                <select
                  data-value=${draft?.status ?? item.status}
                  ?disabled=${saving}
                  @change=${(e: Event) => this._onStageStatus(item, e)}
                >
                  ${STATUSES.map(
                    (s) =>
                      html`<option value=${s}>${this.statusLabel(s)}</option>`,
                  )}
                </select>
              </label>`
            : nothing
        }
      </div>
      ${
        changes
          ? html`<div class="member-save-row">
              <button
                type="button"
                class="button button--primary button--sm member-save"
                ?disabled=${saving}
                @click=${() => this._onSaveMember(item)}
              >
                ${icons.save}
                <span
                  >${
                    saving ? el.admin.memberSavePending : el.admin.memberSave
                  }</span
                >
              </button>
              <button
                type="button"
                class="button button--secondary button--sm member-cancel"
                ?disabled=${saving}
                @click=${() => this._onCancelDraft(item)}
              >
                ${el.admin.memberCancel}
              </button>
              <span class="member-dirty">${this._dirtySummary(changes)}</span>
            </div>`
          : html`<p class="member-dirty">${el.admin.memberNoChange}</p>`
      }
      ${
        changes
          ? html`<p class="member-leave-hint">${el.admin.memberLeaveHint}</p>`
          : nothing
      }
      ${this._memberActionsTemplate(item)}
    </div>`;
  }

  /** What the save would send, field by field (the staged-value summary). */
  private _dirtySummary(changes: MemberDraft): string {
    const parts: string[] = [];
    if (changes.role) {
      parts.push(`${el.admin.roleSelectLabel}: ${roleLabel(changes.role)}`);
    }
    if (changes.status) {
      parts.push(
        `${el.admin.filterStatusLabel}: ${this.statusLabel(changes.status)}`,
      );
    }
    return parts.join(" · ");
  }

  /**
   * The two IMMEDIATE actions: icon-only buttons whose meaning rides
   * `aria-label` + `title`, because the panel is narrow on a phone and a word
   * per action does not fit. Both are admin+ capabilities, so one guard
   * covers the pair.
   */
  private _memberActionsTemplate(item: StaffUser) {
    if (!this._canManageRow(item)) return nothing;
    const resetting = this._resettingId === item.id;
    const deleting = this._deletingId === item.id;
    const resetLabel = resetting ? el.admin.resetPending : el.admin.resetButton;
    const deleteLabel = deleting
      ? el.admin.deletePending
      : el.admin.deleteButton;
    return html`<div
      class="member-actions"
      role="group"
      aria-label=${el.admin.memberActionsLabel}
    >
      <button
        type="button"
        class="button button--icon button--secondary member-action"
        aria-label=${resetLabel}
        title=${resetLabel}
        ?disabled=${resetting}
        @click=${() => this._onResetClick(item)}
      >
        ${icons.key}
      </button>
      <button
        type="button"
        class="button button--icon button--danger member-action member-action--danger"
        aria-label=${deleteLabel}
        title=${deleteLabel}
        ?disabled=${deleting}
        @click=${() => this._onDeleteClick(item)}
      >
        ${icons.trash}
      </button>
    </div>`;
  }

  /** Request a host re-render after a state change. */
  private _refresh() {
    this._host.requestUpdate();
  }
}
