/**
 * Staff audit API suite — path, method, the
 * query-string shape, and the two vocabularies that must agree: every event
 * the endpoint accepts has a Greek label in the catalog (the Go↔TS half is
 * pinned by internal/audit/event_names_pin_test.go).
 */

import { afterEach, describe, expect, test } from "bun:test";

import { el } from "@shared/catalog/el.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import { AUDIT_EVENTS, getStaffAuditEvents } from "./audit-api.js";

afterEach(() => {
  restoreMockFetch();
});

const AUDIT_BODY = {
  items: [
    {
      id: "e1",
      event: "role_changed",
      result: "success",
      actorId: "actor-1",
      targetId: "target-1",
      targetRole: "user",
      requestId: "req-1",
      remoteAddr: "192.0.2.1",
      createdAt: "2026-09-18T10:00:00.000Z",
    },
  ],
};

describe("staff audit API", () => {
  test("getStaffAuditEvents GETs /staff/audit-events with no parameters when none are given", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: AUDIT_BODY,
    });

    const result = await getStaffAuditEvents();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/staff/audit-events");
    expect(result.items[0]?.id).toBe("e1");
    expect(result.items[0]?.remoteAddr).toBe("192.0.2.1");
  });

  test("event and limit ride the query string", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: AUDIT_BODY,
    });

    await getStaffAuditEvents({ event: "sign_in_failure", limit: 10 });

    expect(req.url).toBe(
      "/api/v1/staff/audit-events?event=sign_in_failure&limit=10",
    );
  });

  test("a blank event is dropped rather than sent as an empty filter", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: AUDIT_BODY,
    });

    await getStaffAuditEvents({ event: "", limit: 50 });

    expect(req.url).toBe("/api/v1/staff/audit-events?limit=50");
  });

  test("every accepted event has a Greek catalog label", () => {
    // The third copy of the vocabulary (the labels): a new event without one
    // would render its raw English wire value to a Greek reader.
    for (const event of AUDIT_EVENTS) {
      expect(
        el.admin.auditEventNames[
          event as keyof typeof el.admin.auditEventNames
        ],
        `el.admin.auditEventNames.${event}`,
      ).toBeString();
    }
  });
});
