/**
 * Project form page tests — the create submit (no
 * slug), the edit load + If-Match submit with the admin+ slug field, the
 * 412 reload, and the delete confirm.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  listenForNavigation,
  mockFetchImpl,
  mockResponse,
  mockSessionContext,
  restoreMockFetch,
  stripCssComments,
  stubProperty,
} from "@shared/api/test-utils.js";

import "@features/auth/data-access/session.js";
import "./project-form-page.js";
import type { ProjectFormPage } from "./project-form-page.js";
import "./ui/download-rows.js";
import type { DownloadRows } from "./ui/download-rows.js";

function mockSession(role = "admin") {
  return mockSessionContext({ user: { role } });
}

async function render(
  projectId = "",
  role = "admin",
): Promise<ProjectFormPage> {
  const page = document.createElement("project-form-page") as ProjectFormPage;
  page.projectId = projectId;
  (page as any).session = mockSession(role);
  document.body.appendChild(page);
  await page.updateComplete;
  await settle();
  await page.updateComplete;
  return page;
}

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

function fill(page: ProjectFormPage, selector: string, value: string) {
  const input = page.shadowRoot?.querySelector(selector) as HTMLInputElement;
  input.value = value;
  input.dispatchEvent(new InputEvent("input"));
}

function submit(page: ProjectFormPage) {
  const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
  form.dispatchEvent(new Event("submit", { cancelable: true }));
}

/** Navigation listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

/** The confirm stub's restore, when a test installed one. */
let restoreConfirm: (() => void) | null = null;

/** The rendered <download-rows> widget (throws when absent). */
function downloadsWidget(page: ProjectFormPage): DownloadRows {
  const widget = page.shadowRoot?.querySelector(
    "download-rows",
  ) as DownloadRows | null;
  if (!widget) throw new Error("download-rows widget missing");
  return widget;
}

const PROJECT_RESPONSE = {
  id: "p1",
  title: "New Project",
  description: "",
  slug: "new-project",
  thumbnailPath: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
  thumbnailUrl:
    "https://fans.example/media/images/abcdef0123456789abcdef0123456789.jpg",
  status: "published",
  publishedAt: "2023-11-14T22:13:20.000Z",
  createdAt: "2023-11-14T22:13:20.000Z",
  updatedAt: "2023-11-14T22:13:20.000Z",
  revision: 1,
  creator: null,
  updater: null,
  downloads: [
    { name: "Batch", magnetUrl: "magnet:?xt=urn:btih:abc", torrentUrl: null },
  ],
};

beforeEach(() => {
  document.body.innerHTML = "";
});

afterEach(() => {
  for (const cleanup of navCleanups) cleanup();
  navCleanups.length = 0;
  restoreConfirm?.();
  restoreConfirm = null;
  restoreMockFetch();
});

describe("project-form-page", () => {
  test("create as a draft saves with status draft and lands on the list", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({
        ok: true,
        status: 201,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render();
    fill(page, "input[type=text]", "New Project");
    // The media upload widget feeds the path; inject one directly (the
    // widget's own tests cover its flow).
    const widget = page.shadowRoot?.querySelector("media-upload") as {
      _value: { path: string; url: string } | null;
    } | null;
    if (widget) {
      widget._value = {
        path: PROJECT_RESPONSE.thumbnailPath,
        url: PROJECT_RESPONSE.thumbnailUrl,
      };
    }
    // The downloads widget must carry at least one linked row (the create
    // starts with one empty row; load a filled one directly).
    downloadsWidget(page).load([
      {
        label: "Batch",
        magnetUrl: "magnet:?xt=urn:btih:abc",
        torrentUrl: null,
      },
    ]);
    await page.updateComplete;

    submit(page);
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"title":"New Project"');
    // The create body carries NO slug key — the server generates it.
    expect(bodies[0]?.includes("slug")).toBe(false);
    // The downloads array rides the create body (name label).
    expect(bodies[0]).toContain('"downloads":[{"name":"Batch"');
    // A plain save on a create lands as a draft — and a draft belongs on the
    // list, not on the public page.
    expect(bodies[0]).toContain('"status":"draft"');
    expect(nav.path).toBe("/admin/projects");
  });

  test("publishing on create posts status published and opens the public page", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({
        ok: true,
        status: 201,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render();
    fill(page, "input[type=text]", "New Project");
    const widget = page.shadowRoot?.querySelector("media-upload") as {
      _value: { path: string; url: string } | null;
    } | null;
    if (widget) {
      widget._value = {
        path: PROJECT_RESPONSE.thumbnailPath,
        url: PROJECT_RESPONSE.thumbnailUrl,
      };
    }
    downloadsWidget(page).load([
      {
        label: "Batch",
        magnetUrl: "magnet:?xt=urn:btih:abc",
        torrentUrl: null,
      },
    ]);
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".action-publish") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"published"');
    expect(nav.path).toBe("/projects/p1");
  });

  test("create without a title shows the required error and sends nothing", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return mockResponse({
        ok: true,
        status: 201,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render();
    submit(page);
    await settle();
    await page.updateComplete;

    const input = page.shadowRoot?.querySelector(
      "#content-title",
    ) as HTMLInputElement;
    const error = page.shadowRoot?.querySelector("#content-title-error");
    expect(error?.textContent?.trim()).toBe(el.violations.required);
    // The field-level shape: the control is marked invalid, described by
    // the error line, and holds focus (the auth-form contract).
    expect(input?.getAttribute("aria-invalid")).toBe("true");
    expect(input?.getAttribute("aria-describedby")).toBe("content-title-error");
    expect(page.shadowRoot?.activeElement).toBe(input);
    // The error is a SIBLING of the label: inside it, the text would join
    // the input's accessible name on top of aria-describedby.
    expect(error?.closest("label")).toBeNull();
    expect(
      page.shadowRoot?.querySelector('label[for="content-title"]'),
    ).not.toBeNull();
    expect(calls).toBe(0);
  });

  test("a download row without any link blocks the submit client-side", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return mockResponse({
        ok: true,
        status: 201,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render();
    fill(page, "input[type=text]", "New Project");
    const widget = page.shadowRoot?.querySelector("media-upload") as {
      _value: { path: string; url: string } | null;
    } | null;
    if (widget) {
      widget._value = {
        path: PROJECT_RESPONSE.thumbnailPath,
        url: PROJECT_RESPONSE.thumbnailUrl,
      };
    }
    // The create form's default row is empty — no links, so the submit is
    // blocked before any request.
    submit(page);
    await settle();
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector(".field-error")).not.toBeNull();
    expect(calls).toBe(0);
  });

  test("an admin edit shows the slug field and saves with the row's state", async () => {
    const nav = listenForNavigation(navCleanups);
    const calls: Array<{ method?: string; ifMatch?: string; body?: string }> =
      [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        method: init?.method,
        ifMatch: (init?.headers as Record<string, string>)["If-Match"],
        body: init?.body ? String(init.body) : undefined,
      });
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    // The success paragraph is permanently mounted with its live-region
    // role, so a later save announces; only the text inside it changes.
    const successLine = page.shadowRoot?.querySelector(".form-success");
    expect(successLine).not.toBeNull();
    expect(successLine?.getAttribute("role")).toBe("status");
    expect(successLine?.textContent?.trim()).toBe("");
    // One heading, visible in the ready state.
    expect(page.shadowRoot?.querySelectorAll("h1").length).toBe(1);

    const slugInput = page.shadowRoot?.querySelectorAll(
      ".field input[type=text]",
    );
    // The slug field renders for an admin (title + slug = two text inputs).
    expect(slugInput?.length).toBe(2);
    // The loaded row's downloads populate the widget.
    expect(downloadsWidget(page).getValue()[0]?.label).toBe("Batch");

    submit(page);
    await settle();
    await page.updateComplete;

    expect(calls.map((c) => c.method)).toEqual(["GET", "PUT"]);
    expect(calls[1]?.ifMatch).toBe('"1"');
    expect(calls[1]?.body).toContain('"slug":"new-project"');
    // The round-tripped download rows ride the PUT body (full replacement).
    expect(calls[1]?.body).toContain('"downloads":[{"name":"Batch"');
    // A plain save keeps a published row published — and opens its page.
    expect(calls[1]?.body).toContain('"status":"published"');
    expect(nav.path).toBe("/projects/p1");
  });

  test("a published project offers unpublish and archive instead of publish", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector(".state-chip")
        ?.getAttribute("data-status"),
    ).toBe("published");
    expect(page.shadowRoot?.querySelector(".action-publish")).toBeNull();

    (
      page.shadowRoot?.querySelector(".action-unpublish") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    expect(bodies[0]).toContain('"status":"draft"');
    // Unpublished: the form stays put with the success line.
    const successLine = page.shadowRoot?.querySelector(".form-success");
    expect(successLine?.getAttribute("role")).toBe("status");
    expect(successLine?.textContent?.trim()).toBe(el.admin.savedProject);
    expect(nav.path).toBeNull();
  });

  test("archiving a published project writes archived and returns to the list", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".action-archive") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    expect(bodies[0]).toContain('"status":"archived"');
    expect(nav.path).toBe("/admin/projects");
  });

  test("a failed write leaves the chip on the row's real state, and the next write keeps it", async () => {
    const bodies: string[] = [];
    let fail = true;
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PUT") {
        bodies.push(String(init.body));
        if (fail) {
          return mockResponse({
            ok: false,
            status: 500,
            headers: { "Content-Type": "application/problem+json" },
            jsonBody: {
              type: "/problems/internal-error",
              title: "Internal error",
              status: 500,
            },
          });
        }
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".action-unpublish") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector(".state-chip")
        ?.getAttribute("data-status"),
    ).toBe("published");
    expect(page.shadowRoot?.querySelector(".action-unpublish")).not.toBeNull();

    bodies.length = 0;
    fail = false;
    submit(page);
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"published"');
  });

  test("a save during an in-flight upload writes nothing and says why", async () => {
    const calls: string[] = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(init?.method ?? "GET");
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;
    calls.length = 0;

    const widget = page.shadowRoot?.querySelector("media-upload") as {
      isUploading: () => boolean;
    } | null;
    if (!widget) throw new Error("media-upload widget missing");
    widget.isUploading = () => true;

    submit(page);
    await settle();
    await page.updateComplete;

    expect(calls).toEqual([]);
    expect(
      page.shadowRoot?.querySelector(".field-error")?.textContent?.trim(),
    ).toBe(el.admin.uploadInFlight);
  });

  test("a moderator edit hides the slug field and omits it from the body", async () => {
    const calls: Array<{ method?: string; body?: string }> = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        method: init?.method,
        body: init?.body ? String(init.body) : undefined,
      });
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1", "moderator");
    await settle();
    await page.updateComplete;

    // Only the title text input renders — no slug field for a moderator.
    expect(
      page.shadowRoot?.querySelectorAll(".field input[type=text]").length,
    ).toBe(1);
    // Delete is admin+: no delete button either.
    expect(page.shadowRoot?.querySelector(".delete-button")).toBeNull();

    submit(page);
    await settle();
    await page.updateComplete;

    expect(calls.map((c) => c.method)).toEqual(["GET", "PUT"]);
    expect(calls[1]?.body?.includes("slug")).toBe(false);
    // The downloads still ride the moderator's PUT body.
    expect(calls[1]?.body).toContain('"downloads":[{"name":"Batch"');
  });

  test("a 412 on save reloads the project and shows the stale copy", async () => {
    const calls: string[] = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(init?.method ?? "GET");
      if (init?.method === "PUT") {
        return mockResponse({
          ok: false,
          status: 412,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: {
            type: "/problems/precondition-failed",
            title: "Precondition failed",
            status: 412,
          },
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    submit(page);
    await settle();
    await page.updateComplete;
    await settle();

    // The 412 triggers a reload (GET) and the Greek stale copy.
    expect(calls.filter((c) => c === "GET").length).toBe(2);
    expect(
      page.shadowRoot?.querySelector(".form-error")?.textContent?.trim(),
    ).toBe(el.admin.staleProject);
  });

  test("a 412 on delete shows the project stale copy", async () => {
    restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    const calls: string[] = [];
    mockFetchImpl(async (_input, init) => {
      calls.push(init?.method ?? "GET");
      if (init?.method === "DELETE") {
        return mockResponse({
          ok: false,
          status: 412,
          headers: { "Content-Type": "application/problem+json" },
          jsonBody: {
            type: "/problems/precondition-failed",
            title: "Precondition failed",
            status: 412,
          },
        });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(
        ".delete-button",
      ) as HTMLButtonElement | null
    )?.click();
    await settle();
    await page.updateComplete;
    await settle();

    // The delete path rides the same per-kind override as the save path.
    expect(calls).toContain("DELETE");
    expect(
      page.shadowRoot?.querySelector(".form-error")?.textContent?.trim(),
    ).toBe(el.admin.staleProject);
  });

  test("delete confirms, sends If-Match, and navigates to /admin/projects", async () => {
    const nav = listenForNavigation(navCleanups);
    restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    const calls: Array<{ method?: string; ifMatch?: string }> = [];
    mockFetchImpl(async (_input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        method: init?.method ?? "GET",
        ifMatch: (init?.headers as Record<string, string> | undefined)?.[
          "If-Match"
        ],
      });
      if (init?.method === "DELETE") {
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      }
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    const del = page.shadowRoot?.querySelector(
      ".delete-button",
    ) as HTMLButtonElement | null;
    del?.click();
    await settle();
    await page.updateComplete;

    const deleteCall = calls.find((call) => call.method === "DELETE");
    expect(deleteCall).toBeDefined();
    // The loaded revision rides the If-Match precondition.
    expect(deleteCall?.ifMatch).toBe('"1"');
    expect(nav.path).toBe("/admin/projects");
  });

  test("a moderator on the create route sees the forbidden state and no request starts", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return mockResponse({
        ok: true,
        status: 200,
        jsonBody: PROJECT_RESPONSE,
      });
    });

    const page = await render("", "moderator");

    // The guard's forbidden state is final: the create form never flashes
    // for a below-floor viewer, and no request is made on their behalf.
    expect(page.shadowRoot?.querySelector("form")).toBeNull();
    expect(page.shadowRoot?.textContent).toContain(
      el.problems["/problems/forbidden"],
    );
    expect(page.shadowRoot?.querySelector("h1")?.textContent?.trim()).toBe(
      el.admin.formTitleCreateProject,
    );
    expect(calls).toBe(0);
  });

  test("a not-found load keeps a heading landmark", async () => {
    mockFetchImpl(async () =>
      mockResponse({
        ok: false,
        status: 404,
        headers: { "Content-Type": "application/problem+json" },
        jsonBody: {
          type: "/problems/not-found",
          title: "Not found",
          status: 404,
        },
      }),
    );

    const page = await render("p1");

    expect(page.shadowRoot?.querySelector("h1")?.textContent?.trim()).toBe(
      el.admin.formTitleEditProject,
    );
  });

  test("an error load keeps a heading landmark", async () => {
    mockFetchImpl(async () =>
      mockResponse({
        ok: false,
        status: 500,
        headers: { "Content-Type": "application/problem+json" },
        jsonBody: {
          type: "/problems/internal-error",
          title: "Internal error",
          status: 500,
        },
      }),
    );

    const page = await render("p1");

    expect(
      page.shadowRoot?.querySelector("h1.sr-only")?.textContent?.trim(),
    ).toBe(el.admin.formTitleEditProject);
  });

  test("the form header and the action row wrap instead of overflowing", async () => {
    // The blog sheet's rules, mirrored here (the two sheets copy each other
    // wholesale): the header's three items and the four action buttons must
    // wrap on a phone. happy-dom resolves no CSS, so this is a source pin.
    const styles = stripCssComments(
      await Bun.file(
        new URL("./project-form-page.css", import.meta.url),
      ).text(),
    );
    const header = styles.match(/\.form-header \{([^}]*)\}/s)?.[1] ?? "";
    expect(header).toContain("flex-wrap: wrap");
    expect(styles).toMatch(/\.form-header > a \{[^}]*margin-left: auto/);

    const actions = styles.match(/\.form-actions \{([^}]*)\}/s)?.[1] ?? "";
    expect(actions).toContain("flex-wrap: wrap");
  });
});
