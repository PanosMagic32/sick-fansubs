/**
 * Blog list page — the home route's public blog list.
 *
 * Layout: the newest post is a featured hero card on the FIRST page only
 * (page 2+ renders a plain grid), the rest flow into a responsive card
 * grid.
 *
 * Pagination is URL-carried cursor paging: `after` rides the URL, Next
 * pushes the continuation cursor, Previous is history.back(), and
 * Back/forward/refresh restore the exact page. The state machine lives in
 * the shared UrlCursorPagingController (shared/utils/url-cursor-paging.ts).
 *
 * States are deliberate: loading, error + retry, empty, and success. Every
 * failed load — first page or page turn — shows the SAME error state with
 * retry; results are REPLACED per page, never appended.
 *
 * The page requests the first page with an explicit limit of 10 (the API
 * default stays 20). Dates render through the shared numeric formatter
 * (`formatDateTimeNumeric`), the blogs/projects format.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { NAVIGATE_EVENT } from "@core/router.js";
import { el } from "@shared/catalog/el.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/pager-nav.js";
import "@shared/ui/content-card.js";

import { listBlogPosts } from "./data-access/blog-api.js";
import type { BlogPostListItem } from "./data-access/types.js";

import { mapError } from "@shared/utils/format.js";
import { requestSignal } from "@shared/api/request-signal.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";

import styles from "./blog-list-page.css?inline";

type PageStatus = "loading" | "error" | "success";

@customElement("blog-list-page")
export class BlogListPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @state() private status: PageStatus = "loading";
  @state() private items: BlogPostListItem[] = [];
  @state() private hasNextPage = false;
  @state() private endCursor: string | null = null;
  /** The loaded page's filtered row count (pageInfo.total) — the pager's
   * page-count numerator. Reset on every load start/error. */
  @state() private total = 0;
  @state() private errorMessage = "";

  /** In-flight request controller — aborted on teardown or newer loads. */
  private _abort: AbortController | null = null;

  /** The URL-carried cursor paging state machine (shared) — the controller
   * owns the URL sync, the refresh-safe hasPrevious marker, the limit, and
   * the Next/Previous mechanics (see shared/utils/url-cursor-paging.ts). */
  private readonly paging = new UrlCursorPagingController(this, {
    path: "/",
    navigateEvent: NAVIGATE_EVENT,
    defaultLimit: DEFAULT_PAGE_SIZE,
    nextURL: (after, limit) =>
      `/?${new URLSearchParams({ limit: String(limit), after }).toString()}`,
    limitURL: (limit) =>
      `/?${new URLSearchParams({ limit: String(limit) }).toString()}`,
    onLoad: (after, limit, scrollOnLoad) =>
      void this._load(after, limit, scrollOnLoad),
  });

  disconnectedCallback() {
    this._abort?.abort();
    super.disconnectedCallback();
  }

  private async _load(
    after: string | null,
    limit: number,
    scrollOnLoad: boolean,
  ) {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this.status = "loading";
    this.total = 0;

    try {
      const page = await listBlogPosts(
        {
          limit,
          ...(after ? { after } : {}),
        },
        { signal: requestSignal(controller) },
      );
      // Success-path abort guard: a response landing after a newer load
      // aborted this controller must not paint stale items (same rule as
      // the detail page).
      if (controller.signal.aborted) return;
      this.items = page.items;
      this.hasNextPage = page.pageInfo.hasNextPage;
      this.endCursor = page.pageInfo.endCursor;
      this.total = page.pageInfo.total;
      this.status = "success";
      // A page turn starts at the top; popstate-synced loads keep the
      // browser's restored scroll position.
      if (scrollOnLoad) window.scrollTo(0, 0);
    } catch (err) {
      if (controller.signal.aborted) return; // superseded by a newer load
      this.errorMessage = mapError(err);
      this.total = 0;
      this.status = "error";
    }
  }

  private _onNextClick = () => {
    if (!this.endCursor) return;
    this.paging.next(this.endCursor);
  };

  private _onPrevClick = () => {
    this.paging.prev();
  };

  /** Page-size change: the controller replaceState's the first page with
   * the new size and re-syncs (a cursor is only meaningful with the same
   * limit). */
  private _onLimitChange = (e: Event) => {
    this.paging.setLimit((e as CustomEvent<number>).detail);
  };

  private _onRetry = () => {
    // Re-sync from the URL — it still carries the same page cursor, so
    // the failed load (first page or page turn) is retried as-is.
    this.paging.sync();
  };

  render() {
    // The page heading is screen-reader-only: the hero is the visual entry
    // point, but the document still needs an h1 landmark (the accessibility
    // baseline). document.title uses el.titles.home ("Αρχική") via the
    // shell's path mapping — deliberately different copy from the in-page
    // heading.
    return html`
      <h1 class="sr-only">${el.blog.pageTitle}</h1>
      ${this._stateTemplate()}
    `;
  }

  private _stateTemplate() {
    // Every state renders inside the shared reserved-footprint block so the
    // chrome above never resizes between them.
    if (this.status === "loading") {
      return html`
        <div class="state-block">
          <loading-spinner></loading-spinner>
        </div>
      `;
    }
    if (this.status === "error") {
      return html`
        <div class="centered-state state-block">
          <error-banner .message=${this.errorMessage}></error-banner>
          <button
            class="button button--secondary retry"
            type="button"
            @click=${this._onRetry}
          >
            ${el.ui.retry}
          </button>
        </div>
      `;
    }
    if (this.items.length === 0) {
      return html`
        <div class="centered-state state-block">
          <p class="empty" role="status">${el.blog.empty}</p>
        </div>
      `;
    }

    // The hero is the FIRST page's entry point only: on a cursor page
    // every item renders as a plain grid card.
    const isFirstPage = this.paging.after === null;
    const hero = isFirstPage ? (this.items[0] ?? null) : null;
    const rest = isFirstPage ? this.items.slice(1) : this.items;

    return html`
      ${
        hero
          ? html`<content-card
              shape="hero"
              href=${`/blog/${hero.id}`}
              title=${hero.title}
              badge=${el.blog.latestBadge}
              badge-tone="latest"
              subtitle=${hero.subtitle}
              description=${hero.description}
              thumbnail-url=${hero.thumbnailUrl}
              .creator=${hero.creator}
              published-at=${hero.publishedAt}
              .commentCount=${hero.commentCount}
              .favoriteCount=${hero.favoriteCount}
            ></content-card>`
          : ""
      }

      <ul class="card-grid" role="list">
        ${rest.map(
          (post) => html`
            <li>
              <content-card
                shape="grid"
                href=${`/blog/${post.id}`}
                title=${post.title}
                heading-level=${isFirstPage ? 3 : 2}
                subtitle=${post.subtitle}
                description=${post.description}
                thumbnail-url=${post.thumbnailUrl}
                .creator=${post.creator}
                published-at=${post.publishedAt}
                ?sr-username=${true}
                .commentCount=${post.commentCount}
                .favoriteCount=${post.favoriteCount}
              ></content-card>
            </li>
          `,
        )}
      </ul>

      <pager-nav
        ?hasPrevious=${this.paging.hasPrevious}
        ?hasNext=${this.hasNextPage}
        .limit=${this.paging.limit}
        .page=${this.paging.page}
        .total=${this.total}
        .limits=${PAGE_SIZES}
        @sf-pager-prev=${this._onPrevClick}
        @sf-pager-next=${this._onNextClick}
        @sf-limit-change=${this._onLimitChange}
      ></pager-nav>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "blog-list-page": BlogListPage;
  }
}
