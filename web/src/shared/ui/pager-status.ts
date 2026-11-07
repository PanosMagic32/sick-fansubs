/**
 * Persistent pager live region. The visible caption lives inside
 * <pager-nav>, which stays mounted while a page turn loads; an ARIA live
 * region must persist across the change to announce it, so this element is
 * always mounted and carries the same sentence in a visually hidden region.
 *
 * The text is empty until the page reports a non-zero total. Hosts keep the
 * last successful total while a load is in flight, so the mounted pager can
 * keep its geometry and focus; the sentence changes when the response lands
 * (an unchanged node is not re-announced). A failed load clears the total.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement, property } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { pagerPageOf } from "@shared/catalog/el.js";
import { DEFAULT_PAGE_SIZE } from "@shared/utils/url-cursor-paging.js";

@customElement("pager-status")
export class PagerStatus extends LitElement {
  static styles = [unsafeCSS(sharedStyles), unsafeCSS(controlStyles)];

  /** The 1-based page ordinal the URL states (the controller's `page`). */
  @property({ type: Number }) page = 1;

  /** The filtered row count at read time (pageInfo.total). */
  @property({ type: Number }) total = 0;

  /** The active page size (the controller's `limit`). */
  @property({ type: Number }) limit: number = DEFAULT_PAGE_SIZE;

  render() {
    const pages = Math.max(1, Math.ceil(this.total / this.limit));
    const text = this.total > 0 ? pagerPageOf(this.page, pages) : "";
    return html`<span class="sr-only" role="status">${text}</span>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "pager-status": PagerStatus;
  }
}
