/**
 * Blog create/edit form — /admin/blog/new (create) and /admin/blog/:id
 * (edit). Modify AND delete both live on this page.
 *
 * The thumbnail is the upload-first flow: the media-upload widget returns
 * the server's storage path, and the form submits `thumbnailPath` — the edit
 * form falls back to the loaded post's stored path when no new image was
 * uploaded. The field is never hand-built.
 *
 * Downloads: the <download-rows> widget owns the row list
 * (add/remove/reorder); the form loads the stored rows in edit mode and
 * submits the full replacement array. Every row needs at least one link — a
 * light client guard shows the required copy; the SERVER validates bounds
 * and the link grammar.
 *
 * Concurrency: the edit form submits the If-Match precondition built from
 * the loaded revision (the same value the ETag header carries). A 412 means
 * someone else edited — the form reloads the fresh row and shows the Greek
 * "changed elsewhere" copy. A 428 cannot happen from this form (it always
 * sends the header); a 403 (stale CSRF) heals through a silent session
 * revalidate like the favorites heart.
 *
 * The guard, the load, the action machine, and the delete flow are the
 * shared ContentFormBase skeleton; this module owns the blog fields, the
 * write body, the API calls, and the <form> markup.
 */

import { html, nothing, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import { navigate } from "@core/router.js";
import { el, violationMessage } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";

import {
  createBlogPost,
  deleteBlogPost,
  getStaffBlogPost,
  updateBlogPost,
  type BlogPostWrite,
  type StaffBlogPost,
} from "./data-access/blog-admin-api.js";
import "./ui/media-upload.js";
import "./ui/download-rows.js";
import type { DownloadRowValue } from "./ui/download-rows.js";
import {
  ContentFormBase,
  type ContentStatus,
  type FormAction,
} from "./ui/content-form-base.js";
import styles from "./blog-form-page.css?inline";

@customElement("blog-form-page")
export class BlogFormPage extends ContentFormBase<
  StaffBlogPost,
  BlogPostWrite
> {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(styles),
  ];

  /** The post id in edit mode; "" in create mode (/admin/blog/new). */
  @property({ type: String }) postId = "";

  // Form fields.
  @state() private _subtitle = "";

  protected get _rowId(): string {
    return this.postId;
  }

  protected get _listPath(): string {
    return "/admin";
  }

  protected get _savedLabel(): string {
    return el.admin.saved;
  }

  protected get _deletedLabel(): string {
    return el.admin.deleted;
  }

  /** The form's heading — visible in the ready state, sr-only in every
   * non-ready state so a heading landmark always exists. */
  private get _formTitle(): string {
    return this._isEdit ? el.admin.formTitleEdit : el.admin.formTitleCreate;
  }

  protected _fetchRow(id: string, signal: AbortSignal): Promise<StaffBlogPost> {
    return getStaffBlogPost(id, { signal });
  }

  protected _downloadRows(row: StaffBlogPost): DownloadRowValue[] {
    return row.downloads.map((d): DownloadRowValue => ({
      label: d.resolution,
      magnetUrl: d.magnetUrl,
      torrentUrl: d.torrentUrl,
    }));
  }

  protected _applyKindFields(row: StaffBlogPost): void {
    this._subtitle = row.subtitle;
  }

  protected _writeBody(status: ContentStatus): BlogPostWrite | null {
    const uploaded = this._mediaWidget()?.getValue()?.path ?? null;
    // The uploaded path wins; otherwise the loaded post's stored path (the
    // write contract wants the STORAGE form — never the served URL).
    const thumbnailPath = uploaded ?? this._row?.thumbnailPath ?? "";
    if (!thumbnailPath) {
      this._fieldError = violationMessage("required");
      return null;
    }
    const rowsWidget = this._downloadWidget();
    const downloads = rowsWidget?.getValue() ?? [];
    // Light client guard for the product rule (every row needs at least
    // one link) — the server re-validates everything else.
    if (rowsWidget && !rowsWidget.isValid()) {
      this._fieldError = violationMessage("required");
      return null;
    }
    return {
      title: this._title.trim(),
      subtitle: this._subtitle.trim(),
      description: this._description.trim(),
      thumbnailPath,
      status,
      downloads: downloads.map((r) => ({
        resolution: r.label,
        magnetUrl: r.magnetUrl,
        torrentUrl: r.torrentUrl,
      })),
    };
  }

  protected async _save(
    action: FormAction,
    status: ContentStatus,
    body: BlogPostWrite,
  ): Promise<void> {
    if (this._isEdit && this._row) {
      const updated = await updateBlogPost(
        this.postId,
        this._row.revision,
        body,
      );
      this._row = updated;
      this._status = updated.status;
      if (status === "published") {
        // Published: the editor's next question is "how does it look?".
        navigate(`/blog/${updated.id}`);
        return;
      }
      if (action === "archive") {
        // Archived: it is off the public site — back to the list.
        navigate(this._listPath);
        return;
      }
      // A draft save stays put so the editor can keep working.
      this._success = this._savedLabel;
      return;
    }

    const created = await createBlogPost(body);
    if (status === "published") {
      navigate(`/blog/${created.id}`);
      return;
    }
    // Created as a draft: the list is where a draft belongs.
    navigate(this._listPath);
  }

  protected async _deleteRow(id: string, revision: number): Promise<void> {
    await deleteBlogPost(id, revision);
  }

  render() {
    if (this.status === "loading") {
      return html`
        <h1 class="sr-only">${this._formTitle}</h1>
        <loading-spinner></loading-spinner>
      `;
    }
    if (this.status === "forbidden") {
      return html`
        <h1 class="sr-only">${this._formTitle}</h1>
        <p role="alert">${el.problems["/problems/forbidden"]}</p>
      `;
    }
    if (this.status === "notFound") {
      return html`
        <h1 class="sr-only">${this._formTitle}</h1>
        <p>${el.admin.formNotFound}</p>
        <a href="/admin">${el.admin.backToDashboard}</a>
      `;
    }
    if (this.status === "error") {
      return html`
        <h1 class="sr-only">${this._formTitle}</h1>
        <error-banner
          .message=${this.errorMessage}
          @retry=${() =>
            this._isEdit ? this._loadRow() : (this.status = "ready")}
        ></error-banner>
      `;
    }

    return html`
      <section class="blog-form">
        <header class="form-header">
          <h1>${this._formTitle}</h1>
          ${this._stateChipTemplate()}
          <a href="/admin">${el.admin.backToDashboard}</a>
        </header>

        <form @submit=${this._onSubmit}>
          <div class="field">
            <label for="content-title">${el.admin.titleLabel}</label>
            <input
              id="content-title"
              type="text"
              .value=${this._title}
              aria-invalid=${this._titleError ? "true" : nothing}
              aria-describedby=${
                this._titleError ? "content-title-error" : nothing
              }
              @input=${(e: Event) => {
                this._title = (e.target as HTMLInputElement).value;
                this._fieldError = "";
                this._titleError = "";
              }}
            />
            ${
              this._titleError
                ? html`<span class="field-error" id="content-title-error"
                    >${this._titleError}</span
                  >`
                : ""
            }
          </div>
          <label class="field">
            <span>${el.admin.subtitleLabel}</span>
            <input
              type="text"
              .value=${this._subtitle}
              @input=${(e: Event) => {
                this._subtitle = (e.target as HTMLInputElement).value;
              }}
            />
          </label>
          <label class="field">
            <span>${el.admin.descriptionLabel}</span>
            <textarea
              .value=${this._description}
              @input=${(e: Event) => {
                this._description = (e.target as HTMLTextAreaElement).value;
              }}
            ></textarea>
          </label>

          <div class="field">
            <span>${el.admin.thumbnailLabel}</span>
            ${
              this._isEdit && this._row?.thumbnailUrl
                ? html`<img
                    class="current-thumbnail"
                    src=${this._row.thumbnailUrl}
                    alt=""
                  />`
                : ""
            }
            <media-upload></media-upload>
          </div>

          <div class="field">
            <span>${el.admin.downloadsLabel}</span>
            <p class="downloads-hint">${el.admin.downloadsHint}</p>
            <download-rows
              rowLabel=${el.admin.downloadResolutionLabel}
              rowPlaceholder="1080p"
            ></download-rows>
          </div>

          ${
            this._fieldError
              ? html`<p class="field-error" role="alert">
                  ${this._fieldError}
                </p>`
              : ""
          }
          ${
            this._error
              ? html`<p class="form-error" role="alert">${this._error}</p>`
              : ""
          }
          <p class="form-success" role="status">${this._success}</p>

          <div class="form-actions">
            <button
              type="submit"
              class="button button--secondary action-save"
              ?disabled=${this._pending}
            >
              ${this._buttonLabel("save")}
            </button>
            ${
              // A published row's alternatives are the two ways off the
              // public site; everything else can be published.
              this._isEdit && this._status === "published"
                ? html`
                    <button
                      type="button"
                      class="button button--secondary action-unpublish"
                      ?disabled=${this._pending}
                      @click=${() => this._onAction("unpublish")}
                    >
                      ${this._buttonLabel("unpublish")}
                    </button>
                    <button
                      type="button"
                      class="button button--secondary action-archive"
                      ?disabled=${this._pending}
                      @click=${() => this._onAction("archive")}
                    >
                      ${this._buttonLabel("archive")}
                    </button>
                  `
                : html`<button
                    type="button"
                    class="button button--secondary action-publish"
                    ?disabled=${this._pending}
                    @click=${() => this._onAction("publish")}
                  >
                    ${this._buttonLabel("publish")}
                  </button>`
            }
            ${
              this._isEdit && this._canDelete
                ? html`<button
                    type="button"
                    class="button button--danger delete-button"
                    ?disabled=${this._pending}
                    @click=${this._onDelete}
                  >
                    ${el.admin.deleteButton}
                  </button>`
                : ""
            }
          </div>
        </form>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "blog-form-page": BlogFormPage;
  }
}
