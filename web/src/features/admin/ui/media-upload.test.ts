/**
 * Media upload widget tests — staff-only visibility, choose-IS-upload, the
 * visible status line, confirmed updates, error copy, the speculative delete on
 * remove, and the value contract for the create/edit form.
 */

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  mockResponse,
  mockSessionContext,
  restoreMockFetch,
  stubProperty,
} from "@shared/api/test-utils.js";
import { setCSRFToken, clearCSRFToken } from "@shared/api/client.js";

import "./media-upload.js";
import type { MediaUpload } from "./media-upload.js";

// ── Helpers ─────────────────────────────────────────────────────────

const RESULT = {
  path: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
  url: "http://localhost:3000/media/images/abcdef0123456789abcdef0123456789.jpg",
};

/** The URL's last segment — the `file` the widget deletes. */
const RESULT_FILE = "abcdef0123456789abcdef0123456789.jpg";

function mockSession(role: string) {
  return mockSessionContext({ user: { role } });
}

async function render(role: string): Promise<MediaUpload> {
  const widget = document.createElement("media-upload") as MediaUpload;
  (widget as any).session = mockSession(role);
  document.body.appendChild(widget);
  await widget.updateComplete;
  await settle();
  await widget.updateComplete;
  return widget;
}

async function settle(): Promise<void> {
  await new Promise((r) => setTimeout(r, 10));
}

function chooseFile(widget: MediaUpload, file: File) {
  const input =
    widget.shadowRoot?.querySelector<HTMLInputElement>("input[type=file]");
  if (!input) throw new Error("file input missing");
  Object.defineProperty(input, "files", { value: [file], configurable: true });
  input.dispatchEvent(new Event("change"));
}

function button(
  widget: MediaUpload,
  selector: string,
): HTMLButtonElement | null {
  return widget.shadowRoot?.querySelector(selector) ?? null;
}

function preview(widget: MediaUpload): HTMLImageElement | null {
  return widget.shadowRoot?.querySelector("img") ?? null;
}

function statusText(widget: MediaUpload): string {
  return (
    widget.shadowRoot?.querySelector(".media-status")?.textContent?.trim() ?? ""
  );
}

/** Choose a file and let the auto-upload settle. */
async function chooseAndUpload(
  widget: MediaUpload,
  name = "thumb.jpg",
): Promise<void> {
  chooseFile(widget, new File(["img"], name, { type: "image/jpeg" }));
  await settle();
  await widget.updateComplete;
}

const MOCK_OBJECT_URL = "blob:mock-preview";

let restoreURLMocks: Array<() => void> = [];

beforeEach(() => {
  document.body.innerHTML = "";
  // The object-URL stubs shadow window properties on the SHARED happy-dom
  // window — every install must pair with its restore (testing rule 10).
  restoreURLMocks = [
    stubProperty(
      URL,
      "createObjectURL",
      mock(() => MOCK_OBJECT_URL),
    ),
    stubProperty(
      URL,
      "revokeObjectURL",
      mock(() => {}),
    ),
  ];
});

afterEach(() => {
  for (const restore of restoreURLMocks) restore();
  restoreURLMocks = [];
  restoreMockFetch();
  clearCSRFToken();
});

// ── Visibility ──────────────────────────────────────────────────────

describe("media-upload", () => {
  test("renders nothing for a user or moderator", async () => {
    for (const role of ["user", "moderator"]) {
      const widget = await render(role);
      expect(widget.shadowRoot?.querySelector(".media-upload")).toBeNull();
      widget.remove();
    }
  });

  test("renders the picker for an admin, with the empty status line", async () => {
    const widget = await render("admin");
    expect(widget.shadowRoot?.querySelector("input[type=file]")).not.toBeNull();
    expect(statusText(widget)).toBe(el.admin.uploadStatusNone);
  });

  // ── Auto-upload flow ─────────────────────────────────────────────

  test("choosing a file uploads it at once — no second press, value stored", async () => {
    const widget = await render("admin");
    const eventSpy = mock(() => {});
    widget.addEventListener(
      "media-upload",
      eventSpy as unknown as EventListener,
    );

    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      jsonBody: RESULT,
    });

    await chooseAndUpload(widget);

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/media");
    expect(preview(widget)?.getAttribute("src")).toBe(RESULT.url);
    expect(widget.getValue()).toEqual(RESULT);
    expect(eventSpy).toHaveBeenCalledTimes(1);

    // The upload button is GONE: picking is uploading, and a chosen file can
    // therefore never be left unuploaded at submit (the old silent-discard
    // trap).
    expect(button(widget, ".media-upload-button")).toBeNull();
    expect(statusText(widget)).toBe(el.admin.uploadStatusUploaded);
    expect(
      widget.shadowRoot
        ?.querySelector(".media-status")
        ?.classList.contains("is-uploaded"),
    ).toBe(true);
  });

  test("the status line reports the in-flight upload and disables the picker", async () => {
    const widget = await render("admin");

    // The POST never settles — the pending state stays visible.
    mockFetchImpl(() => new Promise<Response>(() => {}));

    chooseFile(widget, new File(["img"], "a.jpg", { type: "image/jpeg" }));
    await widget.updateComplete;

    expect(statusText(widget)).toBe(el.admin.uploadStatusUploading);
    expect(
      widget.shadowRoot?.querySelector<HTMLInputElement>("input[type=file]")
        ?.disabled,
    ).toBe(true);
    expect(widget.getValue()).toBeNull();
  });

  test("a new choice aborts the upload in flight", async () => {
    const widget = await render("admin");

    // The first upload never settles: the second choice must ABORT it, so a
    // stale response can never overwrite the newer value.
    const seen: { signal?: AbortSignal | null } = {};
    let call = 0;
    mockFetchImpl(async (_input, init) => {
      call += 1;
      if (call === 1) {
        seen.signal = init?.signal ?? null;
        return new Promise<Response>(() => {});
      }
      return mockResponse({
        ok: true,
        status: 201,
        jsonBody: {
          path: "media/images/cd/cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd.jpg",
          url: "http://localhost:3000/media/images/cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd.jpg",
        },
      });
    });

    chooseFile(widget, new File(["a"], "a.jpg", { type: "image/jpeg" }));
    await widget.updateComplete;
    chooseFile(widget, new File(["b"], "b.jpg", { type: "image/jpeg" }));
    await settle();
    await widget.updateComplete;

    expect(seen.signal?.aborted).toBe(true);
    expect(widget.getValue()?.path).toContain("cdcdcdcd");
    expect(statusText(widget)).toBe(el.admin.uploadStatusUploaded);
  });

  test("a 413 shows the too-large copy and keeps the previous value", async () => {
    const widget = await render("admin");

    // First upload succeeds — establishes the value.
    mockFetchOnce({ ok: true, status: 201, jsonBody: RESULT });
    await chooseAndUpload(widget, "a.jpg");
    expect(widget.getValue()).toEqual(RESULT);

    // Replacement upload fails with 413 — the old value stands (confirmed
    // updates), and the status line goes back to saying so.
    mockFetchOnce({
      ok: false,
      status: 413,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 413,
        violations: [{ field: "file", code: "tooLarge" }],
      },
    });
    await chooseAndUpload(widget, "b.jpg");

    expect(widget.getValue()).toEqual(RESULT);
    expect(
      widget.shadowRoot?.querySelector(".media-error")?.textContent?.trim(),
    ).toBe(el.admin.uploadTooLarge);
    expect(preview(widget)?.getAttribute("src")).toBe(RESULT.url);
    expect(statusText(widget)).toBe(el.admin.uploadStatusUploaded);
  });

  test("a 422 shows the invalid-format copy and clears the local preview", async () => {
    const widget = await render("admin");

    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [{ field: "file", code: "invalidFormat" }],
      },
    });
    await chooseAndUpload(widget, "x.jpg");

    expect(widget.getValue()).toBeNull();
    expect(
      widget.shadowRoot?.querySelector(".media-error")?.textContent?.trim(),
    ).toBe(el.admin.uploadInvalid);
    // A failed upload leaves no preview of a file the server never stored.
    expect(preview(widget)).toBeNull();
    expect(statusText(widget)).toBe(el.admin.uploadStatusNone);
  });

  // ── Remove + speculative delete ──────────────────────────────────

  test("removing an uploaded image deletes the file and clears the value", async () => {
    setCSRFToken("tok");
    const widget = await render("admin");

    mockFetchOnce({ ok: true, status: 201, jsonBody: RESULT });
    await chooseAndUpload(widget);
    expect(widget.getValue()).toEqual(RESULT);

    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });
    button(widget, ".media-remove-button")?.click();
    await settle();
    await widget.updateComplete;

    // The `file` is the LAST segment of the served URL — never hand-built.
    expect(req.method).toBe("DELETE");
    expect(req.url).toBe(`/api/v1/media/${RESULT_FILE}`);
    expect(req.headers["x-csrf-token"]).toBe("tok");
    expect(widget.getValue()).toBeNull();
    expect(preview(widget)).toBeNull();
    expect(statusText(widget)).toBe(el.admin.uploadStatusNone);
  });

  test("a refused delete (409, still referenced) is silent and clears locally", async () => {
    const widget = await render("admin");

    mockFetchOnce({ ok: true, status: 201, jsonBody: RESULT });
    await chooseAndUpload(widget);

    mockFetchOnce({
      ok: false,
      status: 409,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/conflict",
        title: "Conflict",
        status: 409,
      },
    });
    button(widget, ".media-remove-button")?.click();
    await settle();
    await widget.updateComplete;

    // The editor's action is done either way; the 24 h sweep is the backstop
    // for a file that stayed unreferenced.
    expect(widget.getValue()).toBeNull();
    expect(widget.shadowRoot?.querySelector(".media-error")).toBeNull();
  });

  test("reset clears file, preview, value, and messages", async () => {
    const widget = await render("admin");

    mockFetchOnce({ ok: true, status: 201, jsonBody: RESULT });
    await chooseAndUpload(widget, "a.jpg");

    widget.reset();
    await widget.updateComplete;

    expect(widget.getValue()).toBeNull();
    expect(preview(widget)).toBeNull();
    expect(button(widget, ".media-remove-button")).toBeNull();
    expect(statusText(widget)).toBe(el.admin.uploadStatusNone);
  });
});
