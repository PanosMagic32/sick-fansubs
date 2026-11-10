/**
 * Application router — the route table and the Routes controller factory
 * for the app-shell.
 *
 * IMPORTANT — why Routes and not Router:
 * @lit-labs/router's Router class has a lifecycle incompatibility
 * with Lit: its class-field arrow functions are undefined when
 * hostConnected() runs synchronously during addController(), so its
 * window click/popstate listeners are silently never registered.
 * The app-shell therefore owns SPA navigation itself (click interception,
 * popstate handling) and uses Routes — which is Router minus the broken
 * global listeners — for matching, rendering, and goto().
 *
 * Verifiable in the installed package source: the @lit-labs/router package's
 * development/router.js — this._onClick/this._onPopState are class-field
 * arrow handlers, and hostConnected() registers them as window listeners.
 */

import { Routes } from "@lit-labs/router";
import type { PathRouteConfig } from "@lit-labs/router";
import { html } from "lit";
import type { ReactiveControllerHost } from "lit";

import { el } from "@shared/catalog/el.js";

// ── Route definitions ──────────────────────────────────────────────

/**
 * Application route table. Pages handle their own auth checks in
 * connectedCallback (e.g., sign-in-page redirects when already
 * authenticated) — the routes controller only matches and renders.
 */
export const routes: PathRouteConfig[] = [
  {
    path: "/",
    name: "home",
    // The public blog list. wide-route lifts the route root above the
    // 48rem base cap in app-shell.css for the card grid.
    render: () => html`<blog-list-page class="wide-route"></blog-list-page>`,
  },
  {
    // URLPattern named group — params.id is the opaque post id. The page
    // owns its not-found state (the API masks unpublished ids with a 404).
    // wide-route lets the detail image exceed the base cap.
    path: "/blog/:id",
    name: "blog-detail",
    render: (params) =>
      html`<blog-detail-page
        class="wide-route"
        .postId=${params.id ?? ""}
      ></blog-detail-page>`,
  },
  {
    // Card grid without a featured hero. wide-route lifts the root above
    // the base cap for the three-column grid.
    path: "/projects",
    name: "projects",
    render: () =>
      html`<projects-list-page class="wide-route"></projects-list-page>`,
  },
  {
    // ID-addressed exactly like blog posts; the page owns its not-found
    // state. wide-route matches the blog detail route.
    path: "/projects/:id",
    name: "projects-detail",
    render: (params) =>
      html`<project-detail-page
        class="wide-route"
        .projectId=${params.id ?? ""}
      ></project-detail-page>`,
  },
  {
    // Static page — no data, no states; keeps the base cap.
    path: "/about",
    name: "about",
    render: () => html`<about-page></about-page>`,
  },
  {
    // The page reads q/type/after from the URL and re-syncs on popstate and
    // NAVIGATE_EVENT, so navigation with ?q=… works from anywhere; it owns
    // the only search input. wide-route gives it the same content width as
    // the blog/projects lists.
    path: "/search",
    name: "search",
    render: () => html`<search-page class="wide-route"></search-page>`,
  },
  {
    // The auth surfaces own one short card and no page chrome, so they keep
    // the shell's vertical centering through .center-route — every other
    // route is top-aligned.
    path: "/sign-in",
    name: "sign-in",
    render: () => html`<sign-in-page class="center-route"></sign-in-page>`,
  },
  {
    path: "/register",
    name: "register",
    render: () => html`<register-page class="center-route"></register-page>`,
  },
  {
    // Identifier form, generic success copy; the page guards itself like
    // sign-in (authenticated → redirect).
    path: "/auth/forgot",
    name: "forgot",
    render: () =>
      html`<forgot-password-page class="center-route"></forgot-password-page>`,
  },
  {
    // Consumes the emailed ?token=. Deliberately NOT auth-guarded — the
    // link must work while a live session exists, and the page is exempt
    // from the forced-change redirect.
    path: "/auth/reset",
    name: "reset",
    render: () =>
      html`<reset-password-page class="center-route"></reset-password-page>`,
  },
  {
    // Consumes the emailed ?token= and auto-submits once the session
    // context settles. Same guard shape as /auth/reset: the link must work
    // from a live session and is exempt from the forced-change redirect.
    path: "/auth/verify",
    name: "verify",
    render: () =>
      html`<verify-email-page class="center-route"></verify-email-page>`,
  },
  {
    // First protected route; the page redirects anonymous visitors to
    // /sign-in. full-route lifts the base cap — the view fills the screen
    // at FHD and caps only at ultrawide.
    path: "/account",
    name: "account",
    render: () => html`<account-page class="full-route"></account-page>`,
  },
  {
    // The staff content list. The page guards itself (anonymous →
    // /sign-in; below moderator → forbidden). full-route fills the screen
    // at FHD.
    path: "/admin",
    name: "admin",
    render: () => html`<admin-page class="full-route"></admin-page>`,
  },
  {
    // Blog create form — admin+ only.
    path: "/admin/blog/new",
    name: "admin-blog-new",
    render: () => html`<blog-form-page></blog-form-page>`,
  },
  {
    // The create form element with postId selecting edit mode.
    path: "/admin/blog/:id",
    name: "admin-blog-edit",
    render: (params) =>
      html`<blog-form-page .postId=${params.id ?? ""}></blog-form-page>`,
  },
  {
    // The staff project list — a separate element from admin-page: the
    // shared AdminListBase holds the common surface, and the route
    // separation keeps each paging controller keyed to one fixed path.
    path: "/admin/projects",
    name: "admin-projects",
    render: () =>
      html`<admin-projects-page class="full-route"></admin-projects-page>`,
  },
  {
    // Project create form — admin+ only.
    path: "/admin/projects/new",
    name: "admin-projects-new",
    render: () => html`<project-form-page></project-form-page>`,
  },
  {
    // The create form element with projectId selecting edit mode.
    path: "/admin/projects/:id",
    name: "admin-projects-edit",
    render: (params) =>
      html`<project-form-page
        .projectId=${params.id ?? ""}
      ></project-form-page>`,
  },
  {
    // The staff user list with role/status filters — its own element, and
    // the users page owns two filter params that ride the pager URLs.
    path: "/admin/users",
    name: "admin-users",
    render: () =>
      html`<admin-users-page class="full-route"></admin-users-page>`,
  },
  {
    // Live totals + last-30-days activity; not a list, so no
    // AdminListBase — the page owns its guard and states.
    path: "/admin/metrics",
    name: "admin-metrics",
    render: () =>
      html`<admin-metrics-page class="full-route"></admin-metrics-page>`,
  },
  {
    // The super-admin log viewer: the page narrows the shared staff floor
    // to the super-admin predicate, and admin-tabs hides the tab from
    // every lower role.
    path: "/admin/logs",
    name: "admin-logs",
    render: () => html`<admin-logs-page class="full-route"></admin-logs-page>`,
  },
  {
    // The unified inbox the header bell lands on (any signed-in user; the
    // staff audit half stays role-weighted). The page guards itself — the
    // server enforces the real authorization regardless.
    path: "/notifications",
    name: "notifications",
    render: () =>
      html`<notifications-page class="full-route"></notifications-page>`,
  },
];

// ── Route matching ─────────────────────────────────────────────────

/**
 * Compile a route pattern into an exact pathname matcher. Route paths use
 * only static segments and `:name` placeholders, so the translation is a
 * segment-by-segment escape plus a one-segment wildcard for each param.
 */
function compileRoutePath(path: string): RegExp {
  const source = path
    .split("/")
    .map((segment) =>
      segment.startsWith(":")
        ? "[^/]+"
        : segment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"),
    )
    .join("/");
  return new RegExp(`^${source}$`);
}

const routePathMatchers = routes.map((route) => compileRoutePath(route.path));

/**
 * Normalize a pathname to the route table's canonical slash-free form. The
 * root path stays `/`; every deeper trailing slash is dropped.
 */
export function normalizeAppPathname(pathname: string): string {
  return pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
}

/**
 * Whether a pathname is an application route. The shell intercepts only
 * these: a same-origin link to a server-served resource (for example
 * `/media/images/...`) must reach the browser natively, not enter the SPA
 * router and render the fallback.
 */
export function isAppRoutePath(pathname: string): boolean {
  const normalized = normalizeAppPathname(pathname);
  return routePathMatchers.some((matcher) => matcher.test(normalized));
}

// ── Fallback (404) ─────────────────────────────────────────────────

// The fallback's copy comes from the catalog — the "404" heading is
// catalog copy, not a hardcoded glyph.
export const fallback = {
  render: () => html`
    <section class="home-placeholder center-route">
      <h1>${el.ui.notFoundTitle}</h1>
      <p>${el.ui.notFound}</p>
      <a href="/">${el.ui.home}</a>
    </section>
  `,
};

// ── Routes factory ─────────────────────────────────────────────────

/**
 * Create the application routes controller attached to the given host.
 * The host must be a LitElement. Navigation (click interception and
 * popstate) is owned by the app-shell — see the header comment.
 */
export function createRoutes(
  host: ReactiveControllerHost & HTMLElement,
): Routes {
  return new Routes(host, routes, { fallback });
}

// ── Programmatic navigation ────────────────────────────────────────

/** Custom event dispatched when navigate() is called. */
export const NAVIGATE_EVENT = "sf-navigate";

export interface NavigateEventDetail {
  path: string;
}

/**
 * Navigate to a path without a full page reload.
 *
 * Updates the history and notifies the app-shell (which listens for
 * NAVIGATE_EVENT) to render the matching route. Components that need
 * to redirect — e.g., the sign-in page after authentication — call
 * this instead of assigning window.location.href.
 *
 * The target is resolved against the current URL, its pathname normalized
 * (the router's trailing-slash canonical form), and the FULL current URL is
 * compared before writing. A query-only change from `/search?q=x` to
 * `/search` therefore pushes, and a registered route never keeps a
 * noncanonical history entry.
 */
export function navigate(path: string): void {
  navigateWith(path, "pushState");
}

/**
 * Redirect — same as navigate(), but REPLACES the current history entry
 * instead of pushing. Guard redirects (auth pages bouncing an authenticated
 * visitor) must use this: pushState would leave the guarded route in
 * history, and Back would ping-pong between the two routes forever.
 */
export function redirect(path: string): void {
  navigateWith(path, "replaceState");
}

/** One history-write path for both navigation helpers. */
function navigateWith(
  path: string,
  method: "pushState" | "replaceState",
): void {
  const target = new URL(path, navigationBase());
  target.pathname = normalizeAppPathname(target.pathname);
  const targetPath = target.pathname + target.search + target.hash;
  if (
    targetPath !==
    window.location.pathname + window.location.search + window.location.hash
  ) {
    window.history[method]({}, "", targetPath);
  }
  window.dispatchEvent(
    new CustomEvent<NavigateEventDetail>(NAVIGATE_EVENT, {
      detail: { path: targetPath },
    }),
  );
}

/** The current URL when it is absolute; happy-dom's about:blank fallback is
 * not a valid URL base, so tests and non-browser contexts get a loopback
 * origin. */
function navigationBase(): string {
  if (/^https?:/i.test(window.location.href)) return window.location.href;
  return `http://localhost${window.location.pathname}${window.location.search}${window.location.hash}`;
}
