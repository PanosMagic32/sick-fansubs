/**
 * Shared pager — the page-turn bar for URL-carried cursor paging (Next
 * pushes the continuation cursor; Previous is history.back()).
 *
 * One chip-styled control row — Previous chip, the page-size select chip,
 * Next chip — with the position line «Σελίδα X από Y» BELOW it: the caption
 * states the two facts the arrows alone cannot show without competing with
 * the controls for row space. The buttons render ALWAYS and DISABLE at the
 * boundaries (first/last page) so the row keeps stable geometry and the
 * the row keeps stable geometry and the
 * `aria-disabled` state communicates the boundary. The host decides whether
 * to render the pager at all — the list pages keep it mounted while a page
 * load it owns is in flight (the host passes `busy`), so the pressed control
 * keeps focus; it is hidden on the first load, error, and empty results.
 *
 * While `busy`, the arrow controls expose `aria-disabled` and their handlers
 * no-op: a native `disabled` attribute would blur the control the page turn
 * was started from. The page-size select IS natively disabled (a native
 * select cannot be prevented from opening otherwise); if it held focus when
 * the load started, focus returns to it when the load settles.
 *
 * Arrow-only buttons: the visible row carries the glyph, and the catalog
 * «Προηγούμενη» / «Επόμενη» labels are the buttons' ACCESSIBLE NAMES
 * (aria-label) — the arrow glyph is decorative, so assistive tech announces
 * the catalog words and never reads the arrow out as a character.
 *
 * Presentation only: the host page owns the cursor/URL state machine (the
 * shared UrlCursorPagingController) and passes
 * hasPrevious/hasNext/limit/page/total down; clicks re-dispatch as
 * sf-pager-prev / sf-pager-next, and a size change as sf-limit-change
 * (detail: the new size). hasPrevious comes from the history-entry marker —
 * it survives a refresh; a shared/deep-linked page-N URL has no marker, so
 * Previous renders DISABLED there (history.back() could leave the app —
 * see the controller docs).
 */

import { html, LitElement, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { el, pagerPageOf } from "@shared/catalog/el.js";
import { DEFAULT_PAGE_SIZE } from "@shared/utils/url-cursor-paging.js";

import styles from "./pager-nav.css?inline";

@customElement("pager-nav")
export class PagerNav extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  /** Whether a previous page exists in OUR session history (the sfPager
   * marker — enabled after a Next in this tab, including across refreshes). */
  @property({ type: Boolean }) hasPrevious = false;

  /** Whether pageInfo.hasNextPage — a continuation cursor is available. */
  @property({ type: Boolean }) hasNext = false;

  /** The active page size (from the URL, normalized by the controller). */
  @property({ type: Number }) limit: number = DEFAULT_PAGE_SIZE;

  /** The 1-based page ordinal the URL states (the controller's `page`). */
  @property({ type: Number }) page = 1;

  /** The filtered row count at read time (pageInfo.total) — the page-count
   * denominator before the display floor. */
  @property({ type: Number }) total = 0;

  /** The sizes the dropdown offers (shared PAGE_SIZES). */
  @property({ type: Array }) limits: readonly number[] = [];

  /** A page load this pager started is in flight. The controls stay mounted
   * and focusable; `aria-disabled` + no-op handlers prevent a second turn. */
  @property({ type: Boolean }) busy = false;

  /** Set when the size select held focus as a load started; restored after
   * the busy state clears (a native disabled select loses focus). */
  private _restoreSelectFocus = false;

  protected override willUpdate(changed: PropertyValues) {
    if (changed.has("busy") && this.busy) {
      try {
        this._restoreSelectFocus =
          this.shadowRoot?.activeElement === this._select();
      } catch {
        // happy-dom's shadow-root activeElement traversal throws when the
        // root is detached; the browser path is the real contract.
        this._restoreSelectFocus = false;
      }
    }
  }

  /** happy-dom ignores the `selected` attribute on options (browsers honor
   * it), so the active size is applied to the select imperatively after
   * every render — otherwise the first option wins. */
  protected override updated(changed: PropertyValues) {
    const select = this._select();
    if (changed.has("limit") || changed.has("limits")) {
      if (select) select.value = String(this.limit);
    }
    if (changed.has("busy") && !this.busy && this._restoreSelectFocus) {
      this._restoreSelectFocus = false;
      select?.focus();
    }
  }

  private _select(): HTMLSelectElement | null {
    return this.renderRoot.querySelector("select");
  }

  private _onPrevClick() {
    if (this.busy || !this.hasPrevious) return;
    this.dispatchEvent(
      new CustomEvent("sf-pager-prev", { bubbles: true, composed: true }),
    );
  }

  private _onNextClick() {
    if (this.busy || !this.hasNext) return;
    this.dispatchEvent(
      new CustomEvent("sf-pager-next", { bubbles: true, composed: true }),
    );
  }

  private _onLimitChange(e: Event) {
    const select = e.target as HTMLSelectElement;
    if (this.busy) {
      // The native select still opens; keep the committed size visible.
      select.value = String(this.limit);
      return;
    }
    const size = Number.parseInt(select.value, 10);
    if (!Number.isInteger(size)) return;
    this.dispatchEvent(
      new CustomEvent("sf-limit-change", {
        detail: size,
        bubbles: true,
        composed: true,
      }),
    );
  }

  render() {
    // A hand-edited URL may carry a valid-but-unoffered limit — render it
    // as an extra option so the select never shows a blank for the active
    // size (the URL stays the source of truth).
    const options = this.limits.includes(this.limit)
      ? [...this.limits]
      : [...this.limits, this.limit];
    // A report of zero rows still displays one page of them; the floor
    // keeps the line honest for an empty-but-rendered pager.
    const pages = Math.max(1, Math.ceil(this.total / this.limit));

    return html`
      <div class="pager-wrap">
        <nav
          class="pager"
          aria-label=${el.ui.pagerLabel}
          aria-busy=${this.busy ? "true" : "false"}
        >
          <button
            class="button button--secondary button--icon chip-arrow"
            type="button"
            aria-label=${el.ui.prevPage}
            aria-disabled=${this.busy || !this.hasPrevious ? "true" : "false"}
            @click=${this._onPrevClick}
          >
            <span aria-hidden="true">←</span>
          </button>

          <label class="size-chip">
            <span class="size-label">${el.ui.pageSizeLabel}</span>
            <!-- aria-hidden: the select announces its own value — the visible
                 mirror would duplicate it in the accessible name. -->
            <span class="size-value" aria-hidden="true">${this.limit}</span>
            <span class="size-caret" aria-hidden="true">▾</span>
            <!-- The select stretches invisibly over the WHOLE chip (owner:
                 the entire chip is the hit target) — the visible label/value
                 above are decorative; the select owns focus and keyboard. -->
            <select
              class="size-select"
              aria-disabled=${this.busy ? "true" : "false"}
              ?disabled=${this.busy}
              @change=${this._onLimitChange}
            >
              ${options.map(
                (size) => html`<option value=${size}>${size}</option>`,
              )}
            </select>
          </label>

          <button
            class="button button--secondary button--icon chip-arrow"
            type="button"
            aria-label=${el.ui.nextPage}
            aria-disabled=${this.busy || !this.hasNext ? "true" : "false"}
            @click=${this._onNextClick}
          >
            <span aria-hidden="true">→</span>
          </button>
        </nav>

        <span class="page-status" aria-hidden="true"
          >${pagerPageOf(this.page, pages)}</span
        >
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "pager-nav": PagerNav;
  }
}
