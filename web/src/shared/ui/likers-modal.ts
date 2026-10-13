/**
 * Likers modal — the shared who-liked dialog behind the content indicators
 * (a post's or project's favoriters, a comment's hearters).
 *
 * The element owns the cursor state machine and NOTHING about the wire: the
 * host hands it a `loader` (its feature's endpoint call), sets the heading
 * copy, flips `open`, and receives `sf-likers-close`. That keeps shared/
 * free of feature APIs (components.md rule 2) while the paging logic exists
 * once.
 *
 * Loading: page 1 on every open (likes are live data), "Περισσότεροι"
 * appends through the previous page's endCursor. Reads abort on close,
 * teardown, and supersede, so a slow page can never paint over a newer
 * state.
 *
 * The dialog is the native <dialog> (components.md rule 10): opening is a
 * post-render step guarded by `dialog.open`, and the element falls back to
 * the open attribute when the DOM lacks showModal (happy-dom).
 */

import { html, LitElement, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { el } from "@shared/catalog/el.js";
import { LIKERS_CLOSE_EVENT } from "@shared/events.js";
import { mapError } from "@shared/utils/format.js";

import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import { avatarTemplate } from "./avatar.js";
import { icons } from "./icons.js";
import avatarStyles from "./avatar.css?inline";
import styles from "./likers-modal.css?inline";

/** One user who liked content (the LikerItem wire projection). */
export interface LikerItem {
  id: string;
  username: string;
  avatarUrl: string | null;
}

/** One keyset page of likers; a feature response is structurally assignable
 * (extra keys like total pass through). */
export interface LikerPage {
  items: LikerItem[];
  pageInfo: {
    hasNextPage: boolean;
    endCursor: string | null;
  };
}

/** The host-injected endpoint call; after = null means the first page. */
export type LikerPageLoader = (
  after: string | null,
  signal: AbortSignal,
) => Promise<LikerPage>;

/** The shared who-liked page size — the wire default (20) and cap (100)
 * pair; the modal never walks a page size the server would clamp. */
export const LIKER_PAGE_SIZE = 20;

type ModalStatus = "idle" | "loading" | "error" | "success";

@customElement("likers-modal")
export class LikersModal extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(avatarStyles),
    unsafeCSS(styles),
  ];

  /** Host-controlled visibility: true opens (and loads) the dialog, false
   * closes it. */
  @property({ type: Boolean }) open = false;

  /** The dialog heading (catalog copy comes from the host). */
  @property() heading = "";

  /** The endpoint call this modal pages through. */
  @property({ attribute: false }) loader?: LikerPageLoader;

  @state() private _items: LikerItem[] = [];
  @state() private _status: ModalStatus = "idle";
  @state() private _error = "";
  @state() private _hasNext = false;
  @state() private _endCursor: string | null = null;
  @state() private _loadingMore = false;
  @state() private _moreError = "";

  /** The in-flight first/retry page — superseded and aborted on close. */
  private _abort: AbortController | null = null;

  /** The in-flight append; the first-page load aborts it too (a stale
   * append must never land on a fresh list). */
  private _moreAbort: AbortController | null = null;

  private get _dialog(): HTMLDialogElement | null {
    return this.renderRoot.querySelector("dialog");
  }

  disconnectedCallback() {
    this._abort?.abort();
    this._abort = null;
    this._moreAbort?.abort();
    this._moreAbort = null;
    super.disconnectedCallback();
  }

  protected updated(changed: PropertyValues<this>) {
    if (!changed.has("open")) return;
    if (this.open) {
      this._openDialog();
      void this._loadFirst();
      return;
    }
    this._abort?.abort();
    this._abort = null;
    this._moreAbort?.abort();
    this._moreAbort = null;
    this._closeDialog();
    // Reset to the neutral state: the next open starts on a fresh page 1.
    this._items = [];
    this._status = "idle";
    this._error = "";
    this._hasNext = false;
    this._endCursor = null;
    this._loadingMore = false;
    this._moreError = "";
  }

  private _openDialog() {
    const dialog = this._dialog;
    if (!dialog || dialog.open) return;
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
  }

  private _closeDialog() {
    const dialog = this._dialog;
    if (!dialog?.open) return;
    if (typeof dialog.close === "function") {
      dialog.close();
    } else {
      dialog.removeAttribute("open");
    }
  }

  /** The native close (Escape, or our own close call): a user dismissal
   * informs the host, which flips `open` back down. */
  private _onDialogClose = () => {
    if (!this.open) return;
    this.dispatchEvent(
      new CustomEvent(LIKERS_CLOSE_EVENT, { bubbles: true, composed: true }),
    );
  };

  private _onCloseClick = () => {
    this.dispatchEvent(
      new CustomEvent(LIKERS_CLOSE_EVENT, { bubbles: true, composed: true }),
    );
  };

  private async _loadFirst() {
    const loader = this.loader;
    if (!loader) {
      // A host that opened the modal without a loader wired is a wiring
      // bug — show it as a failed load instead of spinning forever.
      this._error = el.ui.error;
      this._status = "error";
      return;
    }
    this._moreAbort?.abort();
    this._moreAbort = null;
    const controller = new AbortController();
    this._abort = controller;
    this._items = [];
    this._status = "loading";
    this._error = "";
    this._hasNext = false;
    this._endCursor = null;
    this._loadingMore = false;
    this._moreError = "";

    try {
      const page = await loader(null, controller.signal);
      if (controller.signal.aborted || this._abort !== controller) return;
      this._items = page.items;
      this._hasNext = page.pageInfo.hasNextPage;
      this._endCursor = page.pageInfo.endCursor;
      this._status = "success";
    } catch (err) {
      if (controller.signal.aborted || this._abort !== controller) return;
      this._error = mapError(err);
      this._status = "error";
    }
  }

  private _onLoadMore = async () => {
    const loader = this.loader;
    const cursor = this._endCursor;
    if (!loader || cursor === null || this._loadingMore) return;
    this._moreAbort?.abort();
    const controller = new AbortController();
    this._moreAbort = controller;
    this._loadingMore = true;
    this._moreError = "";

    try {
      const page = await loader(cursor, controller.signal);
      if (controller.signal.aborted || this._moreAbort !== controller) return;
      this._items = [...this._items, ...page.items];
      this._hasNext = page.pageInfo.hasNextPage;
      this._endCursor = page.pageInfo.endCursor;
    } catch (err) {
      if (controller.signal.aborted || this._moreAbort !== controller) return;
      // The retained list stays usable — only the append failed.
      this._moreError = mapError(err);
    } finally {
      if (this._moreAbort === controller) this._loadingMore = false;
    }
  };

  render() {
    return html`
      <dialog
        class="likers-dialog"
        aria-labelledby="likers-title"
        @close=${this._onDialogClose}
      >
        <div class="likers-head">
          <h2 id="likers-title" class="likers-title">${this.heading}</h2>
          <button
            type="button"
            class="button button--icon button--ghost likers-close"
            aria-label=${el.likers.close}
            title=${el.likers.close}
            @click=${this._onCloseClick}
          >
            ${icons.x}
          </button>
        </div>
        <div class="likers-body">${this._bodyTemplate()}</div>
      </dialog>
    `;
  }

  private _bodyTemplate() {
    if (this._status === "loading" || this._status === "idle") {
      return html`<loading-spinner></loading-spinner>`;
    }
    if (this._status === "error") {
      return html`
        <error-banner .message=${this._error}></error-banner>
        <button
          type="button"
          class="button button--secondary button--sm likers-retry"
          @click=${this._loadFirst}
        >
          ${el.ui.retry}
        </button>
      `;
    }
    if (this._items.length === 0) {
      return html`<p class="likers-empty" role="status">${el.likers.empty}</p>`;
    }
    return html`
      <ul class="likers-list" role="list">
        ${this._items.map(
          (item) => html`
            <li class="liker-row">
              ${avatarTemplate(item)}
              <span class="liker-name">${item.username}</span>
            </li>
          `,
        )}
      </ul>
      ${
        this._moreError
          ? html`<p class="likers-more-error" role="alert">
              ${this._moreError}
            </p>`
          : ""
      }
      ${
        this._hasNext
          ? html`<button
              type="button"
              class="button button--secondary button--sm likers-more"
              ?disabled=${this._loadingMore}
              @click=${this._onLoadMore}
            >
              ${this._loadingMore ? el.likers.loadingMore : el.likers.loadMore}
            </button>`
          : ""
      }
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "likers-modal": LikersModal;
  }
}
