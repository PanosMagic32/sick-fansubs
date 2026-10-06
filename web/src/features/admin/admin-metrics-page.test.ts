/**
 * Admin metrics tab tests — the guard (anonymous
 * redirect, below-moderator forbidden), the promoted server payload rendered
 * as numbers and bars, the window line, the tab chrome, and the error state
 * with retry.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  resetHappyDOMURL,
  restoreHistoryLocationSync,
  restoreMockFetch,
  setHappyDOMURL,
  shimHistoryLocationSync,
  stripCssComments,
  waitFor,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./admin-metrics-page.js";
import type { AdminMetricsPage } from "./admin-metrics-page.js";

// The stylesheet is read from disk (the search-page precedent): the bar
// geometry is pinned against the source with its comments stripped, because
// happy-dom resolves no CSS at all.
const metricsStyles = stripCssComments(
  await Bun.file(new URL("./admin-metrics-page.css", import.meta.url)).text(),
);

/** sf-navigate listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

function mockSession(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "moderator",
) {
  return mockSessionContext({ status, user: { username: "Mod", role } });
}

/** Stubs fetch and records every requested URL. */
function stubFetch(
  handler: (url: string) => Promise<{ status: number; body: unknown }>,
) {
  const urls: string[] = [];
  mockFetchImpl(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    urls.push(url);
    const res = await handler(url);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400 ? { "Content-Type": "application/problem+json" } : {},
      jsonBody: res.body,
    });
  });
  return urls;
}

const METRICS_BODY = {
  totals: {
    users: {
      total: 4,
      byRole: { user: 2, moderator: 1, admin: 1, superAdmin: 0 },
      byStatus: { active: 3, suspended: 1 },
    },
    blogPosts: { total: 10, published: 7, draft: 2, archived: 1 },
    projects: { total: 3, published: 2, draft: 1, archived: 0 },
    comments: 5,
    favorites: 6,
  },
  activity30d: {
    since: "2026-08-18T09:00:00.000Z",
    registrations: 2,
    activeUsers: 3,
    signInFailures: 1,
    contentCreated: 4,
    contentUpdated: 5,
    contentDeleted: 0,
    commentDeletions: 1,
    usersSuspended: 1,
    usersReactivated: 0,
    usersDeleted: 0,
    roleChanges: 2,
  },
};

const ZERO_BODY = {
  totals: {
    users: {
      total: 0,
      byRole: { user: 0, moderator: 0, admin: 0, superAdmin: 0 },
      byStatus: { active: 0, suspended: 0 },
    },
    blogPosts: { total: 0, published: 0, draft: 0, archived: 0 },
    projects: { total: 0, published: 0, draft: 0, archived: 0 },
    comments: 0,
    favorites: 0,
  },
  activity30d: {
    since: "2026-08-18T09:00:00.000Z",
    registrations: 0,
    activeUsers: 0,
    signInFailures: 0,
    contentCreated: 0,
    contentUpdated: 0,
    contentDeleted: 0,
    commentDeletions: 0,
    usersSuspended: 0,
    usersReactivated: 0,
    usersDeleted: 0,
    roleChanges: 0,
  },
};

function problemBody(status: number) {
  return {
    type: "/problems/internal-error",
    title: "X",
    status,
    requestId: "rid",
  };
}

async function render(
  role = "moderator",
  status: "authenticated" | "anonymous" | "error" = "authenticated",
): Promise<AdminMetricsPage> {
  setHappyDOMURL("https://example.com/admin/metrics");
  const page = document.createElement("admin-metrics-page") as AdminMetricsPage;
  (page as any).session = mockSession(status, role);
  document.body.appendChild(page);
  await page.updateComplete;
  await new Promise((r) => setTimeout(r, 10));
  await page.updateComplete;
  return page;
}

function root(page: AdminMetricsPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
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
  restoreMockFetch();
});

describe("admin-metrics-page", () => {
  test("anonymous visitors are redirected to /sign-in and nothing is fetched", async () => {
    let redirected = "";
    const listener = ((e: Event) => {
      redirected = (e as CustomEvent<{ path: string }>).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);
    navCleanups.push(() => window.removeEventListener("sf-navigate", listener));

    const urls = stubFetch(async () => ({ status: 200, body: METRICS_BODY }));

    await render("moderator", "anonymous");

    expect(redirected).toBe("/sign-in");
    expect(urls.length).toBe(0);
  });

  test("a plain user sees the forbidden state and the server is never asked", async () => {
    const urls = stubFetch(async () => ({ status: 200, body: METRICS_BODY }));

    const page = await render("user");

    expect(root(page).querySelector(".admin-forbidden")).not.toBeNull();
    expect(root(page).querySelector(".metrics-card")).toBeNull();
    expect(urls.length).toBe(0);
  });

  test("a moderator sees the totals, the breakdowns, and the activity rows", async () => {
    const urls = stubFetch(async () => ({ status: 200, body: METRICS_BODY }));

    const page = await render("moderator");

    expect(urls).toEqual(["/api/v1/staff/metrics"]);

    const values = Array.from(
      root(page).querySelectorAll(".metrics-card-value"),
    ).map((el) => el.textContent?.trim());
    expect(values).toEqual(["4", "10", "3", "5", "6"]);

    // Breakdown rows carry catalog copy (Greek), never raw wire values.
    const labels = Array.from(
      root(page).querySelectorAll(".metrics-bar-label"),
    ).map((el) => el.textContent?.trim());
    expect(labels).toContain(el.account.roleModerator);
    expect(labels).toContain(el.admin.statusSuspended);
    expect(labels).toContain(el.admin.metricsRoleChanges);

    // The activity window line states the server's opening instant.
    expect(root(page).querySelector(".metrics-since")?.textContent).toContain(
      el.admin.metricsSincePrefix,
    );

    // One bar per activity counter, and the largest value renders 100%.
    const fills = Array.from(
      root(page).querySelectorAll(".metrics-bar-fill"),
    ) as HTMLElement[];
    expect(fills.some((f) => f.getAttribute("style")?.includes("100%"))).toBe(
      true,
    );
  });

  test("only the share-based groups print percentages; the activity scale is captioned", async () => {
    stubFetch(async () => ({ status: 200, body: METRICS_BODY }));

    const page = await render("moderator");
    // Scope by SECTION rather than by row order: the totals section owns every
    // share-based group (roles, statuses, both content kinds), the activity
    // section owns the relative one.
    const sections = [...root(page).querySelectorAll(".metrics-section")];
    const totalsRows = [
      ...(sections[0]?.querySelectorAll(".metrics-bar-row") ?? []),
    ];
    const activityRows = [
      ...(sections[1]?.querySelectorAll(".metrics-bar-row") ?? []),
    ];
    expect(totalsRows).toHaveLength(12);
    expect(activityRows).toHaveLength(11);

    // Totals rows (users by role 2/4, by status 3/4 and 1/4, blog 7/10…):
    // counts AND their share of the group's own total.
    const totalsShares = totalsRows
      .map((r) => r.querySelector(".metrics-bar-share")?.textContent?.trim())
      .filter((s): s is string => s !== undefined);
    expect(totalsShares).toHaveLength(totalsRows.length);
    expect(totalsShares).toContain("50%");
    expect(totalsShares).toContain("75%");
    expect(totalsShares).toContain("70%");

    // Activity rows: counts alone — their denominator is the section's
    // largest counter, so a percentage would claim a whole that does not
    // exist.
    for (const row of activityRows) {
      expect(row.querySelector(".metrics-bar-share")).toBeNull();
    }

    // Both scales are stated in Greek, in the catalog's words.
    const captions = [...root(page).querySelectorAll(".metrics-scale")].map(
      (p) => p.textContent?.trim(),
    );
    expect(captions).toEqual([
      el.admin.metricsTotalsScale,
      el.admin.metricsActivityScale,
    ]);
  });

  test("the activity rows split into two half-width groups", async () => {
    stubFetch(async () => ({ status: 200, body: METRICS_BODY }));

    const page = await render("moderator");
    const sections = [...root(page).querySelectorAll(".metrics-section")];
    const activity = sections[1]!;
    const groups = [
      ...(activity?.querySelectorAll(".activity-breakdowns .metrics-bars") ??
        []),
    ];
    expect(groups).toHaveLength(2);
    for (const group of groups) {
      const rowCount = group.querySelectorAll(".metrics-bar-row").length;
      expect(rowCount).toBeGreaterThan(0);
      expect(rowCount).toBeLessThanOrEqual(6);
    }

    // Both groups share ONE denominator: the section's largest counter, so
    // exactly one bar measures 100% across both groups. A per-group maximum
    // would produce a second 100% bar and make the halves incomparable.
    const widths = [...activity.querySelectorAll(".metrics-bar-fill")].map(
      (fill) => (fill as HTMLElement).style.width,
    );
    expect(widths.filter((w) => w === "100%")).toHaveLength(1);

    // The split's own rules: the base flex row wraps the pair to one column
    // when narrow, and the unequal groups top-align instead of stretching.
    const base =
      metricsStyles.match(/\.metrics-breakdowns \{([^}]*)\}/)?.[1] ?? "";
    expect(base).toContain("flex-wrap: wrap");
    const breakdowns =
      metricsStyles.match(/\.activity-breakdowns \{([^}]*)\}/)?.[1] ?? "";
    expect(breakdowns).toContain("align-items: flex-start");
  });

  test("the track takes the space the labels do not use", async () => {
    // ONE grid per group: the labels keep their room and the track takes the
    // space that is left, so every row's bars start at the same x. A per-row
    // grid sized its own columns instead, and a phone-width row left the
    // track a stub (`minmax(8rem, 16rem)` grew its label track with the free
    // space in the same pass as the flexible one). happy-dom resolves no CSS,
    // so the geometry is pinned against the stylesheet with its comments
    // stripped.
    const group = metricsStyles.match(/\.metrics-bars \{([^}]*)\}/s)?.[1] ?? "";
    expect(group).toContain("display: grid");
    // A content-sized label track is frozen at its content width, so the `1fr`
    // track gets everything else.
    expect(group).toContain("grid-template-columns: max-content 1fr auto");
    // The cap rides on the GROUP — a row has no box to carry it.
    expect(group).toContain("max-width: 48rem");

    // A row contributes its three cells to the GROUP's columns; without
    // `display: contents` each row would size its own columns.
    const row =
      metricsStyles.match(/\.metrics-bar-row \{([^}]*)\}/s)?.[1] ?? "";
    expect(row).toContain("display: contents");
    expect(row).not.toContain("max-width");
    expect(row).not.toContain("minmax(");

    // The group's title is a full-width line, not a column entry.
    const title =
      metricsStyles.match(/\.metrics-bars-title \{([^}]*)\}/s)?.[1] ?? "";
    expect(title).toContain("grid-column: 1 / -1");

    // The label inherits the row's font size, and its cell owns the column's
    // floor and ceiling.
    const label =
      metricsStyles.match(/\.metrics-bar-label \{([^}]*)\}/s)?.[1] ?? "";
    expect(label).not.toContain("font-size");
    expect(label).toContain("min-width: 8rem");
    expect(label).toContain("max-width: 16rem");
    // The track keeps its height.
    const track =
      metricsStyles.match(/\.metrics-bar-track \{([^}]*)\}/s)?.[1] ?? "";
    expect(track).toContain("height: 0.7rem");
  });

  test("the metrics tab is current in the shared tab chrome", async () => {
    stubFetch(async () => ({ status: 200, body: METRICS_BODY }));

    const page = await render("moderator");
    const tabs = root(page).querySelector("admin-tabs")!;
    const links = Array.from(
      tabs.shadowRoot!.querySelectorAll("a"),
    ) as HTMLAnchorElement[];

    expect(links.length).toBe(4);
    const current = links.filter(
      (a) => a.getAttribute("aria-current") === "page",
    );
    expect(current.length).toBe(1);
    expect(current[0]?.getAttribute("href")).toBe("/admin/metrics");
  });

  test("zero totals render as zeros with empty bars, not NaN", async () => {
    stubFetch(async () => ({ status: 200, body: ZERO_BODY }));

    const page = await render("moderator");

    const values = Array.from(
      root(page).querySelectorAll(".metrics-card-value"),
    ).map((el) => el.textContent?.trim());
    expect(values).toEqual(["0", "0", "0", "0", "0"]);

    const fills = root(page).querySelectorAll(".metrics-bar-fill");
    expect(fills.length).toBeGreaterThan(0);
    for (const fill of Array.from(fills)) {
      expect(fill.getAttribute("style")).toContain("0%");
    }
  });

  test("a failed load shows the error banner and retry refetches", async () => {
    let fail = true;
    const urls = stubFetch(async () =>
      fail
        ? { status: 500, body: problemBody(500) }
        : { status: 200, body: METRICS_BODY },
    );

    const page = await render("moderator");
    expect(root(page).querySelector("error-banner")).not.toBeNull();
    expect(root(page).querySelector(".metrics-card")).toBeNull();

    fail = false;
    root(page)
      .querySelector("error-banner")!
      .dispatchEvent(new CustomEvent("retry", { bubbles: true }));
    await waitFor(() => root(page).querySelector(".metrics-card") !== null);
    await page.updateComplete;

    expect(urls.length).toBe(2);
    expect(root(page).querySelector(".metrics-card")).not.toBeNull();
  });
});
