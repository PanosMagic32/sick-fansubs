/**
 * Router helpers — redirect() vs navigate() history semantics.
 *
 * Guard redirects (auth pages bouncing authenticated visitors) must use
 * replaceState, not pushState: a pushed guarded route stays in history and
 * Back would ping-pong between the two routes forever.
 */

import { describe, expect, test, afterEach } from "bun:test";
import { render } from "lit";
import type { TemplateResult } from "lit";
import type { PathRouteConfig } from "@lit-labs/router";

import { el } from "@shared/catalog/el.js";
import {
  listenForNavigation,
  resetHappyDOMURL,
} from "@shared/api/test-utils.js";

import { fallback, navigate, redirect, routes } from "./router.js";

/** Replace history.replaceState with a spy that records the paths. */
function stubReplaceState() {
  const calls: string[] = [];
  const original = window.history.replaceState;
  Object.defineProperty(window.history, "replaceState", {
    value: (_data: unknown, _unused: string, path: string) => {
      calls.push(path);
    },
    writable: true,
    configurable: true,
  });
  return {
    calls,
    restore: () => {
      Object.defineProperty(window.history, "replaceState", {
        value: original,
        writable: true,
        configurable: true,
      });
    },
  };
}

afterEach(() => {
  document.body.innerHTML = "";
  resetHappyDOMURL();
});

describe("redirect", () => {
  test("uses replaceState and dispatches NAVIGATE_EVENT", () => {
    const { calls, restore } = stubReplaceState();
    const nav = listenForNavigation();
    try {
      // "/account" differs from happy-dom's default pathname, so the
      // replaceState branch runs.
      redirect("/account");
      expect(calls).toContain("/account");
      expect(nav.path).toBe("/account");
    } finally {
      nav.remove();
      restore();
    }
  });
});

describe("navigate", () => {
  test("dispatches NAVIGATE_EVENT with the path", () => {
    const nav = listenForNavigation();
    try {
      navigate("/search");
      expect(nav.path).toBe("/search");
    } finally {
      nav.remove();
    }
  });
});

describe("fallback (404)", () => {
  test("renders catalog copy — the 404 heading is catalog, not a hardcoded glyph", () => {
    const container = document.createElement("div");
    render(fallback.render(), container);

    expect(container.textContent).toContain(el.ui.notFoundTitle);
    expect(container.textContent).toContain(el.ui.notFound);
    expect(container.textContent).toContain(el.ui.home);
  });
});

// ── Layout classes ──────────────────────────────────────────────────

describe("route layout classes", () => {
  /**
   * The route table's layout contract: which roots opt into the shell's
   * vertical centering (.center-route) and which take the wider content
   * cap (.wide-route).
   *
   * The template is rendered into a DETACHED container: the root element is
   * created and its class attribute is set by the template, while no
   * custom-element lifecycle runs (nothing is connected, so no page fetch,
   * guard, or redirect fires). That is exactly the contract under test.
   */
  function rootClass(template: TemplateResult): string {
    const container = document.createElement("div");
    render(template, container);
    return container.firstElementChild?.getAttribute("class") ?? "";
  }

  function routeClass(path: string): string {
    // Look the route up by its exact path (dynamic patterns included); the
    // cast narrows the RouteConfig union to the path variant.
    const route = (routes as PathRouteConfig[]).find((r) => r.path === path);
    expect(route, path).toBeDefined();
    return rootClass(route!.render!({}) as TemplateResult);
  }

  test("the single-card auth routes keep the centering opt-in", () => {
    // They own one short block and no page chrome, so nothing above them
    // can move — a centered card is the better read.
    for (const path of [
      "/sign-in",
      "/register",
      "/auth/forgot",
      "/auth/reset",
      "/auth/verify",
    ]) {
      expect(routeClass(path), path).toContain("center-route");
    }
    expect(rootClass(fallback.render())).toContain("center-route");
  });

  test("content routes are top-aligned — no centering opt-in", () => {
    for (const path of [
      "/",
      "/about",
      "/account",
      "/admin",
      "/admin/metrics",
      "/notifications",
    ]) {
      expect(routeClass(path), path).not.toContain("center-route");
    }
  });

  test("every route root carries exactly the layout class it needs", () => {
    const expected: Record<string, string> = {
      "/": "wide-route",
      "/blog/:id": "wide-route",
      "/projects": "wide-route",
      "/projects/:id": "wide-route",
      "/search": "wide-route",
      "/account": "full-route",
      "/admin": "full-route",
      "/admin/projects": "full-route",
      "/admin/users": "full-route",
      "/admin/metrics": "full-route",
      "/admin/logs": "full-route",
      "/notifications": "full-route",
      "/sign-in": "center-route",
      "/register": "center-route",
      "/auth/forgot": "center-route",
      "/auth/reset": "center-route",
      "/auth/verify": "center-route",
      "/about": "",
      "/admin/blog/new": "",
      "/admin/blog/:id": "",
      "/admin/projects/new": "",
      "/admin/projects/:id": "",
    };
    // Full coverage: a new route without an expectation fails here.
    expect(Object.keys(expected).sort()).toEqual(
      (routes as PathRouteConfig[]).map((r) => r.path).sort(),
    );
    for (const [path, cls] of Object.entries(expected)) {
      const actual = routeClass(path);
      if (cls === "") {
        expect(actual, path).not.toContain("wide-route");
        expect(actual, path).not.toContain("full-route");
        expect(actual, path).not.toContain("center-route");
      } else {
        expect(actual, path).toContain(cls);
      }
    }
  });
});
