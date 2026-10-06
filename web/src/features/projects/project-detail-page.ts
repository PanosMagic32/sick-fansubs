/**
 * Projects detail page — one published project at /projects/:id.
 *
 * The page mirrors the blog detail page structurally — thumbnail on top, a
 * header block with the title, the description with the blog-detail quote
 * treatment (❝ ❞ glyphs + accent bar), the meta lines, and a plain
 * downloads list. The structural differences: the projects table has no
 * subtitle, and the thumbnail is null-guarded (a masked value renders no
 * img). The lead block is the shared <content-card shape="detail"> — the
 * page wires its inputs and slots the toggles and the staff edit link into
 * its actions slot; the card owns the lead block's markup and geometry.
 *
 * States are deliberate: loading, error + retry, notFound, and success.
 * The notFound state is the API's masked 404 (unknown id or unpublished
 * project) — a distinct view from the router fallback for unknown routes.
 *
 * The page owns ONE AbortController for its in-flight request (same rule
 * as the blog pages): aborted on teardown and superseded when projectId
 * changes — Routes reuses this element across /projects/a → /projects/b
 * navigations, so a stale response must never paint over a newer one.
 *
 * Meta policy: publishedAt always renders; updatedAt renders only when it
 * is strictly after publishedAt (the shared card compares the parsed
 * instants — the same policy as the blog detail page: hide the migration
 * fallback noise). Both lines carry the NUMERIC Greek date form, an EN DASH
 * between the date and the name, and no «Από» prefix.
 *
 * Downloads render magnet/torrent anchors. The app-shell click interceptor
 * passes non-http(s) schemes through natively (magnet: opens the client's
 * torrent handler), so these links must stay plain <a> elements.
 */

import { consume } from "@lit/context";
import { html, LitElement, nothing, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { el } from "@shared/catalog/el.js";
import { ApiError } from "@shared/api/client.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import { mapError } from "@shared/utils/format.js";
import { isStaffRole } from "@shared/utils/roles.js";
import "@shared/ui/content-card.js";
import { icons } from "@shared/ui/icons.js";

import { getProject } from "./data-access/projects-api.js";
import type { ProjectDetail } from "./data-access/types.js";
// The favorites heart — cross-feature import under the authenticated
// capabilities exception (authenticated capabilities live in the auth
// feature; content features consume them). The detail page also listens
// for its confirmed-success event to keep the public count live; the event
// contracts themselves live in @shared/events.js.
import "@features/auth/ui/favorite-toggle.js";
// The follow bell — the same cross-feature authenticated-capability
// exception as the heart; sits beside it in the title row.
import "@features/auth/ui/follow-toggle.js";
// The comments section — the public content feature at the end of the
// detail section. It mounts the thread only after a click or a
// `#comment-<id>` deep link; its count event keeps the top indicator live.
import "@features/comments/ui/comments-section.js";
import {
  COMMENT_COUNT_CHANGED_EVENT,
  FAVORITE_CHANGED_EVENT,
  type CommentCountChangedDetail,
  type FavoriteChangedDetail,
} from "@shared/events.js";
// The session context powers the staff-only edit affordance — the same
// cross-feature authenticated-capability exception as the heart.
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";

import { requestSignal } from "@shared/api/request-signal.js";
import styles from "./project-detail-page.css?inline";

type PageStatus = "loading" | "error" | "notFound" | "success";

@customElement("project-detail-page")
export class ProjectDetailPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  /** Opaque public project id from the /projects/:id route. */
  @property({ type: String }) projectId = "";

  @state() private status: PageStatus = "loading";
  @state() private project: ProjectDetail | null = null;
  @state() private errorMessage = "";

  // The staff edit button's session (moderator+). Client-side
  // visibility only; the server enforces the real authorization.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  private get _isStaff(): boolean {
    if (this.session?.state.status !== "authenticated") return false;
    return isStaffRole(this.session.state.user.role);
  }

  /** In-flight request controller — aborted on teardown or newer loads. */
  private _abort: AbortController | null = null;

  connectedCallback() {
    super.connectedCallback();
    void this._load();
    // Live indicator sync — same wiring as the blog
    // detail page: the toggle's confirmed flip and the thread's count
    // refetches update the public counts in place.
    window.addEventListener(FAVORITE_CHANGED_EVENT, this._onFavoriteChanged);
    window.addEventListener(
      COMMENT_COUNT_CHANGED_EVENT,
      this._onCommentCountChanged,
    );
  }

  disconnectedCallback() {
    this._abort?.abort();
    window.removeEventListener(FAVORITE_CHANGED_EVENT, this._onFavoriteChanged);
    window.removeEventListener(
      COMMENT_COUNT_CHANGED_EVENT,
      this._onCommentCountChanged,
    );
    super.disconnectedCallback();
  }

  private _onFavoriteChanged = (e: Event) => {
    const d = (e as CustomEvent<FavoriteChangedDetail>).detail;
    if (
      !this.project ||
      d.kind !== "projects" ||
      d.contentId !== this.project.id
    )
      return;
    this.project = {
      ...this.project,
      favoriteCount: Math.max(
        0,
        this.project.favoriteCount + (d.favorited ? 1 : -1),
      ),
    };
  };

  private _onCommentCountChanged = (e: Event) => {
    const d = (e as CustomEvent<CommentCountChangedDetail>).detail;
    if (
      !this.project ||
      d.kind !== "projects" ||
      d.contentId !== this.project.id
    )
      return;
    this.project = { ...this.project, commentCount: d.count };
  };

  protected willUpdate(changed: PropertyValues<this>) {
    // projectId changed: Routes reuses this element when navigating between
    // two detail pages — reload instead of showing the previous project.
    // hasUpdated guards the first render (connectedCallback already
    // started the initial load).
    if (changed.has("projectId") && this.hasUpdated) {
      void this._load();
    }
  }

  private async _load() {
    this._abort?.abort();
    const controller = new AbortController();
    this._abort = controller;
    this.status = "loading";
    this.errorMessage = "";

    try {
      // The page-owned controller aborts on teardown/projectId change; the
      // composed signal also carries the client timeout so a hung request
      // cannot leave the spinner forever.
      const project = await getProject(this.projectId, {
        signal: requestSignal(controller),
      });
      // The abort guard must cover the SUCCESS path too: a response that
      // resolves just after a newer load aborted this controller must not
      // paint stale data over the newer project.
      if (controller.signal.aborted) return;
      this.project = project;
      this.status = "success";
      // Refine the shell's base title ("Projects — …") with the project
      // title; the shell resets it on the next navigation.
      document.title = `${project.title} — ${el.ui.siteName}`;
    } catch (err) {
      if (controller.signal.aborted) return; // superseded by a newer load
      if (err instanceof ApiError && err.type === "/problems/not-found") {
        this.status = "notFound";
        return;
      }
      this.errorMessage = mapError(err);
      this.status = "error";
    }
  }

  render() {
    if (this.status === "loading") {
      // sr-only h1 in the non-success states: the document keeps a heading
      // landmark even while loading/erroring (the accessibility baseline,
      // same pattern as the blog pages). Success and notFound render real h1s.
      return html`
        <h1 class="sr-only">
          <span lang="en">${el.titles.projectsDetail}</span>
        </h1>
        <loading-spinner></loading-spinner>
      `;
    }
    if (this.status === "notFound") {
      return html`
        <div class="centered-state">
          <h1 class="not-found-title">${el.projects.projectsNotFound}</h1>
          <a class="back-link" href="/">${el.blog.backToHome}</a>
        </div>
      `;
    }
    if (this.status === "error") {
      return html`
        <h1 class="sr-only">
          <span lang="en">${el.titles.projectsDetail}</span>
        </h1>
        <div class="centered-state">
          <error-banner .message=${this.errorMessage}></error-banner>
          <button
            class="button button--secondary retry"
            type="button"
            @click=${this._load}
          >
            ${el.ui.retry}
          </button>
        </div>
      `;
    }

    const project = this.project;
    if (!project) return html``;
    return html`
      <article class="detail">
        <content-card
          shape="detail"
          heading-level="1"
          title=${project.title}
          description=${project.description}
          thumbnail-url=${project.thumbnailUrl ?? nothing}
          .creator=${project.creator}
          published-at=${project.publishedAt}
          .updater=${project.updater}
          updated-at=${project.updatedAt}
          .commentCount=${project.commentCount}
          .favoriteCount=${project.favoriteCount}
        >
          <favorite-toggle
            slot="actions"
            kind="projects"
            content-id=${project.id}
          ></favorite-toggle>
          <follow-toggle
            slot="actions"
            kind="projects"
            content-id=${project.id}
          ></follow-toggle>
          ${
            this._isStaff
              ? html`<a
                  slot="actions"
                  class="button button--icon button--ghost admin-edit-link"
                  href="/admin/projects/${project.id}"
                  aria-label=${el.admin.edit}
                  title=${el.admin.edit}
                  >${icons.edit}</a
                >`
              : ""
          }
        </content-card>

        ${
          project.downloads.length > 0
            ? html`
                <section class="downloads">
                  <h2 class="downloads-title">
                    ${el.projects.projectsDownloadsTitle}
                  </h2>
                  <ul class="downloads-list" role="list">
                    ${project.downloads.map(
                      (d) => html`
                        <li class="download-row">
                          <span class="download-name">${d.name}</span>
                          ${
                            d.magnetUrl
                              ? html`<a
                                  class="button button--secondary button--sm download-link"
                                  href=${d.magnetUrl}
                                  lang="en"
                                  >${el.blog.magnet}</a
                                >`
                              : ""
                          }
                          ${
                            d.torrentUrl
                              ? html`<a
                                  class="button button--secondary button--sm download-link"
                                  href=${d.torrentUrl}
                                  lang="en"
                                  >${el.blog.torrent}</a
                                >`
                              : ""
                          }
                        </li>
                      `,
                    )}
                  </ul>
                </section>
              `
            : ""
        }

        <comments-section
          kind="projects"
          content-id=${project.id}
          .count=${project.commentCount}
        ></comments-section>
      </article>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "project-detail-page": ProjectDetailPage;
  }
}
