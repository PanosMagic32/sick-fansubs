/**
 * Download-row editor — the add/remove/reorder list the blog and project
 * forms embed.
 *
 * Contract (the media-upload precedent): the parent calls load() with the
 * stored rows in edit mode, getValue() at submit, and reset() never (the
 * form's lifetime matches the widget's — reset() exists for symmetry and
 * tests). getValue() maps empty inputs to null (the wire contract's "no
 * link for this slot") and trims; the SERVER validates the grammar — the
 * client never hand-validates links.
 *
 * The downloads array is REQUIRED (≥ 1 row), so ONE row is the floor: the
 * widget seeds an empty row when it is loaded with none, and a LONE row
 * renders no action controls at all — nothing to reorder, and removing it
 * would empty the required array.
 * `_remove` refuses to empty the list as well, so the state is unreachable
 * rather than merely hidden; `_move` was already a no-op at one row.
 *
 * The row label field differs per content type (resolution for blog, name
 * for projects) — rowLabel carries the Greek label text; the placeholder
 * is the generic "1080p" only for the blog form (the parent passes
 * rowPlaceholder when it wants one).
 */

import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import styles from "./download-rows.css?inline";

/** One row as the parent exchanges it (null = no link for that slot). */
export interface DownloadRowValue {
  label: string;
  magnetUrl: string | null;
  torrentUrl: string | null;
}

interface RowState {
  label: string;
  magnet: string;
  torrent: string;
}

@customElement("download-rows")
export class DownloadRows extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(styles),
  ];

  /** The Greek label for the row-label input ("Ποιότητα" | "Ονομασία"). */
  @property({ type: String }) rowLabel: string = el.admin.downloadNameLabel;
  /** Optional placeholder for the row-label input. */
  @property({ type: String }) rowPlaceholder = "";

  @state() private _rows: RowState[] = [];

  private _emptyRow(): RowState {
    return { label: "", magnet: "", torrent: "" };
  }

  /** Loads the stored rows (edit mode) — nulls become empty inputs. The
   *  widget always keeps at least one row (the required-array contract):
   *  an empty stored set loads as one empty row the staffer must fill. */
  load(rows: DownloadRowValue[]) {
    const mapped = rows.map((r) => ({
      label: r.label,
      magnet: r.magnetUrl ?? "",
      torrent: r.torrentUrl ?? "",
    }));
    this._rows = mapped.length > 0 ? mapped : [this._emptyRow()];
  }

  /** Returns the rows for the write body (trimmed, empty → null). */
  getValue(): DownloadRowValue[] {
    return this._rows.map((r) => ({
      label: r.label.trim(),
      magnetUrl: r.magnet.trim() || null,
      torrentUrl: r.torrent.trim() || null,
    }));
  }

  /** Reports the one product rule the client pre-checks: every row needs
   *  at least one of its two links (the SERVER re-validates everything). */
  isValid(): boolean {
    return this._rows.every(
      (r) => r.magnet.trim() !== "" || r.torrent.trim() !== "",
    );
  }

  /** Clears back to a single empty row (the required-array default). */
  reset() {
    this._rows = [this._emptyRow()];
  }

  connectedCallback() {
    super.connectedCallback();
    // The downloads array is REQUIRED (≥ 1 row) — start with one empty row
    // so the create form can submit a first link immediately.
    if (this._rows.length === 0) this.reset();
  }

  private _add() {
    this._rows = [...this._rows, this._emptyRow()];
  }

  private _remove(i: number) {
    // One row is the required minimum (the downloads array is not optional),
    // so the last row cannot be removed. The control that would do it is not
    // rendered at all (see render), and this guard makes the state
    // unreachable from any caller — an empty array would fail `isValid()`
    // silently and reach the server as a 422.
    if (this._rows.length <= 1) return;
    this._rows = this._rows.filter((_, idx) => idx !== i);
  }

  private _move(i: number, dir: -1 | 1) {
    const target = i + dir;
    if (target < 0 || target >= this._rows.length) return;
    const rows = [...this._rows];
    const moved = rows.splice(i, 1)[0];
    if (!moved) return;
    rows.splice(target, 0, moved);
    this._rows = rows;
  }

  private _setLabel(i: number, value: string) {
    this._rows = this._rows.map((r, idx) =>
      idx === i ? { ...r, label: value } : r,
    );
  }

  private _setLink(i: number, field: "magnet" | "torrent", value: string) {
    this._rows = this._rows.map((r, idx) =>
      idx === i ? { ...r, [field]: value } : r,
    );
  }

  render() {
    // A lone row has nothing to reorder and cannot be removed (the array is
    // required), so it renders no action controls and its fields take the
    // tracks those controls would have used (`data-solo`, see the sheet).
    const solo = this._rows.length === 1;
    return html`
      <div class="download-rows">
        ${this._rows.map(
          (row, i) => html`
            <div class="download-row" data-solo=${solo ? "true" : nothing}>
              <input
                type="text"
                class="download-label"
                .value=${row.label}
                placeholder=${this.rowPlaceholder}
                aria-label=${this.rowLabel}
                @input=${(e: Event) =>
                  this._setLabel(i, (e.target as HTMLInputElement).value)}
              />
              <input
                type="text"
                class="download-link download-magnet"
                .value=${row.magnet}
                placeholder="magnet:?xt=…"
                lang="en"
                aria-label=${el.admin.downloadMagnetLabel}
                @input=${(e: Event) =>
                  this._setLink(
                    i,
                    "magnet",
                    (e.target as HTMLInputElement).value,
                  )}
              />
              <input
                type="text"
                class="download-link download-torrent"
                .value=${row.torrent}
                placeholder="https://…"
                lang="en"
                aria-label=${el.admin.downloadTorrentLabel}
                @input=${(e: Event) =>
                  this._setLink(
                    i,
                    "torrent",
                    (e.target as HTMLInputElement).value,
                  )}
              />
              ${
                solo
                  ? nothing
                  : html`
                      <button
                        type="button"
                        class="button button--icon button--secondary row-button row-up"
                        ?disabled=${i === 0}
                        aria-label=${el.admin.downloadMoveUp}
                        @click=${() => this._move(i, -1)}
                      >
                        ↑
                      </button>
                      <button
                        type="button"
                        class="button button--icon button--secondary row-button row-down"
                        ?disabled=${i === this._rows.length - 1}
                        aria-label=${el.admin.downloadMoveDown}
                        @click=${() => this._move(i, 1)}
                      >
                        ↓
                      </button>
                      <button
                        type="button"
                        class="button button--danger button--sm row-button row-remove"
                        @click=${() => this._remove(i)}
                      >
                        ${el.admin.downloadRemoveRow}
                      </button>
                    `
              }
            </div>
          `,
        )}
        <button
          type="button"
          class="button button--secondary button--sm row-add"
          @click=${this._add}
        >
          ${el.admin.downloadAddRow}
        </button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "download-rows": DownloadRows;
  }
}
