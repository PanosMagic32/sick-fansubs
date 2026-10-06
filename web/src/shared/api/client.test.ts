/**
 * Transport suite — the shared/api/client.ts module itself: request()
 * semantics, CSRF token state, the 401 auto-transition callback, problem
 * parsing, error classes, and Retry-After handling.
 *
 * Endpoint suites (paths, methods, per-endpoint CSRF knobs) live next to
 * the endpoints: features/auth/data-access/auth-api.test.ts.
 */

import { describe, expect, test, beforeEach, afterEach } from "bun:test";

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
  ValidationError,
  clearCSRFToken,
  clearOnUnauthenticated,
  getCSRFToken,
  onUnauthenticated,
  request,
  requestForm,
  setCSRFToken,
} from "./client.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  restoreMockFetch,
} from "./test-utils.js";

// Transport module state is process-wide: reset the CSRF token, the 401
// callback, and the fetch double after every test (bun shares one window
// per worker).
beforeEach(() => {
  clearCSRFToken();
});

afterEach(() => {
  clearOnUnauthenticated();
  restoreMockFetch();
});

// ── CSRF token management ───────────────────────────────────────────

describe("CSRF token", () => {
  test("starts null", () => {
    expect(getCSRFToken()).toBeNull();
  });

  test("setCSRFToken stores and getCSRFToken retrieves", () => {
    setCSRFToken("abc123");
    expect(getCSRFToken()).toBe("abc123");
  });

  test("clearCSRFToken nullifies", () => {
    setCSRFToken("abc123");
    clearCSRFToken();
    expect(getCSRFToken()).toBeNull();
  });
});

// ── 401 auto-transition callback ────────────────────────────────────

describe("onUnauthenticated", () => {
  test("fires callback on 401 response", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });

    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await request("POST", "/anything", {});
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(true);
    expect(getCSRFToken()).toBeNull();
  });

  test("does not fire with suppress401Callback", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });

    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await request("PUT", "/anything", {}, { suppress401Callback: true });
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(false);
  });

  test("does not fire on non-401 errors", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });

    mockFetchOnce({
      ok: false,
      status: 403,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 403,
      },
    });

    try {
      await request("POST", "/anything", {});
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(false);
  });

  test("clearOnUnauthenticated stops the transition", async () => {
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });
    clearOnUnauthenticated();

    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await request("POST", "/anything", {});
    } catch {
      // Expected — ApiError.
    }

    expect(called).toBe(false);
  });
});

// ── Retry-After ─────────────────────────────────────────────────────

describe("Retry-After", () => {
  test("ApiError carries retryAfterSeconds from the header", async () => {
    mockFetchOnce({
      ok: false,
      status: 429,
      headers: {
        "content-type": "application/problem+json",
        "retry-after": "30",
      },
      jsonBody: {
        type: "/problems/rate-limited",
        title: "Too many requests",
        status: 429,
      },
    });

    try {
      await request("POST", "/anything", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).retryAfterSeconds).toBe(30);
    }
  });

  test("retryAfterSeconds is undefined without the header", async () => {
    mockFetchOnce({
      ok: false,
      status: 429,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/rate-limited",
        title: "Too many requests",
        status: 429,
      },
    });

    try {
      await request("POST", "/anything", {});
      expect.unreachable();
    } catch (err) {
      expect((err as ApiError).retryAfterSeconds).toBeUndefined();
    }
  });

  test("an HTTP-date Retry-After is dropped, not misparsed", async () => {
    // RFC 9110 also allows an HTTP-date form; the wire contract (OpenAPI
    // `RateLimited` response, `Retry-After: type: integer`) is integer
    // seconds, so the date form must yield undefined — parseInt would
    // otherwise produce a nonsense number.
    mockFetchOnce({
      ok: false,
      status: 429,
      headers: {
        "content-type": "application/problem+json",
        "retry-after": "Wed, 21 Oct 2015 07:28:00 GMT",
      },
      jsonBody: {
        type: "/problems/rate-limited",
        title: "Too many requests",
        status: 429,
      },
    });

    try {
      await request("POST", "/anything", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).retryAfterSeconds).toBeUndefined();
    }
  });

  test("junk and out-of-contract values are dropped, not misparsed", async () => {
    for (const header of ["30x", "1.9", "0", "-5"]) {
      mockFetchOnce({
        ok: false,
        status: 429,
        headers: {
          "content-type": "application/problem+json",
          "retry-after": header,
        },
        jsonBody: {
          type: "/problems/rate-limited",
          title: "Too many requests",
          status: 429,
        },
      });

      try {
        await request("POST", "/anything", {});
        expect.unreachable();
      } catch (err) {
        expect(
          (err as ApiError).retryAfterSeconds,
          `header ${JSON.stringify(header)}`,
        ).toBeUndefined();
      }
    }
  });

  test("a non-JSON 429 still carries retryAfterSeconds", async () => {
    mockFetchOnce({
      ok: false,
      status: 429,
      headers: { "retry-after": "30" },
    });

    try {
      await request("POST", "/anything", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).type).toBe("/problems/internal-error");
      expect((err as ApiError).retryAfterSeconds).toBe(30);
    }
  });
});

// ── Request semantics ───────────────────────────────────────────────

describe("request", () => {
  test("sends Accept header and no Content-Type without a body", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { ok: true },
    });

    await request("GET", "/ping");

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/ping");
    expect(req.headers["accept"]).toBe("application/json");
    expect(req.headers["content-type"]).toBeUndefined();
  });

  test("includes Content-Type header with a body", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { ok: true },
    });

    await request("POST", "/thing", { a: 1 });

    expect(req.headers["content-type"]).toBe("application/json");
    expect(req.body).toBe('{"a":1}');
  });

  test("attaches the CSRF token when needsCSRF and a token exists", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { ok: true },
    });

    await request("POST", "/thing", undefined, { needsCSRF: true });

    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("skips the CSRF header when no token exists", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { ok: true },
    });

    await request("POST", "/thing", undefined, { needsCSRF: true });

    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("returns the parsed body on 204 No Content", async () => {
    mockFetchOnce({ ok: true, status: 204 });

    const result = await request("POST", "/thing");
    expect(result).toBeUndefined();
  });

  test("a bodyless 201 resolves undefined and still reaches onResponse (the comments Location contract)", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      headers: { Location: "/thing?focus=c1" },
      jsonBody: "",
    });

    let captured: { location: string | null } = { location: null };
    const result = await request<void>(
      "POST",
      "/thing",
      { body: "x" },
      {
        onResponse: (res) => (captured.location = res.headers.get("Location")),
      },
    );

    expect(result).toBeUndefined();
    expect(captured.location).toBe("/thing?focus=c1");
    expect(req.method).toBe("POST");
  });

  test("onResponse does not fire on a failure", async () => {
    mockFetchOnce({
      ok: false,
      status: 500,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/internal-error",
        title: "Internal server error",
        status: 500,
      },
    });

    let fired = false;
    try {
      await request("POST", "/thing", {}, { onResponse: () => (fired = true) });
      expect.unreachable();
    } catch {
      // Expected — ApiError.
    }

    expect(fired).toBe(false);
  });
});

// ── Error mapping ───────────────────────────────────────────────────

describe("request error mapping", () => {
  test("413 problem response throws ApiError with the payload-too-large type", async () => {
    mockFetchOnce({
      ok: false,
      status: 413,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/payload-too-large",
        title: "Payload too large",
        status: 413,
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(413);
      expect((err as ApiError).type).toBe("/problems/payload-too-large");
    }
  });

  test("500 problem response throws ApiError with the server problem type", async () => {
    mockFetchOnce({
      ok: false,
      status: 500,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/internal-error",
        title: "Internal server error",
        status: 500,
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(500);
      expect((err as ApiError).type).toBe("/problems/internal-error");
      expect((err as ApiError).retryAfterSeconds).toBeUndefined();
    }
  });

  test("404 non-JSON response wraps as ApiError", async () => {
    mockFetchOnce({
      ok: false,
      status: 404,
    });

    try {
      await request("GET", "/missing");
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(404);
      expect((err as ApiError).title).toContain("404");
    }
  });

  test("200 with malformed JSON body throws ApiError", async () => {
    mockFetchImpl(
      async () =>
        ({
          ok: true,
          status: 200,
          headers: new Headers(),
          text: async () => "{not json",
          json: async () => {
            throw new SyntaxError("Unexpected token");
          },
        }) as unknown as Response,
    );

    try {
      await request("GET", "/thing");
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).title).toBe("Failed to parse response body");
    }
  });

  test("problem response with malformed JSON body throws ApiError", async () => {
    mockFetchImpl(
      async () =>
        ({
          ok: false,
          status: 400,
          headers: new Headers({ "content-type": "application/problem+json" }),
          json: async () => {
            throw new SyntaxError("Bad JSON");
          },
        }) as unknown as Response,
    );

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).title).toBe("Failed to parse error response");
    }
  });

  test("a problem body of JSON null throws the internal-error ApiError", async () => {
    mockFetchOnce({
      ok: false,
      status: 400,
      headers: { "content-type": "application/problem+json" },
      jsonBody: null,
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).type).toBe("/problems/internal-error");
      expect((err as ApiError).status).toBe(400);
    }
  });

  test("a primitive problem body throws the internal-error ApiError", async () => {
    mockFetchOnce({
      ok: false,
      status: 400,
      headers: { "content-type": "application/problem+json" },
      jsonBody: "oops",
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).title).toBe("Failed to parse error response");
      expect((err as ApiError).status).toBe(400);
    }
  });

  test("a problem body missing status falls back to the response status", async () => {
    mockFetchOnce({
      ok: false,
      status: 403,
      headers: { "content-type": "application/problem+json" },
      jsonBody: { type: "/problems/forbidden", title: "Forbidden" },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).type).toBe("/problems/forbidden");
      expect((err as ApiError).status).toBe(403);
    }
  });

  test("a wrongly-typed problem status falls back to the response status", async () => {
    mockFetchOnce({
      ok: false,
      status: 403,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/forbidden",
        title: "Forbidden",
        status: "403",
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect((err as ApiError).status).toBe(403);
    }
  });

  test("a JSON-array problem body throws the internal-error ApiError", async () => {
    mockFetchOnce({
      ok: false,
      status: 400,
      headers: { "content-type": "application/problem+json" },
      jsonBody: [],
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).type).toBe("/problems/internal-error");
      expect((err as ApiError).status).toBe(400);
    }
  });

  test("the response status wins over a disagreeing problem body status", async () => {
    mockFetchOnce({
      ok: false,
      status: 403,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/forbidden",
        title: "Forbidden",
        status: 401,
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect((err as ApiError).status).toBe(403);
    }
  });

  test("422 with violations array throws ValidationError", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: [{ field: "username", code: "alreadyTaken" }],
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ValidationError);
      expect((err as ValidationError).violations).toHaveLength(1);
    }
  });

  test("422 without violations array throws ApiError not ValidationError", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect(err).not.toBeInstanceOf(ValidationError);
    }
  });

  test("422 with violations:null throws ApiError not ValidationError", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        violations: null,
      },
    });

    try {
      await request("POST", "/thing", {});
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
      expect(err).not.toBeInstanceOf(ValidationError);
    }
  });

  test("AbortError is re-thrown (not wrapped in NetworkError)", async () => {
    mockFetchImpl(async () => {
      throw new DOMException("The operation was aborted", "AbortError");
    });

    try {
      await request("GET", "/thing");
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(DOMException);
      expect((err as DOMException).name).toBe("AbortError");
    }
  });

  test("the composed deadline's TimeoutError surfaces as RequestTimeoutError", async () => {
    const timeout = new DOMException(
      "The operation was timed out",
      "TimeoutError",
    );
    mockFetchImpl(async () => {
      throw timeout;
    });

    try {
      await request("GET", "/thing");
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(RequestTimeoutError);
      expect(err).not.toBeInstanceOf(NetworkError);
      expect((err as RequestTimeoutError).cause).toBe(timeout);
    }
  });

  test("NetworkError preserves original error as cause", async () => {
    const original = new TypeError("Failed to fetch");
    mockFetchImpl(async () => {
      throw original;
    });

    try {
      await request("GET", "/thing");
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(NetworkError);
      expect((err as NetworkError).cause).toBe(original);
    }
  });
});

// ── requestForm (multipart) ────────────────────────────────────────

describe("requestForm", () => {
  test("POSTs the FormData with the CSRF token and parses the body", async () => {
    setCSRFToken("tok");
    const form = new FormData();
    form.append("file", "content");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { ok: true },
    });

    const result = await requestForm<{ ok: boolean }>("/media", form);

    expect(result).toEqual({ ok: true });
    expect(req.method).toBe("POST");
    expect(req.formData).toBe(form);
    expect(req.headers["x-csrf-token"]).toBe("tok");
    // The browser sets the multipart Content-Type (with its boundary).
    expect(req.headers["content-type"]).toBeUndefined();
  });

  test("omits the CSRF header when no token is set", async () => {
    const { req } = mockFetchOnce({ ok: true, status: 200, jsonBody: {} });

    await requestForm("/media", new FormData());

    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("honors the PUT method option (avatar upload)", async () => {
    const { req } = mockFetchOnce({ ok: true, status: 200, jsonBody: {} });

    await requestForm("/me/avatar", new FormData(), { method: "PUT" });

    expect(req.method).toBe("PUT");
  });

  test("a 401 runs the default transition and clears the token", async () => {
    setCSRFToken("tok");
    let called = false;
    onUnauthenticated(() => {
      called = true;
    });
    mockFetchOnce({
      ok: false,
      status: 401,
      headers: { "content-type": "application/problem+json" },
      jsonBody: {
        type: "/problems/auth/invalid-credentials",
        title: "Invalid credentials",
        status: 401,
      },
    });

    try {
      await requestForm("/media", new FormData());
      expect.unreachable();
    } catch (err) {
      expect(err).toBeInstanceOf(ApiError);
    }

    expect(called).toBe(true);
    expect(getCSRFToken()).toBeNull();
  });
});

// ── Error classes ───────────────────────────────────────────────────

describe("ApiError", () => {
  test("has correct name and properties", () => {
    const err = new ApiError({
      type: "/problems/forbidden",
      title: "Forbidden",
      status: 403,
      detail: "Not allowed",
      requestId: "req-123",
    });

    expect(err.name).toBe("ApiError");
    expect(err.message).toBe("Forbidden");
    expect(err.type).toBe("/problems/forbidden");
    expect(err.status).toBe(403);
    expect(err.detail).toBe("Not allowed");
    expect(err.requestId).toBe("req-123");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("ValidationError", () => {
  test("extends ApiError with violations", () => {
    const err = new ValidationError({
      type: "/problems/validation",
      title: "Validation failed",
      status: 422,
      violations: [
        { field: "email", code: "invalidFormat" },
        { field: "password", code: "minLength" },
      ],
    });

    expect(err.name).toBe("ValidationError");
    expect(err).toBeInstanceOf(ApiError);
    expect(err.violations).toHaveLength(2);
  });
});

describe("NetworkError", () => {
  test("preserves message and optional cause", () => {
    const cause = new TypeError("Failed to fetch");
    const err = new NetworkError("Network request failed", cause);

    expect(err.name).toBe("NetworkError");
    expect(err.message).toBe("Network request failed");
    expect(err.cause).toBe(cause);
  });

  test("works without cause", () => {
    const err = new NetworkError("Offline");
    expect(err.cause).toBeUndefined();
  });
});
