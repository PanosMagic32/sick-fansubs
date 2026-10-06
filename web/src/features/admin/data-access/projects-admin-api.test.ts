/**
 * Projects admin API suite — paths, methods, CSRF,
 * and the If-Match precondition headers. Transport semantics live in
 * shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  createProject,
  deleteProject,
  getStaffProject,
  listStaffProjects,
  updateProject,
} from "./projects-admin-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

const CREATE_BODY = {
  title: "T",
  description: "",
  thumbnailPath: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
  status: "published" as const,
  downloads: [{ name: "Batch", magnetUrl: "magnet:?xt=x", torrentUrl: null }],
};

const UPDATE_BODY = {
  ...CREATE_BODY,
  slug: "renamed",
};

describe("projects admin API", () => {
  test("createProject POSTs with CSRF and no slug field", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      jsonBody: {
        id: "p1",
        revision: 1,
        slug: "generated",
        thumbnailPath: CREATE_BODY.thumbnailPath,
      },
    });

    const result = await createProject(CREATE_BODY);

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/projects");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect((req.body as string).includes("slug")).toBe(false);
    expect(result.id).toBe("p1");
    expect(result.slug).toBe("generated");
  });

  test("listStaffProjects GETs with limit/after and the narrowing filters", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    await listStaffProjects({
      limit: 20,
      after: "apv1.abc",
      status: "draft",
      q: "dr stone",
    });

    expect(req.method).toBe("GET");
    expect(req.url).toBe(
      "/api/v1/admin/projects?limit=20&after=apv1.abc&status=draft&q=dr+stone",
    );
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("getStaffProject GETs the staff detail", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { id: "p1", revision: 3, slug: "s" },
    });

    await getStaffProject("p1");

    expect(req.url).toBe("/api/v1/admin/projects/p1");
  });

  test("updateProject PUTs with CSRF, the slug, and the If-Match precondition", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { id: "p1", revision: 2 },
    });

    await updateProject("p1", 1, UPDATE_BODY);

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/projects/p1");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect(req.headers["if-match"]).toBe('"1"');
  });

  test("updateProject omits the slug key when the body has none", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { id: "p1", revision: 2 },
    });

    const { slug: _drop, ...withoutSlug } = UPDATE_BODY;
    await updateProject("p1", 1, withoutSlug);

    expect((req.body as string).includes("slug")).toBe(false);
  });

  test("deleteProject DELETEs with CSRF and the If-Match precondition", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await deleteProject("p1", 2);

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/projects/p1");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect(req.headers["if-match"]).toBe('"2"');
  });
});
