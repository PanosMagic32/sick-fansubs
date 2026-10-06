/**
 * Project create/edit form — /admin/projects/new (create) and
 * /admin/projects/:id (edit). Modify AND delete both live here, the same
 * shape the blog form mirrors.
 *
 * The slug field is admin+-only: the create form has NO slug input (the
 * server generates it from the title), and the edit form shows the slug
 * input only to admin+ — a moderator edit omits the field, and the server
 * 403s a moderator-submitted slug anyway.
 *
 * The thumbnail is the upload-first flow: the media-upload widget returns
 * the server's storage path, and the form submits `thumbnailPath` — the edit
 * form falls back to the loaded project's stored path when no new image was
 * uploaded. The field is never hand-built.
 *
 * Downloads: the <download-rows> widget owns the row list
 * (add/remove/reorder); the form loads the stored rows in edit mode and
 * submits the full replacement array (the row label is the batch `name`).
 * Every row needs at least one link — a light client guard shows the
 * required copy; the SERVER validates bounds and the link grammar.
 *
 * Concurrency: the edit form submits the If-Match precondition built from
 * the loaded revision. A 412 means someone else edited — the form reloads
 * the fresh row and shows the Greek "changed elsewhere" copy. A 403 (stale
 * CSRF) heals through a silent session revalidate like the favorites heart.
 *
 * The guard, the load, the action machine, and the delete flow are the
 * shared ContentFormBase skeleton; this module owns the project fields, the
 * write bodies, the API calls, and the <form> markup.
 */

import { html, nothing, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import { navigate } from "@core/router.js";
import { el, violationMessage } from "@shared/catalog/el.js";
import { isContentCreatorRole } from "@shared/utils/roles.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";

import {
  createProject,
  deleteProject,
  getStaffProject,
  updateProject,
  type ProjectCreateBody,
  type ProjectUpdateBody,
  type StaffProject,
} from "./data-access/projects-admin-api.js";
import "./ui/media-upload.js";
import "./ui/download-rows.js";
import type { DownloadRowValue } from "./ui/download-rows.js";
import {
  ContentFormBase,
  type ContentStatus,
  type FormAction,
} from "./ui/content-form-base.js";
import styles from "./project-form-page.css?inline";

@customElement("project-form-page")
export class ProjectFormPage extends ContentFormBase<
  StaffProject,
  ProjectCreateBody
> {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(styles),
  ];

  /** The project id in edit mode; "" in create mode (/admin/projects/new). */
  @property({ type: String }) projectId = "";

  // Form fields.
  @state() private _slug = "";

  protected get _rowId(): string {
    return this.projectId;
  }

  protected get _listPath(): string {
    return "/admin/projects";
  }

  protected get _savedLabel(): string {
    return el.admin.savedProject;
  }

  protected get _deletedLabel(): string {
    return el.admin.deletedProject;
  }

  /** The stale-revision message a 412 shows; the shared default names the
   * blog post, so the project form names itself. */
  protected get _preconditionMessage(): string {
    return el.admin.staleProject;
  }

  protected _fetchRow(id: string, signal: AbortSignal): Promise<StaffProject> {
    return getStaffProject(id, { signal });
  }

  protected _downloadRows(row: StaffProject): DownloadRowValue[] {
    return row.downloads.map((d): DownloadRowValue => ({
      label: d.name,
      magnetUrl: d.magnetUrl,
      torrentUrl: d.torrentUrl,
    }));
  }

  protected _applyKindFields(row: StaffProject): void {
    this._slug = row.slug;
  }

  /** The form's heading — visible in the ready state, sr-only in every
   * non-ready state so a heading landmark always exists. */
  private get _formTitle(): string {
    return this._isEdit
      ? el.admin.formTitleEditProject
      : el.admin.formTitleCreateProject;
  }

  /** The slug field is admin+-only — a moderator never sees it, so the form
   * cannot submit a capability the session lacks. */
  private get _canEditSlug(): boolean {
    const s = this.session?.state;
    if (!s || s.status !== "authenticated") return false;
    return isContentCreatorRole(s.user.role);
  }

  private _thumbnailPath(): string {
    const uploaded = this._mediaWidget()?.getValue()?.path ?? null;
    // The uploaded path wins; otherwise the loaded project's stored path
    // (the write contract wants the STORAGE form — never the served URL).
    return uploaded ?? this._row?.thumbnailPath ?? "";
  }

  protected _writeBody(status: ContentStatus): ProjectCreateBody | null {
    const thumbnailPath = this._thumbnailPath();
    if (!thumbnailPath) {
      this._fieldError = violationMessage("required");
      return null;
    }
    const widget = this._downloadWidget();
    const downloads = widget?.getValue() ?? [];
    // Light client guard for the product rule (every row needs at least
    // one link) — the server re-validates everything else.
    if (widget && !widget.isValid()) {
      this._fieldError = violationMessage("required");
      return null;
    }
    return {
      title: this._title.trim(),
      description: this._description.trim(),
      thumbnailPath,
      status,
      downloads: downloads.map((r) => ({
        name: r.label,
        magnetUrl: r.magnetUrl,
        torrentUrl: r.torrentUrl,
      })),
    };
  }

  protected async _save(
    action: FormAction,
    status: ContentStatus,
    body: ProjectCreateBody,
  ): Promise<void> {
    if (this._isEdit && this._row) {
      // The slug rides ONLY the update body, and only for admin+ — the
      // server rejects a moderator-submitted slug with 403.
      const updateBody: ProjectUpdateBody = { ...body };
      if (this._canEditSlug) {
        updateBody.slug = this._slug.trim();
      }
      const updated = await updateProject(
        this.projectId,
        this._row.revision,
        updateBody,
      );
      this._row = updated;
      this._slug = updated.slug;
      this._status = updated.status;
      if (status === "published") {
        // Published: the editor's next question is "how does it look?".
        navigate(`/projects/${updated.id}`);
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

    const created = await createProject(body);
    if (status === "published") {
      navigate(`/projects/${created.id}`);
      return;
    }
    // Created as a draft: the list is where a draft belongs.
    navigate(this._listPath);
  }

  protected async _deleteRow(id: string, revision: number): Promise<void> {
    await deleteProject(id, revision);
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
        <p>${el.admin.formNotFoundProject}</p>
        <a href="/admin/projects">${el.admin.backToDashboard}</a>
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
      <section class="project-form">
        <header class="form-header">
          <h1>${this._formTitle}</h1>
          ${this._stateChipTemplate()}
          <a href="/admin/projects">${el.admin.backToDashboard}</a>
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
            <span>${el.admin.descriptionLabel}</span>
            <textarea
              .value=${this._description}
              @input=${(e: Event) => {
                this._description = (e.target as HTMLTextAreaElement).value;
              }}
            ></textarea>
          </label>

          ${
            this._isEdit && this._canEditSlug
              ? html`<label class="field">
                  <span lang="en">${el.admin.slugLabel}</span>
                  <input
                    type="text"
                    .value=${this._slug}
                    @input=${(e: Event) => {
                      this._slug = (e.target as HTMLInputElement).value;
                      this._fieldError = "";
                    }}
                  />
                  <p class="slug-hint">${el.admin.slugHint}</p>
                </label>`
              : ""
          }

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
              rowLabel=${el.admin.downloadNameLabel}
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
    "project-form-page": ProjectFormPage;
  }
}
