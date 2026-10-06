/**
 * Admin dashboard tests — the guard (anonymous
 * redirect, below-moderator forbidden), the staff list rendering with
 * status badges and edit links, and the pager wiring.
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
  stubProperty,
} from "@shared/api/test-utils.js";
import { formatDateTimeNumeric } from "@shared/utils/format.js";

import "@features/auth/data-access/session.js";
import "./admin-page.js";
import type { AdminPage } from "./admin-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";

/** sf-navigate listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

function mockSession(
  status: "authenticated" | "anonymous" | "error" = "authenticated",
  role = "admin",
) {
  return mockSessionContext({ status, user: { role } });
}

async function render(
  role = "admin",
  status: "authenticated" | "anonymous" | "error" = "authenticated",
): Promise<AdminPage> {
  const page = document.createElement("admin-page") as AdminPage;
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

/** The rendered content cards, in list order. */
function cards(page: AdminPage): ContentCard[] {
  return [
    ...(page.shadowRoot?.querySelectorAll("content-card") ?? []),
  ] as ContentCard[];
}

/** A card's own shadow root — the card renders its content there. */
function cardRoot(card: ContentCard): HTMLElement {
  return card.shadowRoot! as unknown as HTMLElement;
}

beforeEach(() => {
  // The list's filters ride the URL, so these tests need the history/location
  // sync shim (the admin-users-page precedent) and a real base URL for the
  // relative pushState targets.
  shimHistoryLocationSync();
  setHappyDOMURL("https://example.com/admin");
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of navCleanups) cleanup();
  navCleanups.length = 0;
  restoreHistoryLocationSync();
  resetHappyDOMURL();
  restoreMockFetch();
});

const LIST_RESPONSE = {
  items: [
    {
      id: "p1",
      title: "Post One",
      subtitle: "Episode One",
      description: "",
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
      subtitle: "",
      description: "",
      thumbnailUrl: "https://fans.example/media/images/b.jpg",
      status: "draft",
      publishedAt: null,
      updatedAt: "2023-11-15T11:00:00.000Z",
      revision: 1,
      creator: null,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

describe("admin-page", () => {
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

  test("exactly ONE list fetch per authenticated mount (no double-load)", async () => {
    // Regression pin: syncOnConnect:false + the authenticated latch must
    // issue ONE list request per mount — a mount-time sync alongside the
    // latch would double-fetch and abort the first.
    const listFetch = mock(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE }),
    );
    mockFetchImpl(listFetch);

    const page = await render("admin");

    expect(page.shadowRoot?.querySelector(".admin-list")).not.toBeNull();
    expect(listFetch.mock.calls.length).toBe(1);
  });

  test("an empty list renders the catalog copy inside the reserved state block", async () => {
    mockFetchImpl(async () =>
      mockResponse({
        ok: true,
        status: 200,
        jsonBody: {
          items: [],
          pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
        },
      }),
    );

    const page = await render("moderator");

    const empty = page.shadowRoot?.querySelector(".admin-empty");
    expect(empty?.textContent?.trim()).toBe(el.admin.listEmpty);
    expect(empty?.getAttribute("role")).toBe("status");
    // The reserved-footprint wrapper is what keeps the tab row above from
    // resizing between the loading, empty, and content states.
    expect(empty?.closest(".state-block")).not.toBeNull();
    expect(page.shadowRoot?.querySelector(".admin-list")).toBeNull();
    expect(page.shadowRoot?.querySelector("pager-nav")).toBeNull();
  });

  test("a moderator renders the list with status badges and edit links", async () => {
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE }),
    );

    const page = await render("moderator");

    // Two management rows: a static card each (no href — a row is not a
    // link), the compact geometry, and the row title as plain text.
    const rows = cards(page);
    expect(rows.length).toBe(2);
    expect(rows.map((row) => row.shape)).toEqual(["row", "row"]);
    expect(rows.every((row) => row.dense)).toBe(true);
    expect(rows.every((row) => row.headingLevel === 0)).toBe(true);
    expect(rows.every((row) => row.href === "")).toBe(true);
    expect(rows.map((row) => row.title)).toEqual(["Post One", "Draft Two"]);
    // The inline subtitle rides the card's subtitle input and renders as the
    // title line; a row without one (the draft) keeps the plain title.
    expect(rows[0]?.subtitle).toBe("Episode One");
    expect(rows[1]?.subtitle).toBe("");
    const firstRow = cardRoot(rows[0]!);
    expect(firstRow.querySelector(".subtitle")?.textContent?.trim()).toBe(
      "Episode One",
    );
    expect(firstRow.querySelector(".subtitle-sep")).not.toBeNull();
    expect(cardRoot(rows[1]!).querySelector(".title-line")).toBeNull();

    // The status chip rides the card's badge inputs; the chip treatments
    // are the card's own contract (pinned in content-card.test.ts).
    expect(rows[0]?.badge).toBe(el.admin.statusPublished);
    expect(rows[0]?.badgeTone).toBe("published");
    expect(rows[1]?.badge).toBe(el.admin.statusDraft);
    expect(rows[1]?.badgeTone).toBe("draft");

    const editLinks = page.shadowRoot?.querySelectorAll(".edit-link");
    expect(editLinks?.[0]?.getAttribute("href")).toBe("/admin/blog/p1");
    // The edit control is the card's slotted in-flow action.
    expect(editLinks?.[0]?.getAttribute("slot")).toBe("actions");
  });

  test("rows hand the card the thumbnail, the description, and the update instant the card formats", async () => {
    mockFetchImpl(async () =>
      mockResponse({
        ok: true,
        status: 200,
        jsonBody: {
          items: [
            {
              ...LIST_RESPONSE.items[0]!,
              description: "Περιγραφή",
              updatedAt: "2025-11-15T10:00:00.000Z",
            },
          ],
          pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
        },
      }),
    );

    const page = await render("moderator");

    // The favorites card shape's inputs: the thumbnail (rendered in the
    // card's own row), the description, and the update instant with its
    // catalog label.
    const [row] = cards(page);
    await row!.updateComplete;
    expect(row?.thumbnailUrl).toBe("https://fans.example/media/images/a.jpg");
    expect(row?.description).toBe("Περιγραφή");
    expect(row?.metaLabel).toBe(el.blog.updated);
    expect(row?.metaInstant).toBe("2025-11-15T10:00:00.000Z");
    expect(cardRoot(row!).querySelector("img.thumb")?.getAttribute("src")).toBe(
      "https://fans.example/media/images/a.jpg",
    );

    // The date is the shared numeric formatter's output — the raw ISO
    // instant the old row printed must never come back.
    const meta = cardRoot(row!).querySelector(".meta")?.textContent ?? "";
    expect(meta).toContain(el.blog.updated);
    expect(meta).toContain(formatDateTimeNumeric("2025-11-15T10:00:00.000Z"));
    expect(meta).not.toContain("2025-11-15T10:00:00.000Z");
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

  test("the status filter pushes a first-page URL and refetches with it", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    const page = await render("admin");
    urls.length = 0;

    const select = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;
    // The every-status arm renders the catalog copy and is the default.
    expect(select.value).toBe("");
    expect(select.options[0]?.textContent?.trim()).toBe(
      el.admin.filterStatusAll,
    );
    expect([...select.options].map((o) => o.value)).toEqual([
      "",
      "draft",
      "published",
      "archived",
    ]);

    select.value = "draft";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await settle();
    await page.updateComplete;

    expect(window.location.search).toBe("?status=draft");
    expect(urls).toEqual(["/api/v1/admin/blog-posts?limit=10&status=draft"]);
    // The control keeps the picked value (the data-value pass — the first
    // option must not win after the re-render).
    const after = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;
    expect(after.value).toBe("draft");
  });

  test("the title filter applies on submit, never per keystroke", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    const page = await render("admin");
    urls.length = 0;

    const input = page.shadowRoot?.querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "piece";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await settle();
    expect(urls).toHaveLength(0);

    page.shadowRoot
      ?.querySelector(".admin-search")
      ?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await settle();
    await page.updateComplete;

    expect(window.location.search).toBe("?q=piece");
    expect(urls).toEqual(["/api/v1/admin/blog-posts?limit=10&q=piece"]);
  });

  test("the URL primes both filters, and out-of-contract values are never sent", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    setHappyDOMURL("https://example.com/admin?status=archived&q=%20piece%20");
    const page = await render("admin");

    expect(urls).toEqual([
      "/api/v1/admin/blog-posts?limit=10&status=archived&q=piece",
    ]);
    const select = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;
    expect(select.value).toBe("archived");
    expect(
      (page.shadowRoot?.querySelector('input[name="q"]') as HTMLInputElement)
        .value,
    ).toBe("piece");

    // A hand-edited status is ignored (the server would 422 it) and the
    // filter falls back to every status.
    urls.length = 0;
    page.remove();
    setHappyDOMURL("https://example.com/admin?status=bogus&q=piece");
    const again = document.createElement("admin-page") as AdminPage;
    (again as any).session = mockSession("authenticated", "admin");
    document.body.appendChild(again);
    await again.updateComplete;
    await settle();
    expect(urls).toEqual(["/api/v1/admin/blog-posts?limit=10&q=piece"]);
    again.remove();
  });

  test("the every-status arm is a real selection the user can return to", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    const page = await render("admin");
    const select = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;

    select.value = "draft";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await settle();
    await page.updateComplete;

    // Back to every status: the URL drops the param, the list refetches, and
    // the CONTROL follows — the imperative pass must apply the empty value,
    // or the select keeps reading «Πρόχειρο» over an unfiltered list.
    urls.length = 0;
    const after = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;
    after.value = "";
    after.dispatchEvent(new Event("change", { bubbles: true }));
    await settle();
    await page.updateComplete;

    expect(window.location.search).toBe("");
    expect(urls).toEqual(["/api/v1/admin/blog-posts?limit=10"]);
    expect(
      (
        page.shadowRoot?.querySelector(
          'select[name="status"]',
        ) as HTMLSelectElement
      ).value,
    ).toBe("");
  });

  test("changing the status carries text typed but not yet submitted", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    const page = await render("admin");
    urls.length = 0;

    const input = page.shadowRoot?.querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    input.value = "piece";
    input.dispatchEvent(new Event("input", { bubbles: true }));

    const select = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;
    select.value = "draft";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    await settle();
    await page.updateComplete;

    // One request carrying both, and the field keeps the text it held — the
    // filter row is one filter set, so a status change never drops typing.
    expect(urls).toEqual([
      "/api/v1/admin/blog-posts?limit=10&status=draft&q=piece",
    ]);
    expect(window.location.search).toBe("?status=draft&q=piece");
    expect(
      (page.shadowRoot?.querySelector('input[name="q"]') as HTMLInputElement)
        .value,
    ).toBe("piece");
  });

  test("a below-floor viewer can never drive the filter row", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    const page = await render("user");

    // The row is visible but inert: the documented invariant is that the
    // client floor means NO request, and an enabled control would fire one
    // 403 per touch.
    const select = page.shadowRoot?.querySelector(
      'select[name="status"]',
    ) as HTMLSelectElement;
    const input = page.shadowRoot?.querySelector(
      'input[name="q"]',
    ) as HTMLInputElement;
    const apply = page.shadowRoot?.querySelector(
      ".admin-filter-apply",
    ) as HTMLButtonElement;
    expect(select.disabled).toBe(true);
    expect(input.disabled).toBe(true);
    expect(apply.disabled).toBe(true);
    expect(urls).toHaveLength(0);
  });

  test("an over-long URL filter is ignored, never sent into a 422", async () => {
    const urls: string[] = [];
    mockFetchImpl(async (input: RequestInfo | URL) => {
      urls.push(String(input));
      return mockResponse({ ok: true, status: 200, jsonBody: LIST_RESPONSE });
    });

    setHappyDOMURL(
      `https://example.com/admin?q=${encodeURIComponent("a".repeat(101))}`,
    );
    const page = await render("admin");

    expect(urls).toEqual(["/api/v1/admin/blog-posts?limit=10"]);
    expect(
      (page.shadowRoot?.querySelector('input[name="q"]') as HTMLInputElement)
        .value,
    ).toBe("");
  });

  test("a filtered empty list says so instead of claiming there is no content", async () => {
    mockFetchImpl(async () =>
      mockResponse({
        ok: true,
        status: 200,
        jsonBody: {
          items: [],
          pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
        },
      }),
    );

    setHappyDOMURL("https://example.com/admin?status=draft&q=zzz");
    const page = await render("admin");

    expect(
      page.shadowRoot?.querySelector(".admin-empty")?.textContent?.trim(),
    ).toBe(el.admin.listEmptyFiltered);

    // Without a filter the same empty page keeps the unfiltered copy.
    page.remove();
    setHappyDOMURL("https://example.com/admin");
    const plain = document.createElement("admin-page") as AdminPage;
    (plain as any).session = mockSession("authenticated", "admin");
    document.body.appendChild(plain);
    await plain.updateComplete;
    await settle();
    expect(
      plain.shadowRoot?.querySelector(".admin-empty")?.textContent?.trim(),
    ).toBe(el.admin.listEmpty);
    plain.remove();
  });

  test("the composed signal carries the client deadline (a timeout-arm abort aborts the request)", async () => {
    const timeoutArm = new AbortController();
    const restoreTimeout = stubProperty(
      AbortSignal,
      "timeout",
      () => timeoutArm.signal,
    );
    const capture: { signal: AbortSignal | null } = { signal: null };
    mockFetchImpl((_input, init) => {
      capture.signal = init?.signal ?? null;
      return new Promise<Response>(() => {});
    });

    try {
      const page = await render("admin");
      expect(capture.signal).not.toBeNull();

      // Aborting the timeout arm alone must abort the request: the pre-fix
      // raw controller.signal would survive this.
      timeoutArm.abort();
      expect(capture.signal!.aborted).toBe(true);
      page.remove();
    } finally {
      restoreTimeout();
    }
  });
});
