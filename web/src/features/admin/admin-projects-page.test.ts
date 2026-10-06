/**
 * Admin projects tab tests — the shared guard via
 * AdminListBase, the staff project list rendering with the slug secondary
 * line, status badges, edit links, and the tab chrome's aria-current.
 */

import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  resetHappyDOMURL,
  restoreMockFetch,
  setHappyDOMURL,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./admin-projects-page.js";
import type { AdminProjectsPage } from "./admin-projects-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";

function mockSession(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "admin",
) {
  return mockSessionContext({ status, user: { role } });
}

async function render(
  role = "admin",
  status: "authenticated" | "anonymous" | "error" = "authenticated",
): Promise<AdminProjectsPage> {
  const page = document.createElement(
    "admin-projects-page",
  ) as AdminProjectsPage;
  (page as any).session = mockSession(status, role);
  document.body.appendChild(page);
  await page.updateComplete;
  await settle();
  await page.updateComplete;
  return page;
}

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

/** Navigation listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

/** The rendered content cards, in list order. */
function cards(page: AdminProjectsPage): ContentCard[] {
  return [
    ...(page.shadowRoot?.querySelectorAll("content-card") ?? []),
  ] as ContentCard[];
}

beforeEach(() => {
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of navCleanups) cleanup();
  navCleanups.length = 0;
  resetHappyDOMURL();
  restoreMockFetch();
});

const LIST_RESPONSE = {
  items: [
    {
      id: "p1",
      title: "Project One",
      description: "",
      slug: "project-one",
      thumbnailUrl: "https://fans.example/media/images/a.jpg",
      status: "published",
      publishedAt: "2023-11-14T22:13:20.000Z",
      updatedAt: "2023-11-15T10:00:00.000Z",
      revision: 1,
      creator: null,
    },
    {
      id: "p2",
      title: "Draft Two",
      description: "",
      slug: "draft-two",
      thumbnailUrl: null,
      status: "draft",
      publishedAt: null,
      updatedAt: "2023-11-15T11:00:00.000Z",
      revision: 1,
      creator: null,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

describe("admin-projects-page", () => {
  test("anonymous visitors are redirected to /sign-in", async () => {
    let redirected = "";
    const listener = ((e: Event) => {
      redirected = (e as CustomEvent<{ path: string }>).detail.path;
    }) as EventListener;
    window.addEventListener("sf-navigate", listener);
    navCleanups.push(() => window.removeEventListener("sf-navigate", listener));

    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE }),
    );

    await render("admin", "anonymous");

    expect(redirected).toBe("/sign-in");
  });

  test("a plain user sees the forbidden state and no list", async () => {
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE }),
    );

    const page = await render("user");

    expect(page.shadowRoot?.querySelector(".admin-forbidden")).not.toBeNull();
    expect(page.shadowRoot?.querySelector(".admin-list")).toBeNull();
  });

  test("a moderator renders the list with badges, slugs, and edit links", async () => {
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE }),
    );

    const page = await render("moderator");

    const rows = cards(page);
    expect(rows.length).toBe(2);

    // The status chip rides the card's badge inputs; the chip treatments
    // are the card's own contract (pinned in content-card.test.ts).
    expect(rows[0]?.badge).toBe(el.admin.statusPublished);
    expect(rows[0]?.badgeTone).toBe("published");
    expect(rows[1]?.badge).toBe(el.admin.statusDraft);
    expect(rows[1]?.badgeTone).toBe("draft");

    // The slug rides the card's muted secondary line on every row.
    expect(rows.map((row) => row.secondary)).toEqual([
      "project-one",
      "draft-two",
    ]);

    const editLinks = page.shadowRoot?.querySelectorAll(".edit-link");
    expect(editLinks?.[0]?.getAttribute("href")).toBe("/admin/projects/p1");
    // The edit control is the card's slotted in-flow action.
    expect(editLinks?.[0]?.getAttribute("slot")).toBe("actions");
  });

  test("the tab chrome marks the projects tab as current", async () => {
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE }),
    );

    const page = await render("moderator");

    // The tab chrome is its own element (the users page is the third
    // consumer), so the tabs live in ITS shadow root.
    const tabsEl = page.shadowRoot?.querySelector("admin-tabs");
    const tabs = tabsEl?.shadowRoot?.querySelectorAll(".admin-tab");
    expect(tabs?.length).toBe(4);
    expect(tabs?.[0]?.getAttribute("aria-current")).toBeNull();
    expect(tabs?.[1]?.getAttribute("aria-current")).toBe("page");
    expect(tabs?.[1]?.getAttribute("href")).toBe("/admin/projects");
    expect(tabs?.[2]?.getAttribute("aria-current")).toBeNull();
    expect(tabs?.[3]?.getAttribute("aria-current")).toBeNull();
  });

  test("the URL-carried filters ride the list request", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });
    setHappyDOMURL(
      "https://example.com/admin/projects?status=draft&q=dr+stone",
    );

    await render("admin");

    // Both filters are the page's own spreads onto the wire params; a
    // deep link must reach the fetch untouched.
    expect(urls).toEqual([
      "/api/v1/admin/projects?limit=10&status=draft&q=dr+stone",
    ]);
  });

  test("a failed load shows the error banner with retry", async () => {
    mockFetchImpl(async () =>
      mockResponse({
        ok: false,
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
        jsonBody: {
          type: "/problems/internal-error",
          title: "Internal server error",
          status: 500,
        },
      }),
    );

    const page = await render("admin");

    expect(
      page.shadowRoot
        ?.querySelector("error-banner")
        ?.shadowRoot?.querySelector(".error"),
    ).not.toBeNull();
  });
});
