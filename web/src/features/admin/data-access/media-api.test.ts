/**
 * Media upload API suite — multipart framing, CSRF,
 * and the 201 result shape. Transport semantics live in
 * shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import { uploadMedia } from "./media-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

describe("uploadMedia", () => {
  test("POSTs multipart with the CSRF token and returns the 201 body", async () => {
    setCSRFToken("csrf-token-1");

    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      jsonBody: {
        path: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
        url: "http://localhost:3000/media/images/abcdef0123456789abcdef0123456789.jpg",
      },
    });

    const file = new File(["fake-image-bytes"], "thumb.jpg", {
      type: "image/jpeg",
    });
    const result = await uploadMedia(file);

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/media");
    expect(req.headers["x-csrf-token"]).toBe("csrf-token-1");
    // The browser's FormData sets the multipart Content-Type itself — the
    // transport must never set application/json here.
    expect(req.headers["content-type"]).toBeUndefined();

    expect(result.path).toBe(
      "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
    );
    expect(result.url).toContain(
      "/media/images/abcdef0123456789abcdef0123456789.jpg",
    );
  });
});
