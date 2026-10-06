/**
 * Comments section — the
 * click-to-load wrapper the blog/project detail pages embed instead of
 * mounting <comments-thread> eagerly.
 *
 * Collapsed (the initial state) it renders the discussion indicator: the
 * catalog heading with the count the detail DTO already carries (so the
 * click costs no request), the CTA, and the zero-state hint. A click
 * mounts the thread, scrolls the section into view under the sticky app
 * header (reduced-motion aware) and hands the section focus — the button it
 * replaced is gone, so the expansion must be followable by keyboard.
 *
 * The thread also mounts WITHOUT a click when the URL carries a
 * `#comment-<id>` fragment — on load, and on any later URL change that adds
 * one (popstate, in-app navigate) — because every notification deep link
 * targets that fragment; landing on a collapsed section would break it.
 */

import { html, LitElement, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import { NAVIGATE_EVENT } from "@core/router.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import { el } from "@shared/catalog/el.js";

import "./comments-thread.js";
import type { CommentKind } from "../data-access/types.js";
import styles from "./comments-section.css?inline";

/** True when the URL targets a comment fragment (the deep-link contract,
 * `#comment-<id>`). */
function hasCommentFragment(): boolean {
  return /^#comment-.+$/.test(window.location.hash);
}

@customElement("comments-section")
export class CommentsSection extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  /** The content kind (the URL segment name). */
  @property() kind: CommentKind = "blog-posts";

  /** The opaque content identifier. */
  @property({ attribute: "content-id" }) contentId = "";

  /** The comment total the detail item already carries — the
   * collapsed indicator, so expanding issues the first request. */
  @property({ type: Number }) count = 0;

  @state() private _expanded = false;

  connectedCallback() {
    super.connectedCallback();
    if (hasCommentFragment()) this._expanded = true;
    window.addEventListener("popstate", this._onURLChange);
    window.addEventListener(NAVIGATE_EVENT, this._onURLChange);
  }

  disconnectedCallback() {
    window.removeEventListener("popstate", this._onURLChange);
    window.removeEventListener(NAVIGATE_EVENT, this._onURLChange);
    super.disconnectedCallback();
  }

  /** A deep link that arrives after mount (back/forward, in-app navigate)
   * opens the thread too — the fragment contract covers arrival, not just
   * page load. */
  private _onURLChange = () => {
    if (!this._expanded && hasCommentFragment()) this._expanded = true;
  };

  private _expand = () => {
    this._expanded = true;
    this._pendingReveal = true;
  };

  /** Set by the CTA; updated() consumes it once the expanded render has
   * committed and the thread exists. */
  private _pendingReveal = false;

  updated() {
    if (!this._pendingReveal) return;
    this._pendingReveal = false;
    this._reveal();
  }

  private _reveal() {
    const reduce =
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
    // happy-dom does not implement scrollIntoView (the thread's focus
    // scroll guards the same way).
    if (typeof this.scrollIntoView === "function") {
      this.scrollIntoView({
        behavior: reduce ? "auto" : "smooth",
        block: "start",
      });
    }
    // The named section is the focus target: a screen reader then announces
    // where focus went (the CTA it replaced is gone), and focus sits inside
    // the thread the click just revealed.
    this.renderRoot
      .querySelector<HTMLElement>(".cs")
      ?.focus({ preventScroll: true });
  }

  render() {
    return html`
      <section class="cs" tabindex="-1" aria-label=${el.comments.title}>
        ${
          this._expanded
            ? html`<comments-thread
                kind=${this.kind}
                content-id=${this.contentId}
              ></comments-thread>`
            : this._indicatorTemplate()
        }
      </section>
    `;
  }

  private _indicatorTemplate() {
    return html`
      <div class="cs-indicator">
        <header class="cs-header">
          <h2 class="cs-title">
            ${el.comments.title}${this.count > 0 ? ` (${this.count})` : ""}
          </h2>
          ${
            this.count === 0
              ? html`<p class="cs-hint">${el.comments.empty}</p>`
              : ""
          }
        </header>
        <button
          class="button button--secondary cs-cta"
          type="button"
          @click=${this._expand}
        >
          ${el.comments.sectionCta}
        </button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "comments-section": CommentsSection;
  }
}
