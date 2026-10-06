/**
 * Projects list page — the public projects list at /projects.
 *
 * Layout: a responsive card grid of the shared content card in its grid
 * shape, one card per project — the blog list's grid with two deliberate
 * removals:
 *   - no featured hero card,
 *   - no subtitle (the projects table has none).
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
 * The page requests each page with an explicit limit (mirrors the blog
 * list's first-page preference; the API default stays 20). Dates render
 * through the shared card's numeric Greek date + time format.
 */

import { html, LitElement, nothing, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { NAVIGATE_EVENT } from "@core/router.js";
import { el } from "@shared/catalog/el.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/ui/pager-nav.js";
import "@shared/ui/content-card.js";

import { getProjects } from "./data-access/projects-api.js";
import type { ProjectListItem } from "./data-access/types.js";
import styles from "./projects-list-page.css?inline";

import { mapError } from "@shared/utils/format.js";
import { requestSignal } from "@shared/api/request-signal.js";
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  UrlCursorPagingController,
} from "@shared/utils/url-cursor-paging.js";

type PageStatus = "loading" | "error" | "success";

@customElement("projects-list-page")
export class ProjectsListPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  @state() private status: PageStatus = "loading";
  @state() private items: ProjectListItem[] = [];
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
    path: "/projects",
    navigateEvent: NAVIGATE_EVENT,
    defaultLimit: DEFAULT_PAGE_SIZE,
    nextURL: (after, limit) =>
      `/projects?${new URLSearchParams({ limit: String(limit), after }).toString()}`,
    limitURL: (limit) =>
      `/projects?${new URLSearchParams({ limit: String(limit) }).toString()}`,
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
      const page = await getProjects(
        {
          limit,
          ...(after ? { after } : {}),
        },
        { signal: requestSignal(controller) },
      );
      // Success-path abort guard: a response landing after a newer load
      // aborted this controller must not paint stale items.
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
    // The page heading is screen-reader-only: the grid is the visual
    // entry, but the document still needs an h1 landmark (the accessibility
    // baseline) — same pattern as the blog list page.
    return html`
      <h1 class="sr-only">
        <span lang="en">${el.projects.projectsTitle}</span>
      </h1>
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
          <p class="empty" role="status">${el.projects.projectsEmpty}</p>
        </div>
      `;
    }

    return html`
      <ul class="card-grid" role="list">
        ${this.items.map(
          (project) => html`
            <li>
              <content-card
                shape="grid"
                href=${`/projects/${project.id}`}
                title=${project.title}
                heading-level=${2}
                description=${project.description}
                thumbnail-url=${project.thumbnailUrl ?? nothing}
                .creator=${project.creator}
                published-at=${project.publishedAt}
                ?sr-username=${true}
                .commentCount=${project.commentCount}
                .favoriteCount=${project.favoriteCount}
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
    "projects-list-page": ProjectsListPage;
  }
}
