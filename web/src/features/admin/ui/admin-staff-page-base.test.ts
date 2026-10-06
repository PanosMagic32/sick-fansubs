/**
 * AdminStaffPageBase tests — the client floor, the guard's three outcomes,
 * the authenticated latch, and the load-error mapping all three staff pages
 * now share.
 *
 * The floor matrix is the load-bearing part: `_hasStaffAccess` decides whether
 * the server is asked at all, so it must mirror the server's moderator+
 * capability (model.CanViewMetrics / CanViewStaffList). A narrower predicate
 * would strand a real moderator on the forbidden state with no request left to
 * correct it — the pin below is the client half of that mirror.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { ApiError } from "@shared/api/client.js";
import { el } from "@shared/catalog/el.js";
import {
  mockSessionContext,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  setHappyDOMURL,
  shimHistoryLocationSync,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import { AdminStaffPageBase } from "./admin-staff-page-base.js";

/** Minimal concrete page: records `_start` calls and exposes the protected
 * surface the assertions need. */
class TestStaffPage extends AdminStaffPageBase {
  started = 0;
  protected _start() {
    this.started++;
  }
  access(role: string): boolean {
    return this._hasStaffAccess(role);
  }
  /** Protected state exposed for assertions (the base owns these). */
  get pageStatus(): string {
    return this.status;
  }
  get pageError(): string {
    return this.errorMessage;
  }
  begin() {
    return this._beginLoad();
  }
  fail(err: unknown, controller: AbortController) {
    this._failLoad(err, controller);
  }
}
customElements.define("test-staff-page", TestStaffPage);

/** sf-navigate listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

function mockSession(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "moderator",
) {
  return mockSessionContext({ status, user: { username: "U", role } });
}

async function render(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "moderator",
): Promise<TestStaffPage> {
  setHappyDOMURL("https://example.com/admin/metrics");
  const page = document.createElement("test-staff-page") as TestStaffPage;
  (page as any).session = mockSession(status, role);
  document.body.appendChild(page);
  await page.updateComplete;
  await new Promise((r) => setTimeout(r, 5));
  await page.updateComplete;
  return page;
}

beforeEach(() => {
  shimHistoryLocationSync();
  window.history.replaceState(null, "", window.location.href);
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of navCleanups) cleanup();
  navCleanups.length = 0;
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  mock.restore();
});

describe("admin-staff-page-base", () => {
  test("the floor is moderator+ — the same floor the server enforces", async () => {
    const page = await render();

    expect(page.access("user")).toBe(false);
    expect(page.access("moderator")).toBe(true);
    expect(page.access("admin")).toBe(true);
    expect(page.access("super-admin")).toBe(true);
    expect(page.access("bogus")).toBe(false);
    expect(page.access("")).toBe(false);
  });

  test("anonymous visitors redirect to /sign-in and no load starts", async () => {
    let redirected = "";
    const listener = ((e: Event) => {
      redirected = (e as CustomEvent<{ path: string }>).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);
    navCleanups.push(() => window.removeEventListener("sf-navigate", listener));

    const page = await render("anonymous");

    expect(redirected).toBe("/sign-in");
    expect(page.started).toBe(0);
  });

  test("a below-floor viewer gets the forbidden state and no load starts", async () => {
    const page = await render("authenticated", "user");

    expect(page.pageStatus).toBe("forbidden");
    expect(page.started).toBe(0);
  });

  test("a session error state uses the catalog connection copy", async () => {
    const page = await render("error");

    expect(page.pageStatus).toBe("error");
    expect(page.pageError).toBe(el.ui.serverConnectionFailed);
    expect(page.started).toBe(0);
  });

  test("the first authenticated render starts exactly one load (the latch)", async () => {
    const page = await render();

    expect(page.started).toBe(1);

    // A revalidated session object (same authenticated status) must not
    // trigger a second start.
    (page as any).session = mockSession("authenticated", "moderator");
    await page.updateComplete;
    await new Promise((r) => setTimeout(r, 5));
    await page.updateComplete;

    expect(page.started).toBe(1);
  });

  test("_failLoad maps 403 to forbidden and anything else to the error state", async () => {
    const page = await render();
    const controller = page.begin();
    expect(page.pageStatus).toBe("loading");

    page.fail(
      new ApiError({
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 403,
      }),
      controller,
    );
    expect(page.pageStatus).toBe("forbidden");
    expect(page.pageError).toBe("");

    const second = page.begin();
    page.fail(new Error("boom"), second);
    expect(page.pageStatus).toBe("error");
    expect(page.pageError).not.toBe("");
  });

  test("_failLoad ignores an aborted (superseded) request", async () => {
    const page = await render();
    const controller = page.begin();
    controller.abort();

    page.fail(new Error("late failure"), controller);

    expect(page.pageStatus).toBe("loading");
    expect(page.pageError).toBe("");
  });
});
