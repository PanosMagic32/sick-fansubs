import { afterEach, describe, expect, mock, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockResponse,
  restoreMockFetch,
} from "@shared/api/test-utils.js";
import { FAVORITE_CHANGED_EVENT } from "@shared/events.js";
import { formatDateTimeNumeric } from "@shared/utils/format.js";

// The session module registers the context the edit affordance consumes
// (the blog detail's pattern).
import "@features/auth/data-access/session.js";

// Side-effect import registers the custom element (same pattern as the
// list page tests).
import "./project-detail-page.js";
import type { ProjectDetailPage } from "./project-detail-page.js";
import type { ContentCard } from "@shared/ui/content-card.js";
import type { ProjectDetail } from "./data-access/types.js";

/** Fetch-level router: component → real projects-api → real transport →
 * stub. The comments section fetches nothing
 * until its CTA is clicked, so these stubs only ever see the project
 * request. */
function stubFetch(
  handler: (url: string) => Promise<{ status: number; body: unknown }>,
) {
  mockFetchImpl(async (input) => {
    const url = typeof input === "string" ? input : input.toString();
    const res = await handler(url);
    return mockResponse({
      ok: res.status < 400,
      status: res.status,
      headers:
        res.status >= 400 ? { "Content-Type": "application/problem+json" } : {},
      jsonBody: res.body,
    });
  });
}

function sampleProject(overrides: Partial<ProjectDetail> = {}): ProjectDetail {
  return {
    id: "pr1",
    title: "Τίτλος έργου",
    description: "Περιγραφή έργου",
    slug: "titlos-ergou",
    thumbnailUrl: "https://example.com/t.jpg",
    publishedAt: "2026-08-15T18:30:00.000Z",
    updatedAt: "2026-08-15T18:30:00.000Z",
    commentCount: 1,
    favoriteCount: 1,
    creator: { id: "u1", username: "creator", avatarUrl: null },
    updater: null,
    downloads: [
      {
        name: "Batch 1",
        magnetUrl: "magnet:?xt=urn:btih:aaa",
        torrentUrl: "https://example.com/t.torrent",
      },
      { name: "Batch 2", magnetUrl: null, torrentUrl: null },
    ],
    ...overrides,
  };
}

function problemBody(type: string, status: number) {
  return { type, title: "X", status, requestId: "rid" };
}

async function renderPage(projectId: string) {
  const page = document.createElement(
    "project-detail-page",
  ) as ProjectDetailPage;
  page.projectId = projectId;
  document.body.appendChild(page);
  await flush(page);
  return page;
}

/** Flush microtasks + a render (same helper contract as the list tests). */
async function flush(page: ProjectDetailPage) {
  await new Promise((r) => setTimeout(r, 0));
  await page.updateComplete;
  // The lead block's card renders one binding hop behind the page.
  const host = root(page).querySelector("content-card") as ContentCard | null;
  await host?.updateComplete;
}

function root(page: ProjectDetailPage): HTMLElement {
  return page.shadowRoot! as unknown as HTMLElement;
}

/** The detail hero block — the page renders exactly one content card. */
function card(page: ProjectDetailPage): ContentCard {
  const host = root(page).querySelector("content-card") as ContentCard | null;
  if (!host) throw new Error("content-card not rendered");
  return host;
}

/** The card's own shadow root — the card renders the lead block there. */
function cardRoot(page: ProjectDetailPage): HTMLElement {
  return card(page).shadowRoot! as unknown as HTMLElement;
}

function text(root: HTMLElement, selector: string): string {
  // Whitespace is normalized: formatters/editors may re-layout a template
  // across lines, which changes the raw text nodes around interpolations.
  // The contract under test is the CONTENT (quote glyphs + text), not the
  // template's line layout — collapse every whitespace run to one space.
  return (root.querySelector(selector)?.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim();
}

function bannerText(page: ProjectDetailPage): string {
  const host = page.shadowRoot?.querySelector("error-banner");
  return host?.shadowRoot?.querySelector(".error")?.textContent?.trim() ?? "";
}

afterEach(() => {
  document.body.innerHTML = "";
  document.title = "";
  mock.restore();
  restoreMockFetch();
});

describe("project-detail-page", () => {
  test("renders the indicator stats and syncs the heart count live", async () => {
    mockFetchImpl(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/comments/count")) {
        return mockResponse({ ok: true, status: 200, jsonBody: { count: 1 } });
      }
      if (url.includes("/comments")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [],
            pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: sampleProject() });
    });

    const page = document.createElement(
      "project-detail-page",
    ) as ProjectDetailPage;
    page.projectId = "pr1";
    document.body.appendChild(page);
    await flush(page);

    const statEls = [...cardRoot(page).querySelectorAll(".detail-stats .stat")];
    expect(statEls.length).toBe(2);
    expect(statEls[0]?.getAttribute("aria-label")).toBe("1 σχόλιο");
    expect(statEls[1]?.getAttribute("aria-label")).toBe("1 αγαπημένο");

    // A confirmed heart flip bumps the public count.
    window.dispatchEvent(
      new CustomEvent(FAVORITE_CHANGED_EVENT, {
        detail: { kind: "projects", contentId: "pr1", favorited: true },
      }),
    );
    await flush(page);
    expect(
      cardRoot(page).querySelector('.detail-stats [aria-label="2 αγαπημένα"]'),
    ).not.toBeNull();
  });

  test("shows the loading spinner while the request is in flight", async () => {
    let resolve!: (r: { status: number; body: unknown }) => void;
    stubFetch(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );

    const page = document.createElement(
      "project-detail-page",
    ) as ProjectDetailPage;
    page.projectId = "pr1";
    document.body.appendChild(page);
    await page.updateComplete;

    expect(root(page).querySelector("loading-spinner")).not.toBeNull();
    expect(
      root(page).querySelector('h1.sr-only span[lang="en"]')?.textContent,
    ).toBe(el.titles.projectsDetail);

    resolve({ status: 200, body: sampleProject() });
    await flush(page);
  });

  test("renders the project fields, meta, and downloads from the detail API", async () => {
    const calls: string[] = [];
    stubFetch(async (url) => {
      calls.push(url);
      return { status: 200, body: sampleProject() };
    });

    const page = await renderPage("pr1");

    expect(calls).toEqual(["/api/v1/projects/pr1"]);

    // The lead block is the shared content-card: the page only wires its
    // detail inputs (the card's own suite pins the card's rendering).
    const hero = card(page);
    expect(hero.shape).toBe("detail");
    expect(hero.headingLevel).toBe(1);
    expect(hero.title).toBe("Τίτλος έργου");
    // Structural parity with the blog detail page: the projects subtype
    // never fills the subtitle slot.
    expect(hero.subtitle).toBe("");
    expect(cardRoot(page).querySelector(".subtitle")).toBeNull();
    expect(hero.description).toBe("Περιγραφή έργου");
    expect(hero.thumbnailUrl).toBe("https://example.com/t.jpg");
    expect(hero.publishedAt).toBe("2026-08-15T18:30:00.000Z");
    expect(hero.updatedAt).toBe("2026-08-15T18:30:00.000Z");

    // The card leads with the thumbnail and renders the title as the page's
    // h1.
    expect(
      root(page).querySelector(".detail")?.firstElementChild?.localName,
    ).toBe("content-card");
    expect(cardRoot(page).firstElementChild?.className).toContain("thumb");
    expect(cardRoot(page).querySelector("img.thumb")?.getAttribute("src")).toBe(
      "https://example.com/t.jpg",
    );
    expect(cardRoot(page).querySelectorAll("img").length).toBe(1);
    expect(text(cardRoot(page), "h1.title")).toBe("Τίτλος έργου");
    // The description keeps the blog-detail quote treatment; the closing
    // glyph stays inline at the end (template layout must never push it
    // onto its own line — the pre-line scope lives on the text span).
    expect(text(cardRoot(page), ".description")).toBe("❝ Περιγραφή έργου ❞");

    // Published meta with the creator byline; no updated line (updatedAt
    // is not strictly after publishedAt).
    const lines = [
      ...cardRoot(page).querySelectorAll(".detail-meta .meta-line"),
    ];
    expect(lines.length).toBe(1);
    expect(lines[0]!.textContent).toContain("Δημοσιεύτηκε");
    expect(lines[0]!.textContent).toContain("creator");
    // The NUMERIC Greek date, dash-separated from the
    // creator's name (which carries no «Από»).
    expect(text(lines[0] as HTMLElement, "time")).toBe(
      formatDateTimeNumeric("2026-08-15T18:30:00.000Z"),
    );
    expect(text(lines[0] as HTMLElement, ".meta-separator")).toBe("–");
    expect(text(lines[0] as HTMLElement, ".meta-byline")).toBe("creator");

    // Downloads: plain rows — name + links; the blog's layout toggle is
    // not ported.
    expect(root(page).querySelector(".layout-toggle")).toBeNull();
    const rows = [...root(page).querySelectorAll(".download-row")];
    expect(rows.length).toBe(2);
    expect(text(rows[0] as HTMLElement, ".download-name")).toBe("Batch 1");
    const links = [...rows[0]!.querySelectorAll("a")];
    expect(links.length).toBe(2);
    expect(links[0]!.getAttribute("href")).toBe("magnet:?xt=urn:btih:aaa");
    expect(links[0]!.textContent?.trim()).toBe("Magnet");
    // Deliberate anglicisms (catalog comment): rendered inside lang="en"
    // so assistive tech pronounces them correctly (same convention as the
    // blog detail page).
    expect(links[0]!.getAttribute("lang")).toBe("en");
    expect(links[1]!.getAttribute("href")).toBe(
      "https://example.com/t.torrent",
    );
    expect(links[1]!.getAttribute("lang")).toBe("en");
    expect(rows[1]!.querySelector("a")).toBeNull();

    // The page refines the shell's base title with the project title.
    expect(document.title).toContain("Τίτλος έργου");
  });

  test("renders no thumbnail when the wire masks it to null", async () => {
    stubFetch(async () => ({
      status: 200,
      body: sampleProject({ thumbnailUrl: null }),
    }));

    const page = await renderPage("pr1");

    // A masked (null) thumbnail keeps the card's no-image form — never a
    // broken src.
    expect(card(page).thumbnailUrl).toBeNull();
    expect(cardRoot(page).querySelector("img.thumb")).toBeNull();
    expect(root(page).querySelector("img")).toBeNull();
  });

  test("shows the updated meta only when updatedAt is strictly after publishedAt", async () => {
    stubFetch(async () => ({
      status: 200,
      body: sampleProject({
        updatedAt: "2026-08-20T10:00:00.000Z",
        updater: {
          id: "u2",
          username: "editor",
          avatarUrl: "https://example.com/e.png",
        },
      }),
    }));

    const page = await renderPage("pr1");

    // The page wires the strict comparison through the card's inputs; the
    // card renders the line.
    const hero = card(page);
    expect(hero.updatedAt).toBe("2026-08-20T10:00:00.000Z");
    expect(hero.updater?.username).toBe("editor");

    const lines = [
      ...cardRoot(page).querySelectorAll(".detail-meta .meta-line"),
    ];
    // The two-line shape: the updater keeps its own line and its own
    // avatar chip; the date form and the date/byline separator differ.
    expect(lines.length).toBe(2);
    const updatedLine = lines[1]!;
    expect(updatedLine.textContent).toContain("Ενημερώθηκε");
    expect(updatedLine.textContent).toContain("editor");
    expect(updatedLine.querySelector("img.avatar")?.getAttribute("src")).toBe(
      "https://example.com/e.png",
    );
    expect(
      [...updatedLine.querySelectorAll("time")].map((t) =>
        t.textContent?.trim(),
      ),
    ).toEqual([formatDateTimeNumeric("2026-08-20T10:00:00.000Z")]);
    expect(
      updatedLine.querySelector(".meta-separator")?.textContent?.trim(),
    ).toBe("–");
    expect(updatedLine.querySelector(".meta-byline")?.textContent?.trim()).toBe(
      "editor",
    );
  });

  test("renders no description block when the description is empty", async () => {
    stubFetch(async () => ({
      status: 200,
      body: sampleProject({ description: "" }),
    }));

    const page = await renderPage("pr1");

    // The block is conditionally rendered — no lone quote glyph and no
    // floating accent bar.
    expect(card(page).description).toBe("");
    expect(cardRoot(page).querySelector(".description")).toBeNull();
  });

  test("renders no downloads section when the project has no downloads", async () => {
    stubFetch(async () => ({
      status: 200,
      body: sampleProject({ downloads: [] }),
    }));

    const page = await renderPage("pr1");

    expect(root(page).querySelector(".downloads")).toBeNull();
  });

  test("shows the Greek not-found state for a masked 404", async () => {
    stubFetch(async () => ({
      status: 404,
      body: problemBody("/problems/not-found", 404),
    }));

    const page = await renderPage("unknown");

    expect(text(root(page), ".not-found-title")).toBe("Το project δε βρέθηκε.");
    const back = root(page).querySelector(".back-link");
    expect(back?.getAttribute("href")).toBe("/");
    expect(back?.textContent?.trim()).toBe("Επιστροφή στην αρχική");
  });

  test("shows the mapped error banner and recovers on retry", async () => {
    let calls = 0;
    stubFetch(async () => {
      calls++;
      if (calls === 1) {
        return {
          status: 500,
          body: problemBody("/problems/internal-error", 500),
        };
      }
      return { status: 200, body: sampleProject() };
    });

    const page = await renderPage("pr1");

    expect(bannerText(page)).toBe("Σφάλμα διακομιστή. Δοκιμάστε αργότερα.");
    expect(
      root(page).querySelector('h1.sr-only span[lang="en"]')?.textContent,
    ).toBe(el.titles.projectsDetail);

    root(page).querySelector<HTMLButtonElement>(".retry")!.click();
    await flush(page);

    expect(text(cardRoot(page), "h1.title")).toBe("Τίτλος έργου");
    expect(root(page).querySelector("error-banner")).toBeNull();
  });

  test("a stale in-flight response never paints after a projectId change", async () => {
    const resolvers: Array<(r: { status: number; body: unknown }) => void> = [];
    stubFetch(
      () =>
        new Promise((resolve) => {
          resolvers.push(resolve);
        }),
    );

    const page = await renderPage("pr1");
    page.projectId = "pr2";
    await page.updateComplete;

    // The second request resolves first with the newer project.
    resolvers[1]!({
      status: 200,
      body: sampleProject({ id: "pr2", title: "Δεύτερο έργο" }),
    });
    await flush(page);
    expect(text(cardRoot(page), "h1.title")).toBe("Δεύτερο έργο");

    // The aborted first request finally lands with stale data — the page
    // must ignore it: stale responses never paint.
    resolvers[0]!({
      status: 200,
      body: sampleProject({ id: "pr1", title: "Πρώτο έργο" }),
    });
    await flush(page);
    expect(text(cardRoot(page), "h1.title")).toBe("Δεύτερο έργο");
  });

  test("reloads when projectId changes without a full remount", async () => {
    const calls: string[] = [];
    stubFetch(async (url) => {
      calls.push(url);
      return { status: 200, body: sampleProject() };
    });

    const page = await renderPage("pr1");
    expect(text(cardRoot(page), "h1.title")).toBe("Τίτλος έργου");

    page.projectId = "pr2";
    await flush(page);

    // The comments section is click-to-load: the project requests are the
    // only ones at this point — the section fetches nothing before a click.
    expect(calls).toEqual(["/api/v1/projects/pr1", "/api/v1/projects/pr2"]);
  });

  test("embeds the click-to-load comments section and syncs its count", async () => {
    const calls: string[] = [];
    mockFetchImpl(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      calls.push(url);
      if (url.endsWith("/comments/count")) {
        return mockResponse({ ok: true, status: 200, jsonBody: { count: 9 } });
      }
      if (url.includes("/comments")) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            items: [],
            pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: sampleProject() });
    });

    const page = await renderPage("pr1");
    await flush(page);

    const section = root(page).querySelector<HTMLElement>("comments-section");
    expect(section).not.toBeNull();
    const sectionRoot = section!.shadowRoot!;
    expect(sectionRoot.querySelector(".cs-title")?.textContent?.trim()).toBe(
      "Σχόλια (1)",
    );
    expect(sectionRoot.querySelector("comments-thread")).toBeNull();
    expect(calls).toEqual(["/api/v1/projects/pr1"]);

    sectionRoot.querySelector<HTMLButtonElement>(".cs-cta")!.click();
    await flush(page);
    await new Promise((r) => setTimeout(r, 10));
    await page.updateComplete;

    expect(sectionRoot.querySelector("comments-thread")).not.toBeNull();
    // The projects endpoint is the one the thread asked — the kind handover
    // is proven by the request path, not by a constructor argument.
    expect(calls).toContain("/api/v1/projects/pr1/comments?sort=top&limit=20");
    // The count sync runs through the REAL path: the thread's count refetch
    // (stub returns 9) reaches the detail indicator via the window event — no
    // synthetic dispatch, so a broken producer/listener pairing fails here.
    expect(
      cardRoot(page).querySelector('.detail-stats [aria-label="9 σχόλια"]'),
    ).not.toBeNull();
  });

  test("a staff member sees the edit affordance; anonymous visitors don't", async () => {
    stubFetch(async () => ({ status: 200, body: sampleProject() }));

    // Anonymous (no session): no edit link — the client-side gate is
    // visibility only; the server enforces the real authorization.
    const anon = await renderPage("pr1");
    expect(root(anon).querySelector(".admin-edit-link")).toBeNull();

    // A moderator: the edit affordance renders beside the heart, pointing
    // at the staff edit form.
    const page = document.createElement(
      "project-detail-page",
    ) as ProjectDetailPage;
    page.projectId = "pr1";
    (page as any).session = {
      state: {
        status: "authenticated",
        message: "",
        user: { id: "u1", username: "Admin", role: "moderator" },
      },
      revalidate: mock(async () => {}),
    };
    document.body.appendChild(page);
    await flush(page);

    const link = root(page).querySelector(".admin-edit-link");
    expect(link).not.toBeNull();
    expect(link?.getAttribute("href")).toBe("/admin/projects/pr1");
    // Icon-only: the pencil is the visual, the accessible name and
    // the hover title carry the wording.
    expect(link?.textContent?.trim()).toBe("");
    expect(link?.querySelector("svg")).not.toBeNull();
    expect(link?.getAttribute("aria-label")).toBe(el.admin.edit);
    expect(link?.getAttribute("title")).toBe(el.admin.edit);
  });
});
