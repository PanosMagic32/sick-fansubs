/**
 * Notifications endpoint suite — paths, methods, and the CSRF knobs of the
 * six feed functions, plus the kind discriminator guards. Transport
 * semantics live in shared/api/client.test.ts.
 */

import { describe, expect, test, afterEach } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  clearReadNotifications,
  deleteAllNotifications,
  deleteNotification,
  isAccountEvent,
  isComment,
  isCommentRemoved,
  isContentUpdated,
  isDraftActivity,
  isHeart,
  isNewContent,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  unreadNotificationCount,
} from "./notifications-api.js";
import type {
  AccountNotificationItem,
  DraftActivityNotificationItem,
} from "./types.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

const ITEM: AccountNotificationItem = {
  id: "ev1",
  kind: "password_reset",
  result: "success",
  actorUsername: "Actorus",
  targetUsername: "Targetus",
  targetRole: "user",
  createdAt: "2026-08-20T12:00:00.000Z",
  read: false,
};

describe("notifications API", () => {
  test("listNotifications sends GET with limit/after and returns the envelope", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [ITEM],
        pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
      },
    });

    const result = await listNotifications({ limit: 20, after: "cursor123" });

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/notifications?limit=20&after=cursor123");
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.items).toHaveLength(1);
    expect(result.pageInfo.hasNextPage).toBe(false);
  });

  test("unreadNotificationCount sends GET and returns the count", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { unread: 7 },
    });

    const result = await unreadNotificationCount();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/notifications/unread-count");
    expect(result.unread).toBe(7);
  });

  test("markNotificationRead sends POST with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await markNotificationRead("ev1");

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/notifications/ev1/read");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("markAllNotificationsRead sends POST with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await markAllNotificationsRead();

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/notifications/read-all");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("deleteNotification sends DELETE with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await deleteNotification("un1");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/notifications/un1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("clearReadNotifications sends POST with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await clearReadNotifications();

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/notifications/clear-read");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("deleteAllNotifications sends POST with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await deleteAllNotifications();

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/notifications/delete-all");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("isComment discriminates both comment kinds", () => {
    expect(
      isComment({
        id: "un1",
        kind: "comment_reply",
        actorUsername: "Eleni",
        contentTitle: "Τίτλος",
        contentKind: "blog-posts",
        contentId: "b1",
        commentId: "c1",
        createdAt: "2026-08-20T12:00:00.000Z",
        read: false,
      }),
    ).toBe(true);
    expect(
      isComment({
        id: "un2",
        kind: "comment",
        actorUsername: "Eleni",
        contentTitle: "Τίτλος",
        contentKind: "projects",
        contentId: "p1",
        commentId: "c2",
        createdAt: "2026-08-20T12:00:00.000Z",
        read: false,
      }),
    ).toBe(true);
    expect(isComment(ITEM)).toBe(false);
  });

  test("isContentUpdated discriminates the content_updated kind", () => {
    const updatedItem = {
      id: "cu1",
      kind: "content_updated" as const,
      actorUsername: "Eleni",
      contentTitle: "Τίτλος",
      contentKind: "blog-posts" as const,
      contentId: "b1",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    };
    expect(isContentUpdated(updatedItem)).toBe(true);
    expect(isContentUpdated(ITEM)).toBe(false);
  });

  test("isNewContent discriminates the new_content kind", () => {
    const newItem = {
      id: "nc1",
      kind: "new_content" as const,
      actorUsername: "Eleni",
      contentTitle: "Νέα ανάρτηση",
      contentKind: "projects" as const,
      contentId: "p1",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    };
    expect(isNewContent(newItem)).toBe(true);
    expect(isNewContent(ITEM)).toBe(false);
  });

  test("isHeart discriminates the heart kind", () => {
    const heartItem = {
      id: "h1",
      kind: "heart" as const,
      actorUsername: "Eleni",
      contentTitle: "Τίτλος",
      contentKind: "blog-posts" as const,
      contentId: "b1",
      commentId: "c1",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    };
    expect(isHeart(heartItem)).toBe(true);
    expect(isHeart(ITEM)).toBe(false);
  });

  test("isCommentRemoved discriminates the comment_removed kind and isAccountEvent separates the ledger", () => {
    const removedItem = {
      id: "rm1",
      kind: "comment_removed" as const,
      actorUsername: null,
      contentTitle: "Τίτλος",
      contentKind: "blog-posts" as const,
      contentId: "b1",
      commentId: "c1",
      createdAt: "2026-08-20T12:00:00.000Z",
      read: false,
    };
    expect(isCommentRemoved(removedItem)).toBe(true);
    expect(isCommentRemoved(ITEM)).toBe(false);

    // The ledger predicate is the complement of the event space: the five
    // account kinds answer true, every user_notifications kind false.
    expect(isAccountEvent(ITEM)).toBe(true);
    expect(isAccountEvent(removedItem)).toBe(false);
  });

  test("a draft_activity item survives the list call and isDraftActivity narrows it", async () => {
    const draftItem: DraftActivityNotificationItem = {
      id: "d1",
      kind: "draft_activity",
      actorUsername: "Panos",
      contentTitle: "Τίτλος",
      contentKind: "blog-posts",
      contentId: "b1",
      draftAction: "created",
      createdAt: "2026-09-21T12:00:00.000Z",
      read: false,
    };
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [draftItem],
        pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
      },
    });

    const result = await listNotifications({ limit: 20 });

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/notifications?limit=20");
    // The item crosses the transport untouched — the kind carries no
    // commentId (the content is the navigation target).
    expect(result.items[0]).toEqual(draftItem);

    const item = result.items[0]!;
    expect(isDraftActivity(item)).toBe(true);
    expect(isDraftActivity(ITEM)).toBe(false);
    if (isDraftActivity(item)) {
      // The narrowing IS the assertion: these fields exist only on the
      // draft kind, so a wrong discriminator fails tsc here.
      expect(item.draftAction).toBe("created");
      expect(item.contentTitle).toBe("Τίτλος");
      expect(item.contentKind).toBe("blog-posts");
      expect(item.contentId).toBe("b1");
    }
  });
});
