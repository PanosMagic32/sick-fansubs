/**
 * Blog form page tests — the create submit, the
 * edit load + If-Match submit, the 412 reload, and the delete confirm.
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
import "./blog-form-page.js";
import type { BlogFormPage } from "./blog-form-page.js";
import "./ui/download-rows.js";
import type { DownloadRows } from "./ui/download-rows.js";

function mockSession(
  role = "admin",
  status: "authenticated" | "error" = "authenticated",
) {
  return mockSessionContext({ status, user: { role } });
}

async function render(postId = "", role = "admin"): Promise<BlogFormPage> {
  const page = document.createElement("blog-form-page") as BlogFormPage;
  page.postId = postId;
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

function fill(page: BlogFormPage, selector: string, value: string) {
  const input = page.shadowRoot?.querySelector(selector) as HTMLInputElement;
  input.value = value;
  input.dispatchEvent(new InputEvent("input"));
}

function submit(page: BlogFormPage) {
  const form = page.shadowRoot?.querySelector("form") as HTMLFormElement;
  form.dispatchEvent(new Event("submit", { cancelable: true }));
}

/** Navigation listeners registered by a test — removed in afterEach (a
 * leaked window listener outlives the shared happy-dom worker). */
const navCleanups: Array<() => void> = [];

/** The confirm stub's restore, when a test installed one. */
let restoreConfirm: (() => void) | null = null;

/** The rendered <download-rows> widget (throws when absent). */
function downloadsWidget(page: BlogFormPage): DownloadRows {
  const widget = page.shadowRoot?.querySelector(
    "download-rows",
  ) as DownloadRows | null;
  if (!widget) throw new Error("download-rows widget missing");
  return widget;
}

const POST_RESPONSE = {
  id: "p1",
  title: "New Post",
  subtitle: "",
  description: "",
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
    {
      resolution: "1080p",
      magnetUrl: "magnet:?xt=urn:btih:abc",
      torrentUrl: null,
    },
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
  document.body.innerHTML = "";
});

describe("blog-form-page", () => {
  test("create as a draft saves with status draft and lands on the list", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input, init) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({ ok: true, status: 201, jsonBody: POST_RESPONSE });
    });

    const page = await render();
    fill(page, "input[type=text]", "New Post");
    // The media upload widget feeds the path; inject one directly (the
    // widget's own tests cover its flow).
    const widget = page.shadowRoot?.querySelector("media-upload") as {
      _value: { path: string; url: string } | null;
    } | null;
    if (widget) {
      widget._value = {
        path: POST_RESPONSE.thumbnailPath,
        url: POST_RESPONSE.thumbnailUrl,
      };
    }
    // The downloads widget must carry at least one linked row (the create
    // starts with one empty row; load a filled one directly).
    downloadsWidget(page).load([
      {
        label: "1080p",
        magnetUrl: "magnet:?xt=urn:btih:abc",
        torrentUrl: null,
      },
    ]);
    await page.updateComplete;

    // Enter (implicit submit) runs the SAFE action — a plain save, which for
    // a create means a draft.
    submit(page);
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"draft"');
    expect(bodies[0]).toContain('"downloads":[{"resolution":"1080p"');
    expect(bodies[0]).toContain('"magnetUrl":"magnet:?xt=urn:btih:abc"');
    expect(bodies[0]).toContain('"torrentUrl":null');
    // A draft belongs on the list, not on the public page.
    expect(nav.path).toBe("/admin");
  });

  test("publishing on create posts status published and opens the public page", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input, init) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({ ok: true, status: 201, jsonBody: POST_RESPONSE });
    });

    const page = await render();
    fill(page, "input[type=text]", "New Post");
    const widget = page.shadowRoot?.querySelector("media-upload") as {
      _value: { path: string; url: string } | null;
    } | null;
    if (widget) {
      widget._value = {
        path: POST_RESPONSE.thumbnailPath,
        url: POST_RESPONSE.thumbnailUrl,
      };
    }
    downloadsWidget(page).load([
      {
        label: "1080p",
        magnetUrl: "magnet:?xt=urn:btih:abc",
        torrentUrl: null,
      },
    ]);
    await page.updateComplete;

    // The create form's chip states the default outcome; nothing has been
    // written yet.
    const chip = page.shadowRoot?.querySelector(".state-chip");
    expect(chip?.getAttribute("data-status")).toBe("draft");

    (
      page.shadowRoot?.querySelector(".action-publish") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"published"');
    expect(nav.path).toBe("/blog/p1");
  });

  test("create without a title shows the required error and sends nothing", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return mockResponse({ ok: true, status: 201, jsonBody: POST_RESPONSE });
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
      return mockResponse({ ok: true, status: 201, jsonBody: POST_RESPONSE });
    });

    const page = await render();
    fill(page, "input[type=text]", "New Post");
    const widget = page.shadowRoot?.querySelector("media-upload") as {
      _value: { path: string; url: string } | null;
    } | null;
    if (widget) {
      widget._value = {
        path: POST_RESPONSE.thumbnailPath,
        url: POST_RESPONSE.thumbnailUrl,
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

  test("edit saves with the row's own state and opens the public page", async () => {
    const nav = listenForNavigation(navCleanups);
    const calls: Array<{ method?: string; ifMatch?: string; body?: string }> =
      [];
    mockFetchImpl(async (_input, init) => {
      calls.push({
        method: init?.method,
        ifMatch: (init?.headers as Record<string, string>)["If-Match"],
        body: init?.body ? String(init.body) : undefined,
      });
      if (init?.method === "GET") {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: POST_RESPONSE,
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    // The loaded row's downloads populate the widget.
    expect(downloadsWidget(page).getValue()[0]?.label).toBe("1080p");

    submit(page);
    await settle();
    await page.updateComplete;

    expect(calls.map((c) => c.method)).toEqual(["GET", "PUT"]);
    expect(calls[1]?.ifMatch).toBe('"1"');
    // The round-tripped download rows ride the PUT body (full replacement).
    expect(calls[1]?.body).toContain('"downloads":[{"resolution":"1080p"');
    // A plain save keeps the row where it is — published stays published.
    expect(calls[1]?.body).toContain('"status":"published"');
    // …and a published row's save opens the public page.
    expect(nav.path).toBe("/blog/p1");
  });

  test("a published row offers the two ways off the public site", async () => {
    const bodies: string[] = [];
    mockFetchImpl(async (_input, init) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    // The chip states the row's state, and the alternatives are unpublish /
    // archive instead of publish.
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

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"draft"');
    // Unpublished: the form stays put with the success line.
    expect(
      page.shadowRoot?.querySelector(".form-success")?.textContent?.trim(),
    ).toBe(el.admin.saved);
  });

  test("archiving a published row writes archived and returns to the list", async () => {
    const nav = listenForNavigation(navCleanups);
    const bodies: string[] = [];
    mockFetchImpl(async (_input, init) => {
      if (init?.body) bodies.push(String(init.body));
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".action-archive") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"archived"');
    expect(nav.path).toBe("/admin");
  });

  test("a draft row's save stays on the form with the success line", async () => {
    const draft = { ...POST_RESPONSE, status: "draft", publishedAt: null };
    const nav = listenForNavigation(navCleanups);
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: draft }),
    );

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot
        ?.querySelector(".state-chip")
        ?.getAttribute("data-status"),
    ).toBe("draft");
    expect(page.shadowRoot?.querySelector(".action-publish")).not.toBeNull();

    submit(page);
    await settle();
    await page.updateComplete;

    const success = page.shadowRoot?.querySelector(".form-success");
    expect(success?.textContent?.trim()).toBe(el.admin.saved);
    expect(success?.getAttribute("role")).toBe("status");
    expect(nav.path).toBeNull();
  });

  test("a 412 on save reloads the post and shows the stale copy", async () => {
    const calls: string[] = [];
    let gets = 0;
    mockFetchImpl(async (_input, init) => {
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
      gets += 1;
      // The reload returns a DIFFERENT row, so the widget's re-population is
      // observable.
      if (gets > 1) {
        return mockResponse({
          ok: true,
          status: 200,
          jsonBody: {
            ...POST_RESPONSE,
            downloads: [
              {
                resolution: "2160p",
                magnetUrl: null,
                torrentUrl: "https://fans.example/2160.torrent",
              },
            ],
          },
        });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
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
    ).toBe(el.problems["/problems/precondition-failed"]);
    // …and the reloaded row re-populates the download widget (the pending
    // rows are consumed once the ready render exists).
    expect(downloadsWidget(page).getValue()[0]?.label).toBe("2160p");
  });

  test("a failed write leaves the chip on the row's real state, and the next write keeps it", async () => {
    const bodies: string[] = [];
    let fail = true;
    mockFetchImpl(async (_input, init) => {
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
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    (
      page.shadowRoot?.querySelector(".action-unpublish") as HTMLButtonElement
    )?.click();
    await settle();
    await page.updateComplete;

    // The write failed, so the row is still published — and the chip says so.
    expect(
      page.shadowRoot
        ?.querySelector(".state-chip")
        ?.getAttribute("data-status"),
    ).toBe("published");
    expect(page.shadowRoot?.querySelector(".action-unpublish")).not.toBeNull();

    // The next save carries the row's OWN state — not the failed target.
    bodies.length = 0;
    fail = false;
    submit(page);
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain('"status":"published"');
  });

  test("the save control is the form's submit button, so Enter runs the safe save", async () => {
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE }),
    );

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    expect(
      page.shadowRoot?.querySelector(".action-save")?.getAttribute("type"),
    ).toBe("submit");
    // ONE submit control: Enter cannot mean anything but the safe save.
    expect(
      page.shadowRoot?.querySelectorAll('button[type="submit"]').length,
    ).toBe(1);
  });

  test("with no new file, the row's stored thumbnail rides the write", async () => {
    const bodies: string[] = [];
    mockFetchImpl(async (_input, init) => {
      if (init?.method === "PUT") bodies.push(String(init.body));
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;

    submit(page);
    await settle();
    await page.updateComplete;

    expect(bodies.length).toBe(1);
    expect(bodies[0]).toContain(
      `"thumbnailPath":"${POST_RESPONSE.thumbnailPath}"`,
    );
  });

  test("a save during an in-flight upload writes nothing and says why", async () => {
    const calls: string[] = [];
    mockFetchImpl(async (_input, init) => {
      calls.push(init?.method ?? "GET");
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1");
    await settle();
    await page.updateComplete;
    calls.length = 0;

    // The widget reports a file still in flight: its value holds the
    // PREVIOUS thumbnail, so a write now would submit the wrong file.
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

  test("a moderator edit hides the delete button (admin+ floor)", async () => {
    mockFetchImpl(async () =>
      mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE }),
    );

    const page = await render("p1", "moderator");
    await settle();
    await page.updateComplete;

    expect(page.shadowRoot?.querySelector(".delete-button")).toBeNull();
    // The edit surface itself stays available to a moderator, with exactly
    // one heading.
    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
    expect(page.shadowRoot?.querySelectorAll("h1").length).toBe(1);
  });

  test("delete confirms, sends If-Match, and navigates to /admin", async () => {
    const nav = listenForNavigation(navCleanups);
    restoreConfirm = stubProperty(
      window,
      "confirm",
      mock(() => true),
    );
    const calls: string[] = [];
    mockFetchImpl(async (_input, init) => {
      calls.push(init?.method ?? "GET");
      if (init?.method === "DELETE") {
        return mockResponse({ ok: true, status: 204, jsonBody: "" });
      }
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
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

    expect(calls).toContain("DELETE");
    expect(nav.path).toBe("/admin");
  });

  test("a moderator on the create route sees the forbidden state and no row load starts", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("", "moderator");

    // The guard's forbidden state is final: the create form never flashes
    // for a below-floor viewer, and no request is made on their behalf.
    expect(page.shadowRoot?.querySelector("form")).toBeNull();
    expect(page.shadowRoot?.textContent).toContain(
      el.problems["/problems/forbidden"],
    );
    expect(page.shadowRoot?.querySelector("h1")?.textContent?.trim()).toBe(
      el.admin.formTitleCreate,
    );
    expect(calls).toBe(0);
  });

  test("a below-floor edit makes no row request and shows forbidden", async () => {
    let calls = 0;
    mockFetchImpl(async () => {
      calls++;
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = await render("p1", "user");

    expect(page.shadowRoot?.textContent).toContain(
      el.problems["/problems/forbidden"],
    );
    expect(page.shadowRoot?.querySelector("h1")?.textContent?.trim()).toBe(
      el.admin.formTitleEdit,
    );
    expect(calls).toBe(0);
  });

  test("a session error that recovers starts exactly one row load", async () => {
    const calls: string[] = [];
    mockFetchImpl(async (_input, init) => {
      calls.push(init?.method ?? "GET");
      return mockResponse({ ok: true, status: 200, jsonBody: POST_RESPONSE });
    });

    const page = document.createElement("blog-form-page") as BlogFormPage;
    page.postId = "p1";
    (page as any).session = mockSession("admin", "error");
    document.body.appendChild(page);
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    // The session-error state makes no request and latches nothing.
    expect(calls).toEqual([]);

    (page as any).session = mockSession("admin", "authenticated");
    page.requestUpdate();
    await page.updateComplete;
    await settle();
    await page.updateComplete;

    expect(calls).toEqual(["GET"]);
    expect(page.shadowRoot?.querySelector("form")).not.toBeNull();
  });

  test("the notFound and error states keep a heading landmark", async () => {
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
    const notFound = await render("missing");
    expect(notFound.shadowRoot?.querySelector("h1")?.textContent?.trim()).toBe(
      el.admin.formTitleEdit,
    );

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
    const failed = await render("p1");
    expect(failed.shadowRoot?.querySelector("h1")?.textContent?.trim()).toBe(
      el.admin.formTitleEdit,
    );
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
      const page = await render("p1");
      expect(capture.signal).not.toBeNull();

      // The request carries the composition of the page's own controller and
      // the client deadline: aborting the timeout arm alone must abort it.
      // The pre-fix raw controller.signal would survive this.
      timeoutArm.abort();
      expect(capture.signal!.aborted).toBe(true);
      page.remove();
    } finally {
      restoreTimeout();
    }
  });

  test("the form header and the action row wrap instead of overflowing", async () => {
    // The owner's "content overflow": the header held a wrapped title, a
    // nowrap state chip, and the back link whose min-contents exceed a phone
    // width, and the four action buttons were one unshrinkable line. Both rows
    // wrap now. happy-dom resolves no CSS, so this is a source pin.
    const styles = stripCssComments(
      await Bun.file(new URL("./blog-form-page.css", import.meta.url)).text(),
    );
    const header = styles.match(/\.form-header \{([^}]*)\}/s)?.[1] ?? "";
    expect(header).toContain("flex-wrap: wrap");
    // The back link still hugs the right edge, on its own line when it wraps.
    expect(styles).toMatch(/\.form-header > a \{[^}]*margin-left: auto/);

    const actions = styles.match(/\.form-actions \{([^}]*)\}/s)?.[1] ?? "";
    expect(actions).toContain("flex-wrap: wrap");
  });
});
