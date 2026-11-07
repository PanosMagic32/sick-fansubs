import { html, LitElement, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { copyrightNotice, el } from "@shared/catalog/el.js";
import { BUY_ME_A_COFFEE_URL, COMMUNITY_LINKS } from "@shared/config/links.js";
import { brandIcons } from "@shared/ui/icons.js";

import styles from "./app-footer.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

type HealthStatus = "loading" | "online" | "offline";

/**
 * Community links from the shared ordered list. The labels are deliberate
 * English brand names; each anchor renders its label in a `lang="en"`
 * sr-only span plus the shared new-tab suffix, matching the header's
 * external-link pattern.
 */
const SOCIAL_LINKS = COMMUNITY_LINKS.map((link) => ({
  ...link,
  icon: brandIcons[link.tone],
}));

/** The health probe's own deadline: it is not an API call, so it carries no
 * shared client timeout — a hung probe must not pin the footer's loading
 * label. */
const HEALTH_PROBE_TIMEOUT_MS = 5_000;

/**
 * Application footer — status, community links, support line, and
 * copyright+version zones. The status probes /health/ready directly (the
 * recorded transport exception — core/shell/AGENTS.md): Online means the
 * application can serve.
 */
@customElement("app-footer")
export class AppFooter extends LitElement {
  @state() private health: HealthStatus = "loading";
  private _version = "";

  // Which health probe owns the footer's status line: a reconnect starts a
  // new probe, and the previous one's late result must not paint over it.
  private _healthProbe = 0;

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  connectedCallback() {
    super.connectedCallback();
    this.setAttribute("role", "contentinfo");
    this._readVersion();
    // A reconnect starts a fresh probe: the previous result is stale until
    // this one answers.
    this.health = "loading";
    this._checkHealth(++this._healthProbe);
    // Re-probe when connectivity or visibility changes: the shell keeps the
    // footer mounted across SPA navigation, so one connect-time probe would
    // otherwise serve the whole page lifetime and contradict the offline
    // strip after a reconnect.
    window.addEventListener("online", this._onReconnect);
    document.addEventListener("visibilitychange", this._onVisibilityChange);
  }

  disconnectedCallback() {
    // Invalidate the in-flight probe: its result belongs to a teardown.
    this._healthProbe++;
    window.removeEventListener("online", this._onReconnect);
    document.removeEventListener("visibilitychange", this._onVisibilityChange);
    super.disconnectedCallback();
  }

  private _onReconnect = () => {
    this._reprobe();
  };

  private _onVisibilityChange = () => {
    if (document.visibilityState === "visible") this._reprobe();
  };

  private _reprobe() {
    this.health = "loading";
    this._checkHealth(++this._healthProbe);
  }

  /** Version from the Go server's `<meta name="app-version">` (read once). */
  private _readVersion() {
    const meta = document.head.querySelector(
      'meta[name="app-version"]',
    ) as HTMLMetaElement | null;
    this._version = meta?.content ?? "";
  }

  private async _checkHealth(probe: number) {
    try {
      const res = await fetch("/health/ready", {
        signal: AbortSignal.timeout(HEALTH_PROBE_TIMEOUT_MS),
      });
      if (!this._ownsProbe(probe)) return;
      this.health = res.ok ? "online" : "offline";
    } catch {
      if (!this._ownsProbe(probe)) return;
      this.health = "offline";
    }
  }

  private _ownsProbe(probe: number): boolean {
    return this.isConnected && probe === this._healthProbe;
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

      <!-- Community destinations: one icon row above the support line, so
           the icons render on every route without competing with the
           product navigation in the header. The nav landmark names the
           group; each link's accessible name is the sr-only brand label. -->
      <div class="footer-center">
        <nav class="footer-social" aria-label=${el.ui.communityLinks}>
          ${SOCIAL_LINKS.map(
            (link) =>
              html`<a
                class="social-${link.tone}"
                href=${link.url}
                target="_blank"
                rel="noopener noreferrer"
                >${link.icon}<span class="sr-only" lang="en">${link.label}</span
                ><span class="sr-only">${el.nav.opensInNewTab}</span></a
              >`,
          )}
        </nav>

        <!-- Donation support line: footer variant B, centered under the
             community row. Inline row on desktop; on mobile the support
             line stacks text → divider → link (see app-footer.css). The
             label is an anglicism — lang="en". -->
        <div class="footer-support">
          <span class="support-text"
            >${el.donation.supportText} <span aria-hidden="true">❤</span></span
          >
          <span class="support-divider" aria-hidden="true"></span>
          <a
            href=${BUY_ME_A_COFFEE_URL}
            target="_blank"
            rel="noopener noreferrer"
            ><span lang="en">${el.about.donateLabel}</span
            ><span class="sr-only">${el.nav.opensInNewTab}</span></a
          >
        </div>
      </div>

      <div class="footer-right">
        <small>${copyrightNotice(new Date().getFullYear())}</small>
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
