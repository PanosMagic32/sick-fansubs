import { html, LitElement, PropertyValues, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";
import { consume } from "@lit/context";
import type { Routes } from "@lit-labs/router";

import type { Theme } from "@core/shell/types.js";
import {
  NAVIGATE_EVENT,
  createRoutes,
  redirect,
  type NavigateEventDetail,
} from "@core/router.js";
import {
  sessionContext,
  type SessionContext,
} from "@features/auth/data-access/session.js";
import { el } from "@shared/catalog/el.js";

import "./donation-banner.js";
import "./offline-banner.js";
import sharedStyles from "@core/styles/baseline.css?inline";
import controlStyles from "@shared/ui/controls.css?inline";
import styles from "./app-shell.css?inline";

const THEME_KEY = "sf-theme";
const REDUCED_MOTION = "(prefers-reduced-motion: reduce)";

/**
 * Split a navigation path into pathname + search + hash. Query-string
 * navigation (e.g. /search?q=…&after=…) is handled by _onNavigate: the
 * routes controller matches PATHNAMES only.
 */
function splitPath(path: string): {
  pathname: string;
  search: string;
  hash: string;
} {
  const hashIndex = path.indexOf("#");
  const noHash = hashIndex === -1 ? path : path.slice(0, hashIndex);
  const hash = hashIndex === -1 ? "" : path.slice(hashIndex);
  const searchIndex = noHash.indexOf("?");
  const pathname = searchIndex === -1 ? noHash : noHash.slice(0, searchIndex);
  const search = searchIndex === -1 ? "" : noHash.slice(searchIndex);
  return { pathname, search, hash };
}

/**
 * Application shell — full-page frame with interactive dot-grid
 * background, header, router outlet, and footer. Owns the
 * theme state (persisted to localStorage), mouse-tracking for
 * the dot glow effect, and SPA navigation.
 *
 * Why the shell owns navigation:
 * @lit-labs/router's Router class has a lifecycle incompatibility
 * with Lit — its window click/popstate listeners are never registered.
 * We use Routes (matching/rendering only) and handle click interception,
 * popstate, and programmatic navigation here. See core/router.ts.
 */
/** The management surfaces' path shapes — their pages share one title. */
const ADMIN_PATH =
  /^\/admin$|^\/admin\/(?:users|metrics|logs)$|^\/admin\/blog\/(?:new|[^/]+)$|^\/admin\/projects(?:\/(?:new|[^/]+))?$/;

@customElement("app-shell")
export class AppShell extends LitElement {
  @state() private theme: Theme = "dark";
  @state() private currentPath = "/";

  // The session context feeds the forced-change route:
  // when bootstrap lands on a flagged session, the shell routes to the
  // change-password form proactively. The field is honestly optional —
  // outside a <session-provider> it stays undefined.
  @consume({ context: sessionContext, subscribe: true })
  private session?: SessionContext;

  private _rafId = 0;
  private _routes!: Routes;

  /** Latches the one-shot forced-change redirect (see _maybeForceChangeRoute). */
  private _forcedRedirected = false;

  static styles = [
    unsafeCSS(sharedStyles),
    unsafeCSS(controlStyles),
    unsafeCSS(styles),
  ];

  connectedCallback() {
    super.connectedCallback();
    this._loadTheme();

    // Create the routes controller and perform the initial route match.
    this._routes = createRoutes(this);
    this._routes.goto(window.location.pathname);

    // Sync the current path so the header can highlight the active link.
    this.currentPath = window.location.pathname;

    // Initial load: set the page title from the URL (focus stays put).
    this._setTitle(this.currentPath);

    window.addEventListener("click", this._onLinkClick);
    window.addEventListener("popstate", this._onPopState);
    window.addEventListener(NAVIGATE_EVENT, this._onNavigate);

    if (!window.matchMedia(REDUCED_MOTION).matches) {
      this.addEventListener("mousemove", this._onMouseMove);
    }
  }

  disconnectedCallback() {
    window.removeEventListener("click", this._onLinkClick);
    window.removeEventListener("popstate", this._onPopState);
    window.removeEventListener(NAVIGATE_EVENT, this._onNavigate);
    this.removeEventListener("mousemove", this._onMouseMove);
    cancelAnimationFrame(this._rafId);
    // The callback is what normally zeroes the handle; with the frame
    // cancelled it never runs, so reset it here or the next connect's
    // `if (this._rafId) return` would disable the glow forever.
    this._rafId = 0;
    super.disconnectedCallback();
  }

  updated(changed: PropertyValues) {
    super.updated(changed);
    if (changed.has("theme")) {
      this.classList.toggle("light", this.theme === "light");
    }
    this._maybeForceChangeRoute();
  }

  /**
   * Proactive forced-change routing: when the session
   * state lands on authenticated + mustChangePassword, navigate to /account
   * (the change-password form) once. The latch survives revalidate's re-set
   * of an identical state object (tab-focus must not yank the user back
   * while they read public content); it resets on anonymous so a later
   * re-flag routes again.
   */
  private _maybeForceChangeRoute() {
    const s = this.session?.state;
    if (!s) return;
    if (s.status === "anonymous") {
      this._forcedRedirected = false;
      return;
    }
    if (
      s.status === "authenticated" &&
      s.mustChangePassword &&
      !this._forcedRedirected
    ) {
      this._forcedRedirected = true;
      // /auth/reset is EXEMPT: a self-chosen password
      // is a legitimate exit from the forced-change gate — yanking the
      // user to /account would strand the emailed reset link. /auth/verify
      // is EXEMPT the same way: the emailed verification link is
      // the same self-service shape.
      if (
        window.location.pathname !== "/account" &&
        window.location.pathname !== "/auth/reset" &&
        window.location.pathname !== "/auth/verify"
      ) {
        redirect("/account");
      }
    }
  }

  /* ── Theme persistence ── */

  private _loadTheme() {
    try {
      const saved = localStorage.getItem(THEME_KEY);
      this.theme =
        saved === "dark" || saved === "light"
          ? saved
          : this._systemPrefersLight()
            ? "light"
            : "dark";
    } catch {
      this.theme = this._systemPrefersLight() ? "light" : "dark";
    }
  }

  private _systemPrefersLight(): boolean {
    return window.matchMedia("(prefers-color-scheme: light)").matches;
  }

  private _onThemeToggle = () => {
    const next: Theme = this.theme === "dark" ? "light" : "dark";
    this.theme = next;
    try {
      localStorage.setItem(THEME_KEY, next);
    } catch {
      /* storage unavailable — theme persists for this session */
    }
  };

  /**
   * Set document.title from the catalog for the given pathname.
   * SPA navigation does not reload the document, so the title must be
   * managed explicitly (a11y: screen readers announce the new page).
   */
  private _setTitle(pathname: string) {
    // Detail paths get the base post/project title and refine it once the
    // content loads; only the exact one-segment shape matches the route.
    const detail = pathname.match(/^\/(blog|projects)\/[^/]+$/);
    if (detail) {
      const base =
        detail[1] === "blog" ? el.titles.blogDetail : el.titles.projectsDetail;
      document.title = `${base} — ${el.ui.siteName}`;
      return;
    }
    document.title = `${this._titleForPath(pathname)} — ${el.ui.siteName}`;
  }

  /** Static-title lookup; dynamic shapes are refined in _setTitle. */
  private _titleForPath(pathname: string): string {
    if (ADMIN_PATH.test(pathname)) return el.titles.admin;
    const titles: Record<string, string> = {
      "/": el.titles.home,
      "/projects": el.titles.projects,
      "/about": el.titles.about,
      "/search": el.titles.search,
      "/sign-in": el.titles.signIn,
      "/register": el.titles.register,
      "/auth/forgot": el.titles.forgot,
      "/auth/reset": el.titles.reset,
      "/auth/verify": el.auth.verifyTitle,
      "/notifications": el.titles.notifications,
      "/account": el.titles.account,
    };
    return titles[pathname] ?? el.titles.notFound;
  }

  /**
   * Move focus to the main region after an SPA navigation. Replacing the
   * route content leaves focus on the (possibly removed) link; screen
   * readers otherwise get no page-change announcement. Skipped on initial
   * load (connectedCallback) — the document starts at the top.
   */
  private _focusMain() {
    this.shadowRoot?.querySelector("main")?.focus();
  }

  /* ── SPA navigation ── */

  /**
   * Intercept same-origin <a> clicks for SPA navigation.
   * Prevents full-page reloads and renders via the routes controller.
   */
  private _onLinkClick = (e: MouseEvent) => {
    // Ignore modified clicks (ctrl+click, middle-click, etc.).
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey)
      return;
    if (e.defaultPrevented) return;

    // Find the <a> element in the event path (works through shadow DOM).
    const anchor = e
      .composedPath()
      .find((el) => el instanceof HTMLAnchorElement) as
      HTMLAnchorElement | undefined;
    if (!anchor) return;

    // Let external links, downloads, and new-tab links through.
    if (anchor.target || anchor.hasAttribute("download")) return;
    if (anchor.rel === "external") return;

    // Read the raw href attribute (not .href) so relative and fragment
    // forms stay intact, then resolve it against the document base — this
    // classifies every shape (absolute, protocol-relative, rooted,
    // relative, query-only) and keeps non-http(s) schemes native: a
    // magnet: link must open the user's torrent handler, never land in
    // pushState.
    const rawHref = anchor.getAttribute("href") ?? "";
    if (!rawHref) return;

    // Fragment-only links (skip-to-content, in-page anchors) stay native
    // so the browser performs fragment scrolling and focus movement.
    if (rawHref.startsWith("#")) return;

    let url: URL;
    try {
      url = new URL(rawHref, document.baseURI);
    } catch {
      return; // unparseable — let the browser handle it
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") return;
    if (url.origin !== window.location.origin) return;

    // Same-pathname links stay native: only the query/hash differs, and a
    // native reload is the established behavior for those.
    if (url.pathname === window.location.pathname) return;

    // SPA navigation: no full-page reload.
    e.preventDefault();
    const path = url.pathname + url.search + url.hash;
    window.history.pushState({}, "", path);
    this._routes.goto(url.pathname);
    this.currentPath = url.pathname;
    this._setTitle(url.pathname);
    this._focusMain();
  };

  /** Back/forward navigation — re-render the matched route. */
  private _onPopState = () => {
    this._routes.goto(window.location.pathname);
    this.currentPath = window.location.pathname;
    this._setTitle(this.currentPath);
    this._focusMain();
  };

  /** Programmatic navigation via navigate() from core/router.ts. */
  private _onNavigate = (e: Event) => {
    const { path } = (e as CustomEvent<NavigateEventDetail>).detail;
    // path may carry a query string (e.g. /search?q=…): the routes
    // controller matches PATHNAMES, so split via the shared helper.
    // currentPath/title use window.location, which navigate() already
    // updated.
    this._routes.goto(splitPath(path).pathname);
    this.currentPath = window.location.pathname;
    this._setTitle(this.currentPath);
    this._focusMain();
  };

  /* ── Mouse tracking (rAF-throttled) ── */

  private _onMouseMove = (e: MouseEvent) => {
    if (this._rafId) return;
    this._rafId = requestAnimationFrame(() => {
      this._rafId = 0;
      const x = (e.clientX / window.innerWidth) * 100;
      const y = (e.clientY / window.innerHeight) * 100;
      if (!Number.isFinite(x) || !Number.isFinite(y)) return;
      this.style.setProperty("--mx", `${x}%`);
      this.style.setProperty("--my", `${y}%`);
    });
  };

  /* ── Render ── */

  render() {
    return html`
      <a href="#main-content" class="skip-link">${el.ui.skipToContent}</a>

      <div class="bg" aria-hidden="true">
        <div class="bg-dots"></div>
        <div class="bg-glow"></div>
      </div>

      <app-header
        .theme=${this.theme}
        .currentPath=${this.currentPath}
        @toggle-theme=${this._onThemeToggle}
      ></app-header>

      <!-- Offline banner: every route, right under the header; the element
           hides itself while online. -->
      <offline-banner></offline-banner>

      <!-- Donation banner: HOME route only — it earns its keep on the
           landing page; the footer support line covers every page
           discreetly. -->
      ${
        this.currentPath === "/"
          ? html`<donation-banner></donation-banner>`
          : ""
      }

      <main id="main-content" tabindex="-1">${this._routes?.outlet()}</main>

      <app-footer></app-footer>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "app-shell": AppShell;
  }
}
