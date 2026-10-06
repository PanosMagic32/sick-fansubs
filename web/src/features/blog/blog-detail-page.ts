/**
 * Blog detail page — one published post at /blog/:id.
 *
 * States are deliberate: loading, error + retry, notFound,
 * and success. The notFound state is the API's masked 404 (unknown id,
 * draft, archived, or unstamped post) — a distinct view from the router
 * fallback for unknown routes.
 *
 * The page owns ONE AbortController for its in-flight request (same rule
 * as the list page): aborted on teardown and superseded when postId
 * changes — Routes reuses this element across /blog/a → /blog/b
 * navigations, so a stale response must never paint over a newer one.
 *
 * The lead block is the shared <content-card shape="detail"> — the page
 * wires its inputs and slots the toggles and the staff edit link into its
 * actions slot; the card owns the lead block's markup and geometry.
 *
 * Meta policy: publishedAt always renders; the card renders updatedAt only
 * when the parsed updated instant is strictly after the published one.
 * Genuine edits land days later, while the migration fallback
 * (updated_at_ms = created_at_ms) lands within seconds of — sometimes
 * before — publishedAt, so the strict comparison hides fallback noise.
 * Both fields carry the API's fixed-width canonical UTC RFC 3339 invariant
 * (formatAPITime) in data-access/types.ts.
 *
 * The two lines keep their established shape (avatar chip, label, date,
 * byline — the updater keeps its own avatar). The date renders as the
 * NUMERIC Greek form (`15/08/2026`, no month word), the dash separates it
 * from the creator's name, and the name carries no «Από» prefix.
 *
 * The separator is an EN DASH: a middle dot disappears into the shell's
 * background dot grid.
 *
 * Downloads render magnet/torrent anchors. The app-shell click interceptor
 * passes non-http(s) schemes through natively (magnet: opens the client's
 * torrent handler), so these links must stay plain <a> elements.
 */

import { html, LitElement, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { consume } from "@lit/context";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { el } from "@shared/catalog/el.js";
import { ApiError } from "@shared/api/client.js";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";

import { getBlogPost } from "./data-access/blog-api.js";
import type { BlogPostDetail } from "./data-access/types.js";
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

import { mapError } from "@shared/utils/format.js";
import { isStaffRole } from "@shared/utils/roles.js";
import "@shared/ui/content-card.js";
import { icons } from "@shared/ui/icons.js";

import styles from "./blog-detail-page.css?inline";

type PageStatus = "loading" | "error" | "notFound" | "success";

@customElement("blog-detail-page")
export class BlogDetailPage extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  /** Opaque public post id from the /blog/:id route. */
  @property({ type: String }) postId = "";

  @state() private status: PageStatus = "loading";
  @state() private post: BlogPostDetail | null = null;
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
    // Live indicator sync: the toggle's confirmed
    // heart flip and the thread's count refetches update the public
    // counts in place — a stale detail indicator would visibly disagree
    // with the thread header one scroll down.
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
    if (!this.post || d.kind !== "blog-posts" || d.contentId !== this.post.id)
      return;
    this.post = {
      ...this.post,
      favoriteCount: Math.max(
        0,
        this.post.favoriteCount + (d.favorited ? 1 : -1),
      ),
    };
  };

  private _onCommentCountChanged = (e: Event) => {
    const d = (e as CustomEvent<CommentCountChangedDetail>).detail;
    if (!this.post || d.kind !== "blog-posts" || d.contentId !== this.post.id)
      return;
    this.post = { ...this.post, commentCount: d.count };
  };

  protected willUpdate(changed: PropertyValues<this>) {
    // postId changed: Routes reuses this element when navigating between
    // two detail pages — reload instead of showing the previous post.
    // hasUpdated guards the first render (connectedCallback already
    // started the initial load).
    if (changed.has("postId") && this.hasUpdated) {
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
      // The page-owned controller aborts on teardown/postId change; the
      // composed signal also carries the client timeout so a hung request
      // cannot leave the spinner forever.
      const post = await getBlogPost(this.postId, {
        signal: requestSignal(controller),
      });
      // The abort guard must cover the SUCCESS path too: a response that
      // resolves just after a newer load aborted this controller must not
      // paint stale data over the newer post.
      if (controller.signal.aborted) return;
      this.post = post;
      this.status = "success";
      // Refine the shell's base detail title with the post title; the
      // shell resets it on the next navigation.
      document.title = `${post.title} — ${el.ui.siteName}`;
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
      // same pattern as the list page). Success and notFound render real h1s.
      return html`
        <h1 class="sr-only">${el.titles.blogDetail}</h1>
        <loading-spinner></loading-spinner>
      `;
    }
    if (this.status === "notFound") {
      return html`
        <div class="centered-state">
          <h1 class="not-found-title">${el.blog.notFound}</h1>
          <a class="back-link" href="/">${el.blog.backToHome}</a>
        </div>
      `;
    }
    if (this.status === "error") {
      return html`
        <h1 class="sr-only">${el.titles.blogDetail}</h1>
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

    const post = this.post;
    if (!post) return html``;
    return html`
      <article class="detail">
        <content-card
          shape="detail"
          heading-level="1"
          title=${post.title}
          subtitle=${post.subtitle}
          description=${post.description}
          thumbnail-url=${post.thumbnailUrl}
          .creator=${post.creator}
          published-at=${post.publishedAt}
          .updater=${post.updater}
          updated-at=${post.updatedAt}
          .commentCount=${post.commentCount}
          .favoriteCount=${post.favoriteCount}
        >
          <favorite-toggle
            slot="actions"
            kind="blog-posts"
            content-id=${post.id}
          ></favorite-toggle>
          <follow-toggle
            slot="actions"
            kind="blog-posts"
            content-id=${post.id}
          ></follow-toggle>
          ${
            this._isStaff
              ? html`<a
                  slot="actions"
                  class="button button--icon button--ghost admin-edit-link"
                  href="/admin/blog/${post.id}"
                  aria-label=${el.admin.edit}
                  title=${el.admin.edit}
                  >${icons.edit}</a
                >`
              : ""
          }
        </content-card>

        ${
          post.downloads.length > 0
            ? html`
                <section class="downloads">
                  <h2 class="downloads-title">${el.blog.downloadsTitle}</h2>
                  <ul class="downloads-list" role="list">
                    ${post.downloads.map(
                      (d) => html`
                        <li class="download-row">
                          <span class="download-resolution"
                            >${d.resolution}</span
                          >
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
          kind="blog-posts"
          content-id=${post.id}
          .count=${post.commentCount}
        ></comments-section>
      </article>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "blog-detail-page": BlogDetailPage;
  }
}
