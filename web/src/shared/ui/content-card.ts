/**
 * Content card — THE card element for every content surface: the blog and
 * projects grids, the blog page's featured hero, the search/favorites/admin
 * rows, and the two detail heroes.
 *
 * One element, four shapes, typed inputs in and slots out
 * (docs/patterns/web/components.md). A shape owns its layout and the
 * arrangement of the inputs it consumes; a caller never re-styles a shape,
 * it only fills the inputs the shape reads:
 *
 *   grid   thumbnail, title with the subtitle beside it, quoted
 *          description, then the footer (creator byline, stats chip, details
 *          marker). The blog/projects list cards. A null thumbnail keeps the
 *          16/9 cell as a seam.
 *   hero   the grid layout as a lead card: two columns on desktop, the
 *          title beside an optional badge, the same footer. The blog list's
 *          first-page entry point.
 *   row    a horizontal list row: optional badge and meta line on top, then
 *          the title — beside an inline subtitle behind a decorative
 *          separator when one is set — optional secondary line, clamped
 *          description (PLAIN text — the quote treatment belongs to the
 *          cards, not to a result row), stats. Two geometries: the roomy
 *          search result and `dense` for the compact favorites/admin rows.
 *          Renders a link when `href` is set, a static row otherwise.
 *          `heading-level="0"` keeps the title plain text for a management
 *          row a list already names.
 *   detail a detail page's lead block: thumbnail, title row (with the
 *          action slot), optional subtitle, stats, quoted description with
 *          the accent bar, then the byline lines (published, and updated
 *          when it is strictly later).
 *
 * `thumbnailUrl` is a string or null; an empty value renders the same
 * no-image form as null (a null attribute binding arrives as ""). The
 * grid and hero shapes keep the image cell as a seam; the row and detail
 * shapes render nothing.
 *
 * `href` links the grid, hero and row shapes (the whole card is one
 * anchor); an empty `href` renders the same box unlinked. The detail shape
 * never links — its title row carries the actions instead.
 *
 * Slots for per-item actions: `actions` is the in-flow control (a row's
 * trailing action, the detail title row's toggles and staff edit link);
 * `overlay` is a linked row's corner control, placed in the frame's
 * top-right so the row reserves room for it. A linked card never carries an
 * in-flow action — an interactive control inside the card's own anchor is
 * invalid — so a linked row uses `overlay`.
 *
 * An internal anchor is safe: the shell's link interceptor reads
 * `e.composedPath()`, so a click on the card's anchor routes like any other
 * link (docs/patterns/web/components.md).
 */

import { html, LitElement, unsafeCSS, type TemplateResult } from "lit";
import { customElement, property } from "lit/decorators.js";

import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import {
  commentCountLabel,
  el,
  favoriteCountLabel,
} from "@shared/catalog/el.js";
import { formatDateTimeNumeric } from "@shared/utils/format.js";

import { avatarTemplate, type AvatarUserRef } from "./avatar.js";
import { icons } from "./icons.js";
import avatarStyles from "./avatar.css?inline";
import statsStyles from "./content-stats.css?inline";
import styles from "./content-card.css?inline";

export type ContentCardShape = "grid" | "hero" | "row" | "detail";

/** The badge's visual treatments — one per chip the surfaces already had. */
export type ContentCardBadgeTone =
  "accent" | "project" | "latest" | "draft" | "published" | "archived";

@customElement("content-card")
export class ContentCard extends LitElement {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(avatarStyles),
    unsafeCSS(statsStyles),
    unsafeCSS(styles),
  ];

  /** The layout this card renders. Reflected: the sheet keys on it. */
  @property({ reflect: true }) shape: ContentCardShape = "grid";

  /** The compact row geometry (the favorites/admin lists, not search). */
  @property({ type: Boolean, reflect: true }) dense = false;

  /** The card's link target for the grid/hero/row shapes. Empty renders a
   * static card (the admin rows); the detail shape never links. */
  @property() href = "";

  /** The heading text. */
  @property() title = "";

  /** The title's level: 1..3 renders that heading, 0 renders plain text
   * (a list row whose list already names it — the management rows). Any
   * other value falls back to 2. */
  @property({ type: Number, attribute: "heading-level" }) headingLevel = 2;

  /** The title's display line: inline beside the title behind the decorative
   * separator on the grid/hero/row shapes, stacked on `detail`. */
  @property() subtitle = "";

  /** The quoted description — the ❝ ❞ treatment every shape shares. */
  @property() description = "";

  /** The image URL; null or an empty value renders the shape's no-image
   * form. */
  @property({ attribute: "thumbnail-url" }) thumbnailUrl: string | null = null;

  /** The chip's text; a template is allowed so an anglicism can carry its
   * `lang="en"` wrapper (the search type badge). */
  @property() badge: string | TemplateResult = "";

  /** The chip's treatment. */
  @property({ attribute: "badge-tone" }) badgeTone: ContentCardBadgeTone =
    "accent";

  /** The muted line under a row's title (the projects list shows the slug). */
  @property() secondary = "";

  /** The row meta line's label ("Ενημέρωση"); empty renders the instant
   * alone. */
  @property({ attribute: "meta-label" }) metaLabel = "";

  /** The row meta line's instant; the shape renders it as a `<time>`. */
  @property({ attribute: "meta-instant" }) metaInstant = "";

  /** The byline's creator (the avatar chip), for the grid/hero/detail
   * shapes. */
  @property({ attribute: false }) creator: AvatarUserRef | null = null;

  /** The byline's publication instant (grid/hero/detail). */
  @property({ attribute: "published-at" }) publishedAt = "";

  /** Render the creator's username for assistive tech where the visual
   * design relies on the avatar alone (the grid cards set it; the hero and
   * the detail byline do not). */
  @property({ type: Boolean, attribute: "sr-username" }) srUsername = false;

  /** The detail shape's second byline avatar (the last editor), when one is
   * set. */
  @property({ attribute: false }) updater: AvatarUserRef | null = null;

  /** The detail shape's update instant; a line renders only when it is
   * strictly later than `publishedAt`. */
  @property({ attribute: "updated-at" }) updatedAt = "";

  /** The stats chip's comment count; the chip renders only when both
   * counts are set (null = this surface has no indicator row). */
  @property({ type: Number, attribute: "comment-count" }) commentCount:
    number | null = null;

  /** The stats chip's favorite count. */
  @property({ type: Number, attribute: "favorite-count" }) favoriteCount:
    number | null = null;

  /** Marks the host when the corner slot carries a control, so the row
   * reserves the control's room instead of running text under it. */
  private _onOverlaySlotChange = (e: Event) => {
    const slot = e.target as HTMLSlotElement;
    this.toggleAttribute("data-overlay", slot.assignedElements().length > 0);
  };

  render() {
    switch (this.shape) {
      case "hero":
        return this._heroTemplate();
      case "row":
        return this._rowTemplate();
      case "detail":
        return this._detailTemplate();
      default:
        return this._gridTemplate();
    }
  }

  private _gridTemplate(): TemplateResult {
    return this._linkTemplate(html`
      <article class="frame">
        ${this._thumbCellTemplate(true)}
        <div class="body">
          ${this._titleLineTemplate()}
          ${
            this.description
              ? html`<p class="description">${this._quotedTemplate()}</p>`
              : ""
          }
          <div class="footer">
            <span class="meta">${this._avatarMetaTemplate()}</span>
            ${this._statsTemplate()} ${this._markerTemplate()}
          </div>
        </div>
      </article>
    `);
  }

  private _heroTemplate(): TemplateResult {
    return this._linkTemplate(html`
      <article class="frame">
        ${this._thumbCellTemplate(false)}
        <div class="body">
          <div class="topline">
            ${this._titleLineTemplate()} ${this._badgeTemplate()}
          </div>
          ${
            this.description
              ? html`<p class="description">${this._quotedTemplate()}</p>`
              : ""
          }
          <div class="footer">
            <span class="meta">${this._avatarMetaTemplate()}</span>
            ${this._statsTemplate()} ${this._markerTemplate()}
          </div>
        </div>
      </article>
    `);
  }

  /** The grid/hero link: the whole card is one anchor; a card without an
   * `href` renders the same box unlinked. */
  private _linkTemplate(inner: TemplateResult): TemplateResult {
    return this.href
      ? html`<a class="link" href=${this.href}>${inner}</a>`
      : html`<div class="link">${inner}</div>`;
  }

  private _rowTemplate(): TemplateResult {
    const inner = html`
      ${this._rowThumbTemplate()}
      <div class="body">
        <div class="topline">
          ${this._badgeTemplate()} ${this._rowMetaTemplate()}
        </div>
        ${this.subtitle ? this._titleLineTemplate() : this._titleTemplate()}
        ${
          this.secondary
            ? html`<span class="secondary">${this.secondary}</span>`
            : ""
        }
        ${
          this.description
            ? html`<p class="description">${this.description}</p>`
            : ""
        }
        ${this._statsTemplate()}
      </div>
    `;

    return html`
      ${
        this.href
          ? html`<a class="frame link" href=${this.href}>${inner}</a>`
          : html`<div class="frame">${inner}<slot name="actions"></slot></div>`
      }
      <slot name="overlay" @slotchange=${this._onOverlaySlotChange}></slot>
    `;
  }

  private _detailTemplate(): TemplateResult {
    return html`
      ${
        this.thumbnailUrl
          ? html`<img class="thumb" src=${this.thumbnailUrl} alt="" />`
          : ""
      }
      <header class="header">
        <div class="title-row">
          ${this._titleTemplate()}
          <slot name="actions"></slot>
        </div>
        ${this.subtitle ? html`<p class="subtitle">${this.subtitle}</p>` : ""}
      </header>
      <div class="detail-stats">${this._statsTemplate()}</div>
      ${
        this.description
          ? html`<p class="description">${this._quotedTemplate()}</p>`
          : ""
      }
      <div class="detail-meta">${this._detailMetaTemplate()}</div>
    `;
  }

  // The title line shared by the grid, hero and row shapes: the subtitle
  // sits beside the title behind a decorative separator, never on its own
  // muted line. The detail shape keeps its own stacked header.
  private _titleLineTemplate(): TemplateResult {
    return html`<div class="title-line">
      ${this._titleTemplate()}
      ${
        this.subtitle
          ? html`<span class="subtitle-sep" aria-hidden="true">–</span>
              <span class="subtitle">${this.subtitle}</span>`
          : ""
      }
    </div>`;
  }

  /** The title at the level the reading order asks for; level 0 keeps it
   * plain text. */
  private _titleTemplate(): TemplateResult {
    switch (this.headingLevel) {
      case 0:
        return html`<span class="title">${this.title}</span>`;
      case 1:
        return html`<h1 class="title">${this.title}</h1>`;
      case 3:
        return html`<h3 class="title">${this.title}</h3>`;
      default:
        return html`<h2 class="title">${this.title}</h2>`;
    }
  }

  /** The quoted description; the text span is the hook the detail shape's
   * `pre-line` uses to preserve data line breaks. */
  private _quotedTemplate(): TemplateResult {
    return html`<span aria-hidden="true">❝</span>
      <span class="description-text">${this.description}</span>
      <span aria-hidden="true">❞</span>`;
  }

  /** The grid/hero thumbnail; a missing image keeps the cell as a border
   * seam so the grid rhythm (and the hero's column ratio) survives it. The
   * hero image is above the fold and stays eager. */
  private _thumbCellTemplate(lazy: boolean): TemplateResult {
    return this.thumbnailUrl
      ? html`<img
          class="thumb"
          src=${this.thumbnailUrl}
          alt=""
          loading=${lazy ? "lazy" : "eager"}
        />`
      : html`<div class="thumb thumb-empty" aria-hidden="true"></div>`;
  }

  /** The row thumbnail; a missing image renders nothing. */
  private _rowThumbTemplate(): TemplateResult | string {
    return this.thumbnailUrl
      ? html`<img
          class="thumb"
          src=${this.thumbnailUrl}
          alt=""
          loading="lazy"
        />`
      : "";
  }

  private _badgeTemplate(): TemplateResult | string {
    return this.badge
      ? html`<span class="badge" data-tone=${this.badgeTone}
          >${this.badge}</span
        >`
      : "";
  }

  /** The row's meta line: an optional label, a middle dot, the instant. */
  private _rowMetaTemplate(): TemplateResult | string {
    if (!this.metaInstant) return "";
    return html`<span class="meta"
      >${
        this.metaLabel
          ? html`<span class="meta-label">${this.metaLabel}</span
              ><span aria-hidden="true"> · </span>`
          : ""
      }<time class="date" datetime=${this.metaInstant}
        >${formatDateTimeNumeric(this.metaInstant)}</time
      ></span
    >`;
  }

  /** The grid/hero byline: avatar chip, optional screen-reader username,
   * the short numeric Greek date. */
  private _avatarMetaTemplate(): TemplateResult {
    return html`
      ${avatarTemplate(this.creator)}
      ${
        this.srUsername && this.creator?.username
          ? html`<span class="sr-only">${this.creator.username}</span>`
          : ""
      }
      <time class="date" datetime=${this.publishedAt}
        >${formatDateTimeNumeric(this.publishedAt)}</time
      >
    `;
  }

  /** The detail bylines: the publication line, then the update line when the
   * content was edited after it was published. */
  private _detailMetaTemplate(): TemplateResult {
    const line = (
      label: string,
      instant: string,
      user: AvatarUserRef | null,
    ) => html`
      <p class="meta-line">
        ${avatarTemplate(user)}
        <span class="meta-label">${label}</span>
        <time datetime=${instant}>${formatDateTimeNumeric(instant)}</time>
        ${
          user
            ? html`<span class="meta-separator" aria-hidden="true">–</span>
                <span class="meta-byline">${user.username}</span>`
            : ""
        }
      </p>
    `;

    return html`
      ${line(el.blog.published, this.publishedAt, this.creator)}
      ${
        // Parsed instants, not the display strings: the update line is
        // about time order, not text order.
        Date.parse(this.updatedAt) > Date.parse(this.publishedAt)
          ? line(el.blog.updated, this.updatedAt, this.updater)
          : ""
      }
    `;
  }

  /** The indicator chip: both counts render together — including zeros —
   * and is omitted unless both counts are set (a page with no indicator). */
  private _statsTemplate(): TemplateResult | string {
    if (this.commentCount === null || this.favoriteCount === null) return "";
    return html`
      <span class="content-stats">
        <span
          class="stat"
          role="img"
          aria-label=${commentCountLabel(this.commentCount)}
        >
          <span class="stat-icon" aria-hidden="true"
            >${icons.messageSquare}</span
          >
          <span class="stat-num" aria-hidden="true">${this.commentCount}</span>
        </span>
        <span
          class="stat"
          role="img"
          aria-label=${favoriteCountLabel(this.favoriteCount)}
        >
          <span class="stat-icon" aria-hidden="true">${icons.heartFilled}</span>
          <span class="stat-num" aria-hidden="true">${this.favoriteCount}</span>
        </span>
      </span>
    `;
  }

  /** The icon-only details marker (two chevrons); the glyphs are structural
   * and exempt from the catalog, the accessible name is not. */
  private _markerTemplate(): TemplateResult {
    return html`<span class="more" role="img" aria-label=${el.ui.details}
      >❯❯</span
    >`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "content-card": ContentCard;
  }
}
