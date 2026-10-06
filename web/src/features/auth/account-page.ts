/**
 * Account page — the first protected route.
 *
 * States:
 *   initializing  — loading spinner (session bootstrap not yet complete)
 *   authenticated — account view: the five section tabs (profile,
 *                   favorites, notifications, devices, security)
 *   anonymous     — guard redirects to /sign-in (replaceState); exception:
 *                   after a successful password change the success view
 *                   renders instead (all sessions were revoked — the user
 *                   must sign in again)
 *   error         — bootstrap failed; shows message + retry button
 *
 * The page keeps ONE shadow root — the panels' aria-labelledby/role=tabpanel
 * contract and the page's stylesheet require it — so each panel's state,
 * handlers, and templates live in a reactive section controller under
 * `ui/` that renders through `template()`. The page composes the tab strip
 * and the panel wrappers, owns the URL/tab wiring, and keeps the flags
 * shared between two panels (`pending`, `actionError`) plus the session
 * context.
 *
 * Guard: page-local, following the proven pattern of the two auth pages. It
 * stays page-local deliberately: its condition has two page-specific
 * exceptions (pending, _passwordChanged) that a shared controller would have
 * to parameterize for a single consumer.
 */

import { consume } from "@lit/context";
import { html, LitElement, unsafeCSS, type PropertyValues } from "lit";
import { customElement, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { redirect } from "@core/router.js";
import { el } from "@shared/catalog/el.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import formStyles from "@shared/ui/forms.css?inline";
import "@shared/ui/loading-spinner.js";
import "@shared/ui/error-banner.js";
import "@shared/pwa/install-control.js";
import "@features/notifications/ui/push-settings.js";

import styles from "./account-page.css?inline";
import authFormStyles from "./ui/form.css?inline";
import { AccountFavoritesSection } from "./ui/account-favorites-section.js";
import { AccountProfileSection } from "./ui/account-profile-section.js";
import { AccountSecuritySection } from "./ui/account-security-section.js";
import { AccountSessionsSection } from "./ui/account-sessions-section.js";

/** The account page's section tabs. Array order is the tablist's DOM and
 * keyboard order (Left/Right and Home/End walk exactly this list). */
const ACCOUNT_TABS = [
  "profile",
  "favorites",
  "notifications",
  "devices",
  "security",
] as const;
type AccountTab = (typeof ACCOUNT_TABS)[number];

@customElement("account-page")
export class AccountPage extends LitElement {
  // No stacked @state — @consume requests updates itself.
  // Optional: outside a <session-provider> the field stays undefined, so
  // the `!this.session` guards below are reachable and required.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** The account page's sections as tabs: profile (identity), favorites,
   * notifications (the install control + the push settings), devices (the
   * session list), security (password change). Internal state — the
   * sections are panels of ONE page, not routes (the favorites pager's URL
   * params already carry the nested posts/projects kind).
   *
   * Deep links carrying the favorites `tab` param open on the favorites
   * panel (the pager's URLs always include it) — read at construction,
   * before the first render. */
  @state() private _activeTab: AccountTab = new URLSearchParams(
    window.location.search,
  ).has("tab")
    ? "favorites"
    : "profile";

  /** The page-shared action banner and in-flight flag (the devices and
   * security panels). The section controllers read and write them through
   * the `actionError`/`pending` accessors. */
  @state() private _error = "";
  @state() private _pending = false;

  /** Set after a successful password change — keeps the guard from
   *  redirecting so the success view can render. */
  @state() private _passwordChanged = false;

  /** Tracks whether we already redirected to prevent loops. */
  private _redirected = false;

  // ── Section controllers ────────────────────────────────────────

  private readonly profile: AccountProfileSection = new AccountProfileSection(
    this,
  );
  private readonly favorites: AccountFavoritesSection =
    new AccountFavoritesSection(this, {
      isActive: () => this._activeTab === "favorites",
    });
  private readonly sessions: AccountSessionsSection =
    new AccountSessionsSection(this);
  private readonly security: AccountSecuritySection =
    new AccountSecuritySection(this);

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(formStyles),
    unsafeCSS(authFormStyles),
    unsafeCSS(styles),
  ];

  // ── Host accessors for the section controllers ─────────────────

  /** True while the session state machine reports an authenticated session. */
  get authed(): boolean {
    return this.session?.state.status === "authenticated";
  }

  /** The session context the sections act on (sign-out, changePassword). */
  get sessionContext(): SessionContext | undefined {
    return this.session;
  }

  get pending(): boolean {
    return this._pending;
  }
  set pending(value: boolean) {
    this._pending = value;
  }

  get actionError(): string {
    return this._error;
  }
  set actionError(value: string) {
    this._error = value;
  }

  /** The security section's success handoff — keeps the guard from
   *  redirecting so the success view can render. */
  markPasswordChanged() {
    this._passwordChanged = true;
  }

  /** Route a page action's failure through the security section's shared
   *  submit mapping (the sign-out actions use it). */
  applySubmitError(err: unknown) {
    this.security.applySubmitError(err);
  }

  // ── Lifecycle + guard ──────────────────────────────────────────

  connectedCallback() {
    super.connectedCallback();
    this._guard();
  }

  updated(changed: PropertyValues) {
    super.updated(changed);
    this._guard();
  }

  /**
   * Guard: anonymous visitors must not see the account page — redirect to
   * /sign-in with replaceState (Back must not ping-pong). Exceptions:
   * mid-request (pending — the sign-out/change flows flip the session to
   * anonymous before they settle) and a successful password change
   * (_passwordChanged — the success view replaces the redirect).
   */
  private _guard() {
    if (this._redirected) return;
    if (
      this.session?.state.status === "anonymous" &&
      !this.pending &&
      !this._passwordChanged
    ) {
      this._redirected = true;
      redirect("/sign-in");
    }
  }

  /** Switch the account section tab (internal state — one page, five
   * panels). Entering the favorites tab re-syncs its pager (the load is
   * gated on the active tab, so the mount-time sync skipped it). The
   * shared submit/sign-out error banner is cleared — an error belongs to
   * the panel that produced it, never a later one.
   *
   * Focus target: a direct activation hands focus to the activated panel
   * (it is labelled by its tab, so the change is announced there). The
   * keyboard path keeps focus on the tab instead — automatic activation,
   * the APG Tabs contract: arrows move focus and selection together, and
   * the tablist stays a single tab stop. */
  private async _onTabSwitch(
    tab: AccountTab,
    focusTarget: "panel" | "tab" = "panel",
  ) {
    if (this._activeTab === tab) return;
    this._activeTab = tab;
    this.actionError = "";
    if (tab === "favorites") {
      this.favorites.sync();
    }
    await this.updateComplete;
    const target =
      focusTarget === "tab" ? `#account-tab-${tab}` : `#account-panel-${tab}`;
    this.shadowRoot?.querySelector<HTMLElement>(target)?.focus();
  }

  /** The tablist's keyboard contract (APG Tabs, automatic activation):
   * Left/Right walk the tabs and wrap, Home/End jump to the ends, and the
   * tab that receives focus becomes the selected one. */
  private _onTabsKeydown = (e: KeyboardEvent) => {
    const current = ACCOUNT_TABS.indexOf(this._activeTab);
    let next: number;
    switch (e.key) {
      case "ArrowRight":
        next = (current + 1) % ACCOUNT_TABS.length;
        break;
      case "ArrowLeft":
        next = (current + ACCOUNT_TABS.length - 1) % ACCOUNT_TABS.length;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = ACCOUNT_TABS.length - 1;
        break;
      default:
        return;
    }
    const tab = ACCOUNT_TABS[next];
    if (!tab) return;
    e.preventDefault();
    void this._onTabSwitch(tab, "tab");
  };

  // ── Render ─────────────────────────────────────────────────────

  render() {
    // Bootstrap not yet complete — show spinner.
    if (this.session?.state.status === "initializing") {
      return html`
        <h1 class="sr-only">${el.ui.account}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Bootstrap failed — show error with retry.
    if (this.session && this.session.state.status === "error") {
      // Local captures: property narrowing does not flow into the template
      // closures below.
      const session = this.session;
      const message = this.session.state.message;
      return html`
        <section
          class="auth-card account-page"
          aria-labelledby="account-heading"
        >
          <h1 id="account-heading">${el.ui.account}</h1>
          <error-banner .message=${message}></error-banner>
          <button
            type="button"
            class="button button--primary retry-btn"
            @click=${() => session.retryBootstrap()}
          >
            ${el.ui.retry}
          </button>
        </section>
      `;
    }

    // Password change succeeded — all sessions were revoked; the user must
    // sign in again. The guard skips its redirect here (_passwordChanged).
    if (this._passwordChanged) {
      return html`
        <section
          class="auth-card account-page"
          aria-labelledby="account-heading"
        >
          <h1 id="account-heading">${el.ui.account}</h1>
          <p class="success" role="status" tabindex="-1">
            ${el.auth.passwordChanged}
          </p>
          <p class="footer-link"><a href="/sign-in">${el.ui.signIn}</a></p>
        </section>
      `;
    }

    // Anonymous without a password change → the guard redirects; render a
    // neutral spinner for the frame before redirect() takes effect.
    if (!this.session || this.session.state.status !== "authenticated") {
      return html`
        <h1 class="sr-only">${el.ui.account}</h1>
        <loading-spinner></loading-spinner>
      `;
    }

    // Authenticated — the account view.
    const mustChange = this.session.state.mustChangePassword;
    return html`
      <section class="auth-card account-page" aria-labelledby="account-heading">
        <h1 id="account-heading">${el.ui.account}</h1>

        ${
          mustChange
            ? html`<p class="forced-change-notice" role="alert">
                ${el.auth.forcedChangeNotice}
              </p>`
            : ""
        }

        <div
          class="account-tabs"
          role="tablist"
          aria-label=${el.account.tabsLabel}
          @keydown=${this._onTabsKeydown}
        >
          <!-- Only the active panel mounts, so the tabs carry no
               aria-controls: a dangling IDREF is an axe failure. -->
          <button
            type="button"
            role="tab"
            id="account-tab-profile"
            class="account-tab"
            aria-selected=${this._activeTab === "profile"}
            tabindex=${this._activeTab === "profile" ? "0" : "-1"}
            @click=${() => this._onTabSwitch("profile")}
          >
            ${el.account.profileTitle}
          </button>
          <button
            type="button"
            role="tab"
            id="account-tab-favorites"
            class="account-tab"
            aria-selected=${this._activeTab === "favorites"}
            tabindex=${this._activeTab === "favorites" ? "0" : "-1"}
            @click=${() => this._onTabSwitch("favorites")}
          >
            ${el.favorites.sectionTitle}
          </button>
          <button
            type="button"
            role="tab"
            id="account-tab-notifications"
            class="account-tab"
            aria-selected=${this._activeTab === "notifications"}
            tabindex=${this._activeTab === "notifications" ? "0" : "-1"}
            @click=${() => this._onTabSwitch("notifications")}
          >
            ${el.account.notificationsTab} (<span lang="en"
              >${el.account.notificationsTabPush}</span
            >)
          </button>
          <button
            type="button"
            role="tab"
            id="account-tab-devices"
            class="account-tab"
            aria-selected=${this._activeTab === "devices"}
            tabindex=${this._activeTab === "devices" ? "0" : "-1"}
            @click=${() => this._onTabSwitch("devices")}
          >
            ${el.ui.sessions}
          </button>
          <button
            type="button"
            role="tab"
            id="account-tab-security"
            class="account-tab"
            aria-selected=${this._activeTab === "security"}
            tabindex=${this._activeTab === "security" ? "0" : "-1"}
            @click=${() => this._onTabSwitch("security")}
          >
            ${el.account.securityTab}
          </button>
        </div>

        ${
          this._activeTab === "profile"
            ? html`
                <div
                  id="account-panel-profile"
                  role="tabpanel"
                  aria-labelledby="account-tab-profile"
                  tabindex="-1"
                >
                  <h2 id="profile-heading">${el.account.profileTitle}</h2>
                  ${this.profile.template()}
                </div>
              `
            : ""
        }
        ${
          this._activeTab === "notifications"
            ? html`
                <div
                  id="account-panel-notifications"
                  role="tabpanel"
                  aria-labelledby="account-tab-notifications"
                  tabindex="-1"
                >
                  <!-- Install control: the same
                       affordance as the home strip, for signed-in users who
                       never land on the homepage. It sits with the
                       push settings — installing the app
                       and allowing notifications are one story. Renders
                       nothing when the app is already installed or this
                       browser already allows notifications. -->
                  <install-control></install-control>

                  <!-- Web push settings: the device
                       notification opt-in + per-kind toggles. The component
                       owns the whole surface — permission flow, subscription
                       state, toggles — and renders nothing for anonymous
                       sessions. A panel SIBLING of the install control. -->
                  <push-settings></push-settings>
                </div>
              `
            : ""
        }
        ${
          this._activeTab === "devices"
            ? html`
                <div
                  id="account-panel-devices"
                  role="tabpanel"
                  aria-labelledby="account-tab-devices"
                  tabindex="-1"
                >
                  <h2 id="sessions-heading">${el.ui.sessions}</h2>
                  ${this.sessions.template()}
                </div>
              `
            : ""
        }
        ${
          this._activeTab === "favorites"
            ? html`
                <div
                  id="account-panel-favorites"
                  role="tabpanel"
                  aria-labelledby="account-tab-favorites"
                  tabindex="-1"
                >
                  <h2 id="favorites-heading">${el.favorites.sectionTitle}</h2>
                  ${this.favorites.template()}
                </div>
              `
            : ""
        }
        ${
          this._activeTab === "security"
            ? html`
                <div
                  id="account-panel-security"
                  role="tabpanel"
                  aria-labelledby="account-tab-security"
                  tabindex="-1"
                >
                  <h2>${el.auth.changePassword}</h2>
                  ${this.security.template()}
                </div>
              `
            : ""
        }
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "account-page": AccountPage;
  }
}
