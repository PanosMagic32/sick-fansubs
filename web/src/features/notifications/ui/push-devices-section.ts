/**
 * Push devices section of <push-settings> — the all-devices section: every
 * subscription row the caller owns, with its device name, the
 * current-device badge, and the rename/delete actions.
 *
 * A reactive section controller: the element owns the shadow-root markup and
 * reads this section's state; the section owns the rows, the action
 * handlers, and the rename dialog's lifecycle. The subscriptions section
 * loads the rows at mount and hands them over (applyLoaded); this section
 * patches its rows locally after its own mutations; the subscriptions
 * section asks it to reload after a subscribe or disable, and its self-test
 * re-read arrives through applyLoaded too.
 *
 * Delete follows the sessions precedent — no confirm, a pending swap on the
 * row's own control — and the CURRENT device's row carries no delete: the
 * off switch owns that row, because deleting it here would strand the
 * browser's live local subscription. A delete of another row needs no local
 * cleanup: that device simply stops receiving until its user turns push on
 * there again.
 */

import {
  html,
  nothing,
  type ReactiveController,
  type ReactiveControllerHost,
} from "lit";

import { requestSignal } from "@shared/api/request-signal.js";
import { el } from "@shared/catalog/el.js";
import { formatDate, mapError } from "@shared/utils/format.js";
import { icons } from "@shared/ui/icons.js";

import {
  deletePushSubscription,
  listPushSubscriptions,
  renamePushSubscription,
} from "../data-access/push-api.js";
import type { PushSubscriptionItem } from "../data-access/types.js";

/** Mirrors the server's `maxPushLabelRunes` (internal/handler/
 * push_subscriptions.go): the rename field clamps against RUNES — a native
 * maxlength attribute would count UTF-16 units and refuse astral names the
 * server accepts. */
const MAX_DEVICE_NAME_RUNES = 32;

/** The narrow subscriptions-section surface the devices section reads. */
export interface PushDevicesSubscriptionsSurface {
  /** The current device's row id (the endpoint match), or null when this
   * device is not subscribed. */
  get currentSubscriptionId(): string | null;
  /** The shared error banner (all three sections write it). */
  get error(): string;
  set error(value: string);
  /** The shared busy predicate — any action in flight in the section. */
  get busy(): boolean;
}

/** The element surface the devices section needs. */
export interface PushDevicesHost extends ReactiveControllerHost {
  /** The element's shadow root — the rename dialog is queried there. */
  readonly shadowRoot: ShadowRoot | null;
  /** The subscriptions section: the current-device id and the shared banner. */
  readonly subscriptions: PushDevicesSubscriptionsSurface;
}

/** The narrow devices-section surface the subscriptions section drives. */
export interface PushDevicesSurface {
  /** Hand fresh rows over: the mount load, the subscriptions section's
   * subscribe/disable re-read, or its self-test re-read. */
  applyLoaded(rows: PushSubscriptionItem[]): void;
  /** Best-effort re-read after a subscribe or disable changed the rows;
   * failures are silent (the mount load owns the error banner). */
  reload(): void;
  /** A device action in flight — joins the shared busy predicate. */
  get pending(): boolean;
}

export class PushDevicesSection implements ReactiveController {
  private readonly _host: PushDevicesHost;

  /** The caller's rows — handed over by the mount load, updated locally by
   * rename/delete, re-read after a subscribe/disable/self-test. */
  private _rows: PushSubscriptionItem[] = [];

  /** The row whose delete is in flight (its control swaps copy + disables). */
  private _deletingId = "";

  /** The open rename editor: the row id and the draft name; null while the
   * dialog is closed. */
  private _renaming: { id: string; value: string } | null = null;
  private _renamePending = false;
  private _renameError = "";

  /** The list-read controller — superseding reads never abort a write. */
  private _readAbort: AbortController | null = null;

  /** The delete/rename write controller (reads use their own). */
  private _writeAbort: AbortController | null = null;

  constructor(host: PushDevicesHost) {
    this._host = host;
    host.addController(this);
  }

  hostDisconnected() {
    this._readAbort?.abort();
    this._writeAbort?.abort();
  }

  /** A device action in flight — the shared busy predicate reads this. */
  get pending(): boolean {
    return this._deletingId !== "" || this._renamePending;
  }

  /** One busy predicate for every control in the settings section: a press
   * in flight must not race another action, whichever section owns it. */
  get busy(): boolean {
    return this._host.subscriptions.busy;
  }

  applyLoaded(rows: PushSubscriptionItem[]) {
    // Fresh rows supersede a re-read still in flight — a late response must
    // not overwrite them (and resurrect a row the server already dropped).
    this._readAbort?.abort();
    this._readAbort = null;
    this._rows = rows;
    this._closeRenameIfGone();
    this._refresh();
  }

  reload(): void {
    const controller = new AbortController();
    this._readAbort?.abort();
    this._readAbort = controller;
    void listPushSubscriptions({ signal: requestSignal(controller) })
      .then(({ subscriptions }) => {
        if (controller.signal.aborted) return;
        this._rows = subscriptions;
        this._closeRenameIfGone();
        this._refresh();
      })
      .catch(() => {
        /* best-effort freshness — the mount load owns the error banner */
      });
  }

  /* ── Delete (other devices) ─────────────────────────────────────────── */

  _onDeleteClick = async (id: string) => {
    if (this.busy || id === this._host.subscriptions.currentSubscriptionId) {
      return;
    }
    this._deletingId = id;
    this._host.subscriptions.error = "";
    this._refresh();
    const controller = new AbortController();
    this._writeAbort?.abort();
    this._writeAbort = controller;
    try {
      await deletePushSubscription(id, { signal: requestSignal(controller) });
      if (controller.signal.aborted) return;
      this._rows = this._rows.filter((row) => row.id !== id);
      this._closeRenameIfGone();
    } catch (err) {
      if (controller.signal.aborted) return;
      this._host.subscriptions.error = mapError(err);
    } finally {
      this._deletingId = "";
      this._refresh();
    }
  };

  /* ── Rename ─────────────────────────────────────────────────────────── */

  /** The rename dialog — a class-scoped query so the subscribe confirm
   * dialog in the same shadow root can never be picked. */
  private _renameDialog(): HTMLDialogElement | null {
    return (
      (this._host.shadowRoot?.querySelector(
        "dialog.push-device-dialog",
      ) as HTMLDialogElement | null) ?? null
    );
  }

  /** Drop the rename editor when its row vanished (a delete or a fresh list
   * can unmount the dialog mid-edit and strand its focus/inert state). */
  private _closeRenameIfGone() {
    if (
      this._renaming !== null &&
      !this._rows.some((row) => row.id === this._renaming?.id)
    ) {
      this._renameDialog()?.close();
      this._renaming = null;
      this._renameError = "";
    }
  }

  _onRenameClick = (row: PushSubscriptionItem) => {
    if (this.busy) return;
    this._renaming = { id: row.id, value: row.label ?? "" };
    this._renameError = "";
    this._refresh();
    const dialog = this._renameDialog();
    if (!dialog || dialog.open) return;
    // showModal gives the focus trap + restore + Escape handling; the
    // attribute fallback covers a DOM without the method.
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
  };

  _onRenameInput = (value: string) => {
    if (!this._renaming) return;
    // The server counts RUNES on the TRIMMED label, so the budget is spent on
    // the trimmed draft only — clamping the raw value would drop name runes
    // that sit behind whitespace the server trims away.
    const runes = [...value.trim()];
    if (runes.length > MAX_DEVICE_NAME_RUNES) {
      // Clamp, then repaint so the field shows the clamped name — the state
      // is the submitted value and never the over-long draft.
      this._renaming.value = runes.slice(0, MAX_DEVICE_NAME_RUNES).join("");
      this._refresh();
      return;
    }
    this._renaming.value = value;
  };

  _onRenameCancel = () => {
    this._renameDialog()?.close();
    this._renaming = null;
    this._renameError = "";
    this._refresh();
  };

  _onRenameSubmit = async (event: Event) => {
    // The native submit would navigate; the dialog form is JS-handled (the
    // settings patterns).
    event.preventDefault();
    if (this.busy || !this._renaming) return;
    const label = this._renaming.value.trim();
    if (label === "") {
      this._renameError = el.notifications.pushDeviceRenameRequired;
      this._refresh();
      return;
    }
    const id = this._renaming.id;
    this._renamePending = true;
    this._renameError = "";
    this._refresh();
    const controller = new AbortController();
    this._writeAbort?.abort();
    this._writeAbort = controller;
    try {
      await renamePushSubscription(id, label, {
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return;
      this._rows = this._rows.map((row) =>
        row.id === id ? { ...row, label } : row,
      );
      this._renameDialog()?.close();
      this._renaming = null;
    } catch (err) {
      if (controller.signal.aborted) return;
      this._renameError = mapError(err);
    } finally {
      this._renamePending = false;
      this._refresh();
    }
  };

  /* ── Rendering ──────────────────────────────────────────────────────── */

  template() {
    // No rows: nothing to manage — the subscribe flow above is the whole
    // story.
    if (this._rows.length === 0) return nothing;
    return html`
      <h3 class="push-devices-title">${el.notifications.pushDevicesTitle}</h3>
      <ul class="push-devices" role="list">
        ${this._rows.map((row) => this._rowTemplate(row))}
      </ul>
      <dialog
        class="push-dialog push-device-dialog"
        aria-labelledby="push-device-dialog-title"
        @cancel=${this._onRenameCancel}
      >
        <form @submit=${this._onRenameSubmit} novalidate>
          <h3 id="push-device-dialog-title">
            ${el.notifications.pushDeviceRenameTitle}
          </h3>
          <div class="push-device-field">
            <label for="push-device-name"
              >${el.notifications.pushDeviceRenameLabel}</label
            >
            <input
              id="push-device-name"
              type="text"
              .value=${this._renaming?.value ?? ""}
              placeholder=${el.notifications.pushDeviceRenamePlaceholder}
              .disabled=${this._renamePending}
              @input=${(e: Event) =>
                this._onRenameInput((e.target as HTMLInputElement).value)}
            />
          </div>
          ${
            this._renameError
              ? html`<p class="push-error" role="alert">
                  ${this._renameError}
                </p>`
              : ""
          }
          <div class="push-dialog-actions">
            <button
              type="submit"
              class="button button--primary"
              .disabled=${this._renamePending || this.busy}
            >
              ${
                this._renamePending
                  ? el.notifications.pushDeviceRenameSaving
                  : el.notifications.pushDeviceRenameSave
              }
            </button>
            <button
              type="button"
              class="button button--secondary"
              .disabled=${this._renamePending}
              @click=${this._onRenameCancel}
            >
              ${el.notifications.pushDeviceRenameCancel}
            </button>
          </div>
        </form>
      </dialog>
    `;
  }

  /** One device row: name, current-device badge, registration instant, and
   * the rename/delete icon actions. The current row carries no delete (the
   * off switch above owns it); the delete swaps its label and disables while
   * the request is out. */
  private _rowTemplate(row: PushSubscriptionItem) {
    const current = row.id === this._host.subscriptions.currentSubscriptionId;
    const deleting = this._deletingId === row.id;
    // The delete's bindings (label, title, disabled) read ONE local, so they
    // can never disagree about the pending state (the comments action row).
    const deleteLabel = deleting
      ? el.notifications.pushDeviceDeletePending
      : el.notifications.pushDeviceDelete;
    return html`
      <li class="push-device-row" data-device-id=${row.id}>
        <div class="push-device-info">
          <span class="push-device-name"
            >${row.label ?? el.notifications.pushDeviceUnknown}</span
          >
          ${
            current
              ? html`<span class="push-device-badge"
                  >${el.notifications.pushDeviceCurrent}</span
                >`
              : ""
          }
        </div>
        <div class="push-device-meta">
          <span class="push-device-added"
            >${el.notifications.pushDeviceAddedAt}:
            <time datetime=${row.createdAt}
              >${formatDate(row.createdAt)}</time
            ></span
          >
          <span class="push-device-actions">
            <button
              type="button"
              class="button button--icon button--ghost push-device-rename"
              aria-label=${el.notifications.pushDeviceRename}
              title=${el.notifications.pushDeviceRename}
              .disabled=${this.busy}
              @click=${() => this._onRenameClick(row)}
            >
              ${icons.edit}
            </button>
            ${
              current
                ? nothing
                : html`<button
                    type="button"
                    class="button button--icon button--ghost push-device-delete"
                    aria-label=${deleteLabel}
                    title=${deleteLabel}
                    .disabled=${this.busy}
                    @click=${() => this._onDeleteClick(row.id)}
                  >
                    ${icons.trash}
                  </button>`
            }
          </span>
        </div>
      </li>
    `;
  }

  /** Ask the host for a re-render — the controller's own state does not
   * schedule one. */
  private _refresh() {
    this._host.requestUpdate();
  }
}
