import { consume } from "@lit/context";
import { html, LitElement, nothing, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { fetchCurrentUserProfile } from "@features/auth/data-access/profile-api.js";
import { unreadNotificationCount } from "@features/notifications/data-access/notifications-api.js";
import { NOTIFICATIONS_CHANGED_EVENT } from "@features/notifications/notifications-page.js";
import { AVATAR_CHANGED_EVENT } from "@shared/events.js";
import { NAVIGATE_EVENT } from "@core/router.js";
import type { Theme } from "@core/shell/types.js";
import { ApiError } from "@shared/api/client.js";
import { requestSignal } from "@shared/api/request-signal.js";
import { el, unreadBadgeLabel, unreadBadgeText } from "@shared/catalog/el.js";
import { TRACKER_URL } from "@shared/config/links.js";
import { avatarTemplate } from "@shared/ui/avatar.js";
import avatarStyles from "@shared/ui/avatar.css?inline";
import { icons } from "@shared/ui/icons.js";
import { mapError } from "@shared/utils/format.js";
import { isStaffRole } from "@shared/utils/roles.js";

import styles from "./app-header.css?inline";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";

/**
 * Application header — brand, navigation, theme toggle, the notifications
 * bell, and the icon-only account link.
 * On mobile, the navigation collapses into a hamburger menu.
 * The theme toggle emits a "toggle-theme" event handled by <app-shell>.
 *
 * The account link is session-aware: it points to
 * /sign-in while anonymous (or while the bootstrap is still in flight)
 * and to /account once authenticated. It is ICON-ONLY at every width: the user's avatar (or first letter) when authenticated,
 * the default user icon otherwise — the accessible name stays via the
 * clipped label.
 *
 * Signed in, the account chip is the disclosure button for the account menu
 * — the account link, sign out, and sign-out-all; signed out it stays the
 * plain link to /sign-in.
 *
 * The notifications bell (authenticated):
 * rendered for every signed-in session with an
 * unread badge. The badge refreshes on session bootstrap + navigation +
 * the page's read-mutation event — never a timer.
 */
@customElement("app-header")
export class AppHeader extends LitElement {
  @property() theme: Theme = "dark";
  @state() navOpen = false;
  /** Set by the router; used for aria-current and active pill styling. */
  @property() currentPath = "/";

  /**
   * Session state for the account link. `subscribe: true` is REQUIRED:
   * without it, @consume delivers the context value only once, and a
   * header mounted before bootstrap settles would never flip.
   */
  // No stacked @state — @consume requests updates itself.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  /** Unread badge count; null when not fetched (anonymous/error/no staff). */
  @state() private _unread: number | null = null;

  /** The authenticated user's avatar URL; null = initial-letter fallback. */
  @state() private _avatarUrl: string | null = null;

  private _unreadAbort: AbortController | null = null;
  private _profileAbort: AbortController | null = null;

  /** Session-change key for the badge/avatar refresh triggers. */
  private _sessionKey = "none";

  /** True once a currentPath update has been observed (the mount-time
   * update is not a navigation — see updated()). */
  private _pathSeen = false;

  /** The signed-in account menu (the account chip): its open state, an
   * in-flight sign-out, and the sign-out failure line. */
  @state() private _menuOpen = false;
  @state() private _menuPending = false;
  @state() private _menuError = "";

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(avatarStyles),
    unsafeCSS(styles),
  ];

  connectedCallback() {
    super.connectedCallback();
    this.setAttribute("role", "banner");
    this.addEventListener("keydown", this._onKeyDown);
    // Two listeners: the document one sees outside clicks, the shadow one
    // sees the real inner target. Focus containment stays deferred.
    document.addEventListener("click", this._onDocumentClick);
    // Read mutations on /notifications refetch the badge (event-driven).
    window.addEventListener(
      NOTIFICATIONS_CHANGED_EVENT,
      this._onNotificationsChanged,
    );
    window.addEventListener(NAVIGATE_EVENT, this._onNavigate);
    window.addEventListener("popstate", this._onNavigate);
    // A confirmed avatar upload refetches the profile chip.
    window.addEventListener(AVATAR_CHANGED_EVENT, this._onAvatarChanged);
    // Re-attach the shadow listener after a reconnect.
    if (this.hasUpdated) {
      this.renderRoot.addEventListener("click", this._onShadowClick);
    }
  }

  disconnectedCallback() {
    this.removeEventListener("keydown", this._onKeyDown);
    (this.renderRoot as ShadowRoot | undefined)?.removeEventListener(
      "click",
      this._onShadowClick,
    );
    document.removeEventListener("click", this._onDocumentClick);
    window.removeEventListener(
      NOTIFICATIONS_CHANGED_EVENT,
      this._onNotificationsChanged,
    );
    window.removeEventListener(AVATAR_CHANGED_EVENT, this._onAvatarChanged);
    window.removeEventListener(NAVIGATE_EVENT, this._onNavigate);
    window.removeEventListener("popstate", this._onNavigate);
    this._unreadAbort?.abort();
    this._profileAbort?.abort();
    super.disconnectedCallback();
  }

  protected override firstUpdated() {
    // The inner listener needs the shadow root.
    this.renderRoot.addEventListener("click", this._onShadowClick);
  }

  protected override updated(changed: PropertyValues<this>) {
    // Badge refresh on navigation and session changes.
    if (changed.has("currentPath")) {
      // Every navigation closes the mobile dropdown, however it started
      // (a link, the brand, an in-page control, back/forward). The
      // outside-click listener cannot catch it — the brand IS inside this
      // header — so the menu follows the path instead.
      //
      // The FIRST update is not a navigation (Lit reports every property
      // as changed on mount), so do not close a menu just asked to show.
      if (this._pathSeen) {
        this.navOpen = false;
        this._menuOpen = false;
      }
      this._pathSeen = true;
    }
    const s = this.session?.state;
    const key =
      s?.status === "authenticated"
        ? `auth:${s.user.username}`
        : (s?.status ?? "none");
    if (key !== this._sessionKey) {
      this._sessionKey = key;
      if (s?.status !== "authenticated") {
        this._avatarUrl = null;
        // A session flip closes and clears the menu.
        this._menuOpen = false;
        this._menuError = "";
      }
      void this._refreshUnread();
      void this._refreshAvatar();
    }
  }

  private _toggleTheme() {
    this.dispatchEvent(new CustomEvent("toggle-theme", { bubbles: true }));
  }

  private _toggleNav = () => {
    this.navOpen = !this.navOpen;
    if (this.navOpen) {
      this._menuOpen = false;
      // Focus the first nav link after the DOM updates
      requestAnimationFrame(() => {
        const first = this.shadowRoot?.querySelector(
          ".header-center a",
        ) as HTMLElement | null;
        first?.focus();
      });
    }
  };

  private _closeNav() {
    this.navOpen = false;
    (
      this.shadowRoot?.querySelector(".hamburger") as HTMLElement | null
    )?.focus();
  }

  private _toggleAccountMenu = () => {
    this._menuOpen = !this._menuOpen;
    if (this._menuOpen) {
      this.navOpen = false;
      this._menuError = "";
      requestAnimationFrame(() => {
        const first = this.shadowRoot?.querySelector(
          ".account-menu a, .account-menu button",
        ) as HTMLElement | null;
        first?.focus();
      });
    }
  };

  private _closeAccountMenu() {
    if (!this._menuOpen) return;
    this._menuOpen = false;
    (
      this.shadowRoot?.querySelector(
        ".account-menu-button",
      ) as HTMLElement | null
    )?.focus();
  }

  /* ── Account menu sign-out ── */

  private _onSignOut = () => {
    void this._runSignOut("signOut");
  };

  private _onSignOutAll = () => {
    void this._runSignOut("signOutAll");
  };

  /**
   * Run one of the session's sign-out actions from the account menu. The
   * provider flips the session to anonymous on success — a protected
   * route's guard redirects, a public page keeps its position with a
   * signed-out header. A 403 means the in-memory CSRF token went stale (a
   * cross-tab re-sign-in replaced the session): revalidate, the account
   * page's heal.
   */
  private async _runSignOut(kind: "signOut" | "signOutAll") {
    if (this._menuPending) return;
    const session = this.session;
    if (!session) return;
    this._menuPending = true;
    this._menuError = "";
    const pathBefore = this.currentPath;
    try {
      await session[kind]();
      this._menuOpen = false;
      // A success that keeps the route closes the panel around the focused
      // control; wait for the re-render, then hand focus back to the chip
      // if it was lost (a route change owns focus itself).
      await this.updateComplete;
      if (this.currentPath === pathBefore && !this.shadowRoot?.activeElement) {
        this.shadowRoot?.querySelector<HTMLElement>(".account-link")?.focus();
      }
    } catch (err) {
      this._menuError = mapError(err);
      if (err instanceof ApiError && err.status === 403) {
        void session.revalidate();
      }
    } finally {
      this._menuPending = false;
    }
  }

  private _onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "Escape") return;
    if (this.navOpen) {
      this._closeNav();
    } else if (this._menuOpen) {
      this._closeAccountMenu();
    }
  };

  private _onDocumentClick = (e: MouseEvent) => {
    if (!this.navOpen && !this._menuOpen) return;
    // Outside the header entirely. Clicks INSIDE it are the shadow
    // listener's business — at this level every inner target is retargeted
    // to the host, so "inside the panel" cannot be decided here.
    if (!e.composedPath().includes(this)) {
      this.navOpen = false;
      this._menuOpen = false;
    }
  };

  /** Clicks inside the header: a panel and its own toggle keep their
   * behaviour (panel links close themselves); every OTHER part of the
   * bar counts as outside and closes whichever panel is open. */
  private _onShadowClick = (e: Event) => {
    const target = e.target as Node | null;
    if (!target) return;
    if (this.navOpen) {
      const nav = this.shadowRoot?.querySelector(".header-center");
      const hamburger = this.shadowRoot?.querySelector(".hamburger");
      if (!nav?.contains(target) && !hamburger?.contains(target)) {
        this.navOpen = false;
      }
    }
    if (this._menuOpen) {
      const root = this.shadowRoot?.querySelector(".account-menu-root");
      if (!root?.contains(target)) {
        this._menuOpen = false;
      }
    }
  };

  private _isActive(path: string) {
    return this.currentPath === path || this.currentPath.startsWith(path + "/");
  }

  /* ── Notifications badge ── */

  private _onNotificationsChanged = () => {
    void this._refreshUnread();
  };

  /** Avatar upload confirmation → refetch the profile for the chip. */
  private _onAvatarChanged = () => {
    void this._refreshAvatar();
  };

  /** Every navigation closes both header panels, including a query-only
   * change the pathname-only currentPath branch cannot see. */
  private _onNavigate = () => {
    this.navOpen = false;
    this._menuOpen = false;
    void this._refreshUnread();
  };

  /**
   * Refetch the unread badge. Authenticated sessions only (the
   * bell is an authenticated surface now): anonymous sessions clear the
   * badge. Failures are silent — the bell just shows no badge (the feed
   * page carries its own error surface).
   */
  private async _refreshUnread() {
    // Abort BEFORE the auth check: a session flip must cancel the previous
    // account's in-flight read, or its late result can repaint the badge
    // after the state cleared it.
    this._unreadAbort?.abort();
    this._unreadAbort = null;
    const s = this.session?.state;
    if (s?.status !== "authenticated") {
      this._unread = null;
      return;
    }
    const controller = new AbortController();
    this._unreadAbort = controller;
    try {
      const result = await unreadNotificationCount({
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return;
      this._unread = result.unread;
    } catch {
      // Silent: badge freshness is best-effort.
    }
  }

  /* ── Account avatar ── */

  /**
   * Fetch the authenticated user's profile for the avatar URL. The
   * session context carries only the public projection (no avatarUrl),
   * so the header asks the profile endpoint once per
   * authentication; failures fall back to the initial-letter chip.
   */
  private async _refreshAvatar() {
    // Same ordering as the badge read: the previous account's request dies
    // before the early return, so it cannot paint a stale avatar.
    this._profileAbort?.abort();
    this._profileAbort = null;
    const s = this.session?.state;
    if (s?.status !== "authenticated") return;
    const controller = new AbortController();
    this._profileAbort = controller;
    try {
      const profile = await fetchCurrentUserProfile({
        signal: requestSignal(controller),
      });
      if (controller.signal.aborted) return;
      this._avatarUrl = profile.avatarUrl;
    } catch {
      // Fall back to the initial-letter chip; the account page still owns
      // its own profile error surface.
    }
  }

  /* ── Session-aware account link ── */

  /**
   * Account link target: /account when authenticated, /sign-in when
   * anonymous. Initializing and error are NOT anonymous states — the
   * session chip renders a status control instead of a sign-in link.
   */
  private get _accountHref(): string {
    return this.session?.state.status === "authenticated"
      ? "/account"
      : "/sign-in";
  }

  /**
   * Account link label: the username when authenticated (user data, not
   * catalog copy); the catalog's account label otherwise.
   */
  private get _accountLabel(): string {
    return this.session?.state.status === "authenticated"
      ? this.session.state.user.username
      : el.ui.account;
  }

  /** Whether the account link points at the current route. */
  private get _accountActive(): boolean {
    return this._isActive(this._accountHref);
  }

  /** True when the account link carries the username (authenticated). */
  private get _accountIsAuthenticated(): boolean {
    return this.session?.state.status === "authenticated";
  }

  /**
   * Initializing and error are connection states, not signed-out states:
   * the chip must not offer /sign-in while the bootstrap is unresolved or
   * failed. An absent context (standalone header) keeps the anonymous link.
   */
  private get _accountIsConnecting(): boolean {
    const status = this.session?.state.status;
    return status === "initializing" || status === "error";
  }

  private _onSessionRetry = () => {
    // The chip stays focusable while the retry is in flight; the click
    // guard replaces `disabled`, which would drop keyboard focus.
    if (this.session?.state.status !== "error") return;
    void this.session.retryBootstrap();
  };

  /** The persistent live region's text for a connection-state flip. */
  private _sessionStatusMessage(): string {
    const status = this.session?.state.status;
    if (status === "initializing") return el.ui.loading;
    if (status === "error") return el.ui.serverConnectionFailed;
    return "";
  }

  /** Staff visibility for the admin link (moderator+). */
  private get _isStaff(): boolean {
    if (this.session?.state.status !== "authenticated") return false;
    return isStaffRole(this.session.state.user.role);
  }

  /**
   * The signed-in account chip is the disclosure button for the account
   * menu: the panel holds the account link, sign out, and sign-out-all.
   * Signed out the chip stays the plain /sign-in link.
   */
  private _accountMenuTemplate() {
    return html`
      <div class="account-menu-root">
        <button
          type="button"
          class="account-link account-menu-button"
          aria-expanded=${this._menuOpen}
          @click=${this._toggleAccountMenu}
        >
          ${this._accountIconTemplate()}
          <span class="link-text">${this._accountLabel}</span>
          <span class="sr-only">, ${el.ui.account}</span>
        </button>
        ${
          this._menuOpen
            ? html`<div id="account-menu" class="account-menu">
                <a
                  class="button button--secondary account-menu-item"
                  href="/account"
                  aria-current=${this._accountActive ? "page" : nothing}
                  @click=${this._closeAccountMenu}
                  >${el.ui.account}</a
                >
                <button
                  type="button"
                  class="button button--secondary account-menu-item"
                  ?disabled=${this._menuPending}
                  @click=${this._onSignOut}
                >
                  ${el.ui.signOut}
                </button>
                <button
                  type="button"
                  class="button button--danger account-menu-item"
                  ?disabled=${this._menuPending}
                  @click=${this._onSignOutAll}
                >
                  ${el.auth.signOutAll}
                </button>
                ${
                  this._menuError
                    ? html`<p class="account-menu-error" role="alert">
                        ${this._menuError}
                      </p>`
                    : ""
                }
              </div>`
            : ""
        }
      </div>
    `;
  }

  /**
   * The connection-state chip: initializing renders a disabled loading
   * button; error renders a retry button. Both keep the account footprint
   * and never link to /sign-in — a deploy outage must not read as a logout.
   */
  private _sessionStatusTemplate() {
    const loading = this.session?.state.status === "initializing";
    const label = loading ? el.ui.loading : el.ui.retry;
    return html`<button
      type="button"
      class="account-link session-status"
      aria-disabled=${loading ? "true" : nothing}
      aria-busy=${loading ? "true" : nothing}
      aria-label=${label}
      @click=${this._onSessionRetry}
    >
      ${this._accountIconTemplate()}
      <span class="link-text">${label}</span>
    </button>`;
  }

  /**
   * The icon-only account link's visual: the avatar image (or initial
   * letter) when authenticated, the default user icon otherwise. The
   * avatar chip is decorative — the clipped label keeps the accessible
   * name.
   */
  private _accountIconTemplate() {
    const s = this.session?.state;
    if (s?.status !== "authenticated") return html`${icons.user}`;
    return avatarTemplate({
      username: s.user.username,
      avatarUrl: this._avatarUrl,
    });
  }

  render() {
    return html`
      <div class="header-left">
        <a href="/" class="brand" @click=${this._closeNav}>
          <img src="/logo/logo.png" alt="" role="presentation" class="logo" />
          ${el.ui.siteName}
        </a>
      </div>

      <nav
        id="header-nav"
        class="header-center ${this.navOpen ? "open" : ""}"
        aria-label=${el.nav.mainNavigation}
      >
        <a
          href="/projects"
          aria-current=${this._isActive("/projects") ? "page" : nothing}
          @click=${this._closeNav}
          >${icons.grid}
          <span class="link-text"
            ><span lang="en">${el.nav.projects}</span></span
          ></a
        >
        <a
          href="/search"
          aria-current=${this._isActive("/search") ? "page" : nothing}
          @click=${this._closeNav}
          >${icons.search} <span class="link-text">${el.nav.search}</span></a
        >
        <!-- The account entry is NOT repeated here: the header's account
             chip is visible at every width, so a second row would list the
             same destination twice. The chip is the one entry — it also
             carries the avatar. -->
        <!-- The about link sits LAST among the internal links, right
             before the external divider/tracker. -->
        <a
          href="/about"
          aria-current=${this._isActive("/about") ? "page" : nothing}
          @click=${this._closeNav}
          >${icons.users} <span class="link-text">${el.nav.about}</span></a
        >
        <span class="nav-divider" aria-hidden="true"></span>
        <a
          href=${TRACKER_URL}
          target="_blank"
          rel="noopener noreferrer"
          @click=${this._closeNav}
          >${icons.tracker}
          <span class="link-text"
            ><span lang="en">${el.nav.tracker}</span>
            <span class="sr-only">${el.nav.opensInNewTab}</span></span
          >
          <span class="external-icon">${icons.external}</span></a
        >
        <!-- Staff-only content administration: rendered only for an
             authenticated moderator+ — client-side visibility only, the
             server enforces the real authorization. It sits AFTER the
             external tracker, behind its own divider, so it reads as a
             separate, discreet control. -->
        ${
          this._isStaff
            ? html`<span class="nav-divider" aria-hidden="true"></span
                ><a
                  href="/admin"
                  aria-current=${this._isActive("/admin") ? "page" : nothing}
                  @click=${this._closeNav}
                  >${icons.sliders}
                  <span class="link-text">${el.titles.admin}</span></a
                >`
            : nothing
        }
      </nav>

      <div class="header-right">
        <!-- Notifications bell (authenticated — the unified
             feed serves every signed-in user): lands on the /notifications
             inbox. The label is
             clipped at every width below the ultrawide tier (the
             account-link contract) and revealed above 1920px. The badge
             is pinned to the GLYPH (the .bell-icon wrapper), never the
             label. -->
        ${
          this._accountIsAuthenticated
            ? html`<a
                href="/notifications"
                class="bell-link"
                aria-label=${
                  this._unread
                    ? `${el.notifications.bellAria}, ${unreadBadgeLabel(this._unread)}`
                    : el.notifications.bellAria
                }
                aria-current=${
                  this._isActive("/notifications") ? "page" : nothing
                }
                ><span class="bell-icon" aria-hidden="true"
                  >${icons.bell}${
                    this._unread
                      ? html`<span class="unread-badge"
                          >${unreadBadgeText(this._unread)}</span
                        >`
                      : nothing
                  }</span
                ><span class="link-text">${el.notifications.title}</span></a
              >`
            : nothing
        }
        <span class="sr-only" role="status"
          >${this._sessionStatusMessage()}</span
        >
        ${
          this._accountIsAuthenticated
            ? this._accountMenuTemplate()
            : this._accountIsConnecting
              ? this._sessionStatusTemplate()
              : html`<a
                  href=${this._accountHref}
                  class="account-link"
                  aria-current=${this._accountActive ? "page" : nothing}
                  >${this._accountIconTemplate()}
                  <span class="link-text">${this._accountLabel}</span></a
                >`
        }
        <button
          type="button"
          class="button button--icon button--secondary theme-btn"
          @click=${this._toggleTheme}
          aria-label=${el.ui.toggleTheme}
        >
          ${this.theme === "dark" ? icons.sun : icons.moon}
        </button>
        <button
          type="button"
          class="button button--icon button--ghost hamburger"
          @click=${this._toggleNav}
          aria-label=${el.ui.menu}
          aria-expanded=${this.navOpen}
          aria-controls="header-nav"
        >
          <span aria-hidden="true">${this.navOpen ? "✕" : "☰"}</span>
        </button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "app-header": AppHeader;
  }
}
