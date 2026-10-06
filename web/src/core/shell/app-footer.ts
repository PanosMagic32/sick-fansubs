import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { el } from "@shared/catalog/el.js";
import { BUY_ME_A_COFFEE_URL } from "@shared/config/links.js";

import styles from "./app-footer.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

type HealthStatus = "loading" | "online" | "offline";

/** The health probe's own deadline: it is not an API call, so it carries no
 * shared client timeout — a hung probe must not pin the footer's loading
 * label. */
const HEALTH_PROBE_TIMEOUT_MS = 5_000;

/**
 * Application footer — status, support line, and copyright+version zones.
 * The status probes /health/ready directly (the recorded transport
 * exception — core/shell/AGENTS.md): Online means the application can
 * serve.
 */
@customElement("app-footer")
export class AppFooter extends LitElement {
  @state() private health: HealthStatus = "loading";
  private _version = "";

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  connectedCallback() {
    super.connectedCallback();
    this.setAttribute("role", "contentinfo");
    this._readVersion();
    this._checkHealth();
  }

  /** Version from the Go server's `<meta name="app-version">` (read once). */
  private _readVersion() {
    const meta = document.head.querySelector(
      'meta[name="app-version"]',
    ) as HTMLMetaElement | null;
    this._version = meta?.content ?? "";
  }

  private async _checkHealth() {
    try {
      const res = await fetch("/health/ready", {
        signal: AbortSignal.timeout(HEALTH_PROBE_TIMEOUT_MS),
      });
      this.health = res.ok ? "online" : "offline";
    } catch {
      this.health = "offline";
    }
  }

  render() {
    const label =
      this.health === "online"
        ? el.ui.statusOnline
        : this.health === "offline"
          ? el.ui.statusOffline
          : el.ui.loading;

    // Online/Offline are deliberate anglicisms: the lang="en" scope tells
    // assistive tech to pronounce them as English. Loading is Greek and
    // stays outside the scope.
    const statusLabel =
      this.health === "online" || this.health === "offline"
        ? html`<span lang="en">${label}</span>`
        : label;

    // © and the v-prefix are structural glyphs, exempt from the catalog.
    const version = this._version
      ? html`<span>v${this._version}</span>`
      : html``;

    return html`
      <div class="footer-left">
        <!-- role="status" (implicit aria-live=polite): the health check
             resolves AFTER the first paint, so screen readers must be
             told when loading flips to online/offline. -->
        <span role="status">
          <span class="status-dot ${this.health}" aria-hidden="true"></span>
          ${statusLabel}
        </span>
      </div>

      <!-- Donation support line: footer variant B, centered between the
           status and the copyright. Inline row on desktop; on mobile the
           zone stacks text → divider → link (see app-footer.css). The
           label is an anglicism — lang="en". -->
      <div class="footer-center">
        <span class="support-text"
          >${el.donation.supportText} <span aria-hidden="true">❤</span></span
        >
        <span class="support-divider" aria-hidden="true"></span>
        <a href=${BUY_ME_A_COFFEE_URL} target="_blank" rel="noopener noreferrer"
          ><span lang="en">${el.about.donateLabel}</span></a
        >
      </div>

      <div class="footer-right">
        <small>© ${new Date().getFullYear()} Sick-HQ</small>
        ${version}
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "app-footer": AppFooter;
  }
}
