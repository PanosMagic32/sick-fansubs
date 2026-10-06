/**
 * Admin logs tab and the audit browser — the two
 * super-admin operational surfaces at /admin/logs.
 *
 * Super-admin only: it extends AdminStaffPageBase and narrows the shared
 * moderator+ floor to the super-admin predicate, mirroring the server's
 * `identity.CanViewLogs` (the base sanctions that override for exactly this
 * case).
 * A below-floor viewer therefore makes NO request — the tab link is hidden
 * from them too (admin-tabs reads the same predicate).
 *
 * Two INDEPENDENT sections on one route: the log tail (`/staff/logs`) and the
 * audit ledger (`/staff/audit-events`). They are separate reads with separate
 * filters, states, and abort controllers — a failed audit read must not blank
 * the log table, and vice versa. The base's status/error slots belong to the
 * log section (the page's original content); the audit section keeps its own,
 * and a 403 from EITHER flips the page to forbidden, because that is the
 * server's floor moving rather than a section failing.
 *
 * Both are bounded TAIL reads, not paged collections: no cursor, no "load
 * more", no total. Filters ride the URL so a reload or a shared link keeps the
 * view; applying them replaces the history entry rather than pushing one (a
 * filter change is not a navigation step worth a back button). Nothing is
 * cached and nothing polls — the error banner's retry is the recovery path.
 */

import { html, nothing, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { requestSignal } from "@shared/api/request-signal.js";
import { ApiError } from "@shared/api/client.js";
import { el } from "@shared/catalog/el.js";
import { formatDateTimeNumeric, mapError } from "@shared/utils/format.js";
import { isSuperAdminRole, roleLabel } from "@shared/utils/roles.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "./ui/admin-tabs.js";
import {
  AdminStaffPageBase,
  type PageStatus,
} from "./ui/admin-staff-page-base.js";
import adminPageStyles from "./ui/admin-page.css?inline";
import styles from "./admin-logs-page.css?inline";

import {
  DEFAULT_LOG_LEVEL,
  DEFAULT_LOG_LIMIT,
  getStaffLogs,
  LOG_LEVELS,
  LOG_LIMIT_MAX,
  LOG_LIMIT_MIN,
  LOG_QUERY_MAX,
  type StaffLogItem,
  type StaffLogLevel,
} from "./data-access/logs-api.js";
import {
  AUDIT_EVENTS,
  AUDIT_LIMIT_MAX,
  AUDIT_LIMIT_MIN,
  DEFAULT_AUDIT_LIMIT,
  getStaffAuditEvents,
  type StaffAuditEvent,
} from "./data-access/audit-api.js";

/** The route this page is mounted at — the base of the URL sync. */
const LOGS_PATH = "/admin/logs";

/** The catalog label for a known wire enum value. An UNKNOWN value (a kind the
 * server added ahead of this bundle) renders verbatim inside lang="en", the
 * language annotation every wire enum on this page carries. */
function auditEventLabel(event: string) {
  const label =
    el.admin.auditEventNames[event as keyof typeof el.admin.auditEventNames];
  return label ?? html`<span lang="en">${event}</span>`;
}

@customElement("admin-logs-page")
export class AdminLogsPage extends AdminStaffPageBase {
  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(adminPageStyles),
    unsafeCSS(styles),
  ];

  /* ── Log section state (the base's status/error are its slots) ──── */

  @state() private items: StaffLogItem[] | null = null;
  @state() private level: StaffLogLevel = DEFAULT_LOG_LEVEL;
  @state() private query = "";
  @state() private limit = DEFAULT_LOG_LIMIT;

  /* ── Audit section state ────────────────────────────────────────── */

  @state() private auditItems: StaffAuditEvent[] | null = null;
  @state() private auditStatus: PageStatus = "loading";
  @state() private auditError = "";
  @state() private auditEvent = "";
  @state() private auditLimit = DEFAULT_AUDIT_LIMIT;
  private _auditAbort: AbortController | null = null;

  /**
   * Latches the forbidden state. The two sections load independently, so
   * without this a slower log response could repaint a page whose audit read
   * had already been told the floor moved — showing log data to a viewer the
   * server just refused, and leaving the audit section spinning forever.
   */
  private _forbidden = false;

  /** The super-admin floor — the server's `identity.CanViewLogs` mirror. */
  protected _hasStaffAccess(role: string): boolean {
    return isSuperAdminRole(role);
  }

  /** The first authenticated load (the base's latch calls this once). */
  protected _start() {
    this._readURL();
    void this._load();
    void this._loadAudit();
  }

  disconnectedCallback() {
    this._auditAbort?.abort();
    super.disconnectedCallback();
  }

  /* ── Loading ────────────────────────────────────────────────────── */

  private async _load() {
    const controller = this._beginLoad();

    try {
      const result = await getStaffLogs(
        { level: this.level, q: this.query, limit: this.limit },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted || this._forbidden) return;
      this.items = result.items;
      this.status = "success";
    } catch (err) {
      if (this._forbidden) return;
      this._failLoad(err, controller);
    }
  }

  private async _loadAudit() {
    this._auditAbort?.abort();
    const controller = new AbortController();
    this._auditAbort = controller;
    this.auditStatus = "loading";
    this.auditError = "";

    try {
      const result = await getStaffAuditEvents(
        { event: this.auditEvent, limit: this.auditLimit },
        { signal: requestSignal(controller) },
      );
      if (controller.signal.aborted || this._forbidden) return;
      this.auditItems = result.items;
      this.auditStatus = "success";
    } catch (err) {
      if (controller.signal.aborted || this._forbidden) return;
      this._failAuditLoad(err);
    }
  }

  /**
   * Maps a failed audit read. A 403 is the SERVER's floor moving, not a
   * section failing, so it latches the page instead of this section — the
   * shared policy of AdminStaffPageBase._failLoad, which cannot be reused
   * here because it writes the log section's own status/error slots.
   */
  private _failAuditLoad(err: unknown) {
    if (err instanceof ApiError && err.status === 403) {
      this._forbidden = true;
      this.status = "forbidden";
      return;
    }
    this.auditError = mapError(err);
    this.auditStatus = "error";
  }

  /* ── URL sync ───────────────────────────────────────────────────── */

  /** Prime both sections' filters from the URL, so a reload or a shared link
   * shows the same view. Values outside the wire contracts are ignored (the
   * server would 422 them; the page starts from its defaults instead). */
  private _readURL() {
    const params = new URLSearchParams(window.location.search);

    const level = params.get("level");
    if (level !== null && (LOG_LEVELS as readonly string[]).includes(level)) {
      this.level = level as StaffLogLevel;
    }

    const query = params.get("q");
    if (query && query.length <= LOG_QUERY_MAX) this.query = query;

    const limit = Number(params.get("limit"));
    if (
      Number.isInteger(limit) &&
      limit >= LOG_LIMIT_MIN &&
      limit <= LOG_LIMIT_MAX
    ) {
      this.limit = limit;
    }

    const event = params.get("aevent");
    if (event && (AUDIT_EVENTS as readonly string[]).includes(event)) {
      this.auditEvent = event;
    }

    const auditLimit = Number(params.get("alimit"));
    if (
      Number.isInteger(auditLimit) &&
      auditLimit >= AUDIT_LIMIT_MIN &&
      auditLimit <= AUDIT_LIMIT_MAX
    ) {
      this.auditLimit = auditLimit;
    }
  }

  /** Reflect BOTH sections' filters in the URL without a navigation step. */
  private _syncURL() {
    const params = new URLSearchParams();
    params.set("level", this.level);
    if (this.query) params.set("q", this.query);
    params.set("limit", String(this.limit));
    if (this.auditEvent) params.set("aevent", this.auditEvent);
    params.set("alimit", String(this.auditLimit));
    window.history.replaceState({}, "", `${LOGS_PATH}?${params.toString()}`);
  }

  /** Apply the log filter form: adopt the values, sync the URL, fetch once. */
  private _onLogFilterSubmit = (e: Event) => {
    e.preventDefault();
    const data = new FormData(e.target as HTMLFormElement);

    const level = String(data.get("level") ?? "");
    if ((LOG_LEVELS as readonly string[]).includes(level)) {
      this.level = level as StaffLogLevel;
    }

    this.query = String(data.get("q") ?? "").trim();

    // The input carries min/max/type=number, so an out-of-contract value is
    // rare; when one arrives anyway it keeps the previous count rather than
    // sending a request the server would answer with a 422.
    const limit = Number(String(data.get("limit") ?? "").trim());
    if (
      Number.isInteger(limit) &&
      limit >= LOG_LIMIT_MIN &&
      limit <= LOG_LIMIT_MAX
    ) {
      this.limit = limit;
    }

    this._syncURL();
    void this._load();
  };

  /** Apply the audit filter form — the same round trip for the second read. */
  private _onAuditFilterSubmit = (e: Event) => {
    e.preventDefault();
    const data = new FormData(e.target as HTMLFormElement);

    const event = String(data.get("event") ?? "");
    if (event === "" || (AUDIT_EVENTS as readonly string[]).includes(event)) {
      this.auditEvent = event;
    }

    const limit = Number(String(data.get("alimit") ?? "").trim());
    if (
      Number.isInteger(limit) &&
      limit >= AUDIT_LIMIT_MIN &&
      limit <= AUDIT_LIMIT_MAX
    ) {
      this.auditLimit = limit;
    }

    this._syncURL();
    void this._loadAudit();
  };

  /* ── Rendering: the log section ─────────────────────────────────── */

  private _logFilters() {
    return html`
      <form class="logs-filters" @submit=${this._onLogFilterSubmit}>
        <label class="logs-filter">
          ${el.admin.logsLevelLabel}
          <select name="level" lang="en">
            ${LOG_LEVELS.map(
              (level) =>
                html`<option value=${level} ?selected=${level === this.level}>
                  ${level.toUpperCase()}
                </option>`,
            )}
          </select>
        </label>
        <label class="logs-filter">
          ${el.admin.logsQueryLabel}
          <input
            type="search"
            name="q"
            .value=${this.query}
            maxlength=${LOG_QUERY_MAX}
          />
        </label>
        <label class="logs-filter">
          ${el.admin.logsLimitLabel}
          <input
            type="number"
            name="limit"
            .value=${String(this.limit)}
            min=${LOG_LIMIT_MIN}
            max=${LOG_LIMIT_MAX}
          />
        </label>
        <button
          type="submit"
          class="button button--secondary button--sm logs-apply"
        >
          ${el.admin.logsApply}
        </button>
      </form>
    `;
  }

  /** One log row. The level is a CHIP carrying `data-level`: the colour
   * follows the level — a warning or an error must be findable at a glance
   * in a wall of lines — and the word itself stays the signal (colour is
   * never the only one). The row also carries the level, so a sheet can tint
   * the whole line without a second attribute. */
  private _logRow(item: StaffLogItem) {
    const fields = Object.keys(item.fields).length
      ? JSON.stringify(item.fields)
      : el.admin.logsFieldsEmpty;

    return html`
      <tr class="logs-row" data-level=${item.level}>
        <td class="logs-time">${formatDateTimeNumeric(item.time)}</td>
        <td>
          <span class="logs-level" data-level=${item.level} lang="en"
            >${item.level}</span
          >
        </td>
        <td class="logs-message">${item.msg}</td>
        <td class="logs-fields">${fields}</td>
      </tr>
    `;
  }

  private _logResults() {
    if (this.items === null || this.items.length === 0) {
      return html`<p class="logs-empty" role="status">
        ${el.admin.logsEmpty}
      </p>`;
    }
    return html`
      <p class="logs-hint">${el.admin.logsHint}</p>
      <div
        class="logs-scroll"
        tabindex="0"
        role="region"
        aria-labelledby="logs-title"
      >
        <table class="logs-table">
          <caption class="sr-only">
            ${el.admin.logsTableCaption}
          </caption>
          <thead>
            <tr>
              <th scope="col">
                <span lang="en">${el.admin.logsTimeColumn}</span>
              </th>
              <th scope="col">${el.admin.logsLevelColumn}</th>
              <th scope="col">${el.admin.logsMessageColumn}</th>
              <th scope="col">${el.admin.logsFieldsColumn}</th>
            </tr>
          </thead>
          <tbody>
            ${this.items.map((item) => this._logRow(item))}
          </tbody>
        </table>
      </div>
    `;
  }

  /* ── Rendering: the audit section ───────────────────────────────── */

  private _auditFilters() {
    return html`
      <form class="logs-filters" @submit=${this._onAuditFilterSubmit}>
        <label class="logs-filter">
          ${el.admin.auditEventLabel}
          <select name="event">
            <option value="" ?selected=${this.auditEvent === ""}>
              ${el.admin.auditEventAll}
            </option>
            ${AUDIT_EVENTS.map(
              (event) =>
                html`<option
                  value=${event}
                  ?selected=${event === this.auditEvent}
                >
                  ${auditEventLabel(event)}
                </option>`,
            )}
          </select>
        </label>
        <label class="logs-filter">
          ${el.admin.auditLimitLabel}
          <input
            type="number"
            name="alimit"
            .value=${String(this.auditLimit)}
            min=${AUDIT_LIMIT_MIN}
            max=${AUDIT_LIMIT_MAX}
          />
        </label>
        <button
          type="submit"
          class="button button--secondary button--sm logs-apply"
        >
          ${el.admin.auditApply}
        </button>
      </form>
    `;
  }

  /** One audit row. The result wears the same chip as the log levels:
   * success and failure are distinct at a glance in a ledger that is mostly
   * identifiers, while the WORD stays the signal. The colour rides the chip's
   * border (see the stylesheet) because the semantic token as small text does
   * not clear 4.5:1 in the light theme. */
  private _auditRow(item: StaffAuditEvent) {
    return html`
      <tr class="audit-row" data-result=${item.result}>
        <td class="logs-time">${formatDateTimeNumeric(item.createdAt)}</td>
        <td class="audit-event">${auditEventLabel(item.event)}</td>
        <td>
          <span class="audit-result" data-result=${item.result} lang="en"
            >${item.result}</span
          >
        </td>
        <td class="audit-id">${item.actorId ?? el.admin.auditNone}</td>
        <td class="audit-id">
          ${item.targetId ?? el.admin.auditNone}
          ${
            item.targetRole
              ? html`<span class="audit-role"
                  >${roleLabel(item.targetRole)}</span
                >`
              : nothing
          }
        </td>
        <td class="audit-id">${item.requestId}</td>
        <td class="audit-id">${item.remoteAddr}</td>
      </tr>
    `;
  }

  private _auditResults() {
    if (this.auditItems === null || this.auditItems.length === 0) {
      return html`<p class="logs-empty" role="status">
        ${el.admin.auditEmpty}
      </p>`;
    }
    return html`
      <p class="logs-hint">${el.admin.auditHint}</p>
      <div
        class="logs-scroll"
        tabindex="0"
        role="region"
        aria-labelledby="audit-title"
      >
        <table class="logs-table">
          <caption class="sr-only">
            ${el.admin.auditTableCaption}
          </caption>
          <thead>
            <tr>
              <th scope="col">
                <span lang="en">${el.admin.auditTimeColumn}</span>
              </th>
              <th scope="col">${el.admin.auditEventColumn}</th>
              <th scope="col">${el.admin.auditResultColumn}</th>
              <th scope="col">
                <span lang="en">${el.admin.auditActorColumn}</span>
              </th>
              <th scope="col">
                <span lang="en">${el.admin.auditTargetColumn}</span>
              </th>
              <th scope="col">
                <span lang="en">${el.admin.auditRequestColumn}</span>
              </th>
              <th scope="col">
                <span lang="en">${el.admin.auditAddrColumn}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            ${this.auditItems.map((item) => this._auditRow(item))}
          </tbody>
        </table>
      </div>
    `;
  }

  private _auditSection() {
    return html`
      <section class="logs-section" aria-labelledby="audit-title">
        <h2 id="audit-title">${el.admin.auditTitle}</h2>
        ${this._auditFilters()}
        ${
          this.auditStatus === "error"
            ? html`<error-banner
                .message=${this.auditError}
                @retry=${() => this._loadAudit()}
              ></error-banner>`
            : nothing
        }
        ${
          this.auditStatus === "loading"
            ? html`<loading-spinner></loading-spinner>`
            : nothing
        }
        ${this.auditStatus === "success" ? this._auditResults() : nothing}
      </section>
    `;
  }

  render() {
    return html`
      <section class="admin-page">
        <admin-tabs .current=${"logs"}></admin-tabs>

        <header class="admin-header">
          <h1>${el.admin.logsTab}</h1>
        </header>

        ${
          this.status === "forbidden"
            ? html`<p class="admin-forbidden" role="alert">
                ${el.problems["/problems/forbidden"]}
              </p>`
            : nothing
        }
        ${
          this.status === "forbidden"
            ? nothing
            : html`
                <section class="logs-section" aria-labelledby="logs-title">
                  <h2 id="logs-title">${el.admin.logsSectionTitle}</h2>
                  ${this._logFilters()}
                  ${
                    // Section-scoped, like the audit banner: the base's error
                    // slot belongs to THIS section, so it renders here rather
                    // than at page level.
                    this.status === "error"
                      ? html`<error-banner
                          .message=${this.errorMessage}
                          @retry=${() => this._load()}
                        ></error-banner>`
                      : nothing
                  }
                  ${
                    this.status === "loading"
                      ? html`<loading-spinner></loading-spinner>`
                      : nothing
                  }
                  ${this.status === "success" ? this._logResults() : nothing}
                </section>
                ${this._auditSection()}
              `
        }
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "admin-logs-page": AdminLogsPage;
  }
}
