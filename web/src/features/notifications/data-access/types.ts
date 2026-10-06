/**
 * Notifications feature wire types — the per-kind feed DTOs, the list
 * envelope, and the push/subscription/preference shapes, mirroring the Go
 * handlers and the OpenAPI schemas. The request functions that use them
 * live in `notifications-api.ts` and `push-api.ts`.
 *
 * `kind` IS the discriminator for feed items: the seven user_notifications
 * kinds plus the five account audit event names. Every item carries the
 * viewer's `read` state; `CommentRemovedNotificationItem.actorUsername` is
 * always null (the moderator is never named), and
 * `DraftActivityNotificationItem.draftAction` is always present.
 */

/** The five account audit events — the staff half of the feed. */
export type AccountEventKind =
  | "password_reset"
  | "role_changed"
  | "user_suspended"
  | "user_reactivated"
  | "user_deleted";

export interface AccountNotificationItem {
  id: string;
  kind: AccountEventKind;
  result: "success" | "failure";
  actorUsername: string | null;
  targetUsername: string | null;
  /** The target's role snapshot at emission time. */
  targetRole: string | null;
  /** Canonical UTC RFC 3339 instant with milliseconds. */
  createdAt: string;
  /** The VIEWER's read state. */
  read: boolean;
}

/** The comment kinds — `comment_reply` and `comment` share the same wire
 * shape; both navigate to the content deep link (`#comment-<commentId>`). */
export interface CommentNotificationItem {
  id: string;
  kind: "comment_reply" | "comment";
  actorUsername: string | null;
  /** Joined at read time; null when the content is gone. */
  contentTitle: string | null;
  /** The URL segment the content lives under: "blog-posts" | "projects". */
  contentKind: "blog-posts" | "projects";
  contentId: string;
  commentId: string;
  createdAt: string;
  read: boolean;
}

/** The heart kind — the comment field set (the hearted comment is the
 * deep-link target), rendered as the heart sentence form. */
export interface HeartNotificationItem {
  id: string;
  kind: "heart";
  actorUsername: string | null;
  /** Joined at read time; null when the content is gone. */
  contentTitle: string | null;
  /** The URL segment the content lives under: "blog-posts" | "projects". */
  contentKind: "blog-posts" | "projects";
  contentId: string;
  commentId: string;
  createdAt: string;
  read: boolean;
}

/** The content_updated kind — a followed content's edit while published.
 * Navigates to the content page itself (no commentId). */
export interface ContentUpdatedNotificationItem {
  id: string;
  kind: "content_updated";
  actorUsername: string | null;
  /** Joined at read time; null when the content is gone. */
  contentTitle: string | null;
  /** The URL segment the content lives under: "blog-posts" | "projects". */
  contentKind: "blog-posts" | "projects";
  contentId: string;
  createdAt: string;
  read: boolean;
}

/** The new_content kind — a new post or project's FIRST publication,
 * broadcast to opted-in viewers. Same wire shape as content_updated. */
export interface NewContentNotificationItem {
  id: string;
  kind: "new_content";
  actorUsername: string | null;
  /** Joined at read time; null when the content is gone. */
  contentTitle: string | null;
  /** The URL segment the content lives under: "blog-posts" | "projects". */
  contentKind: "blog-posts" | "projects";
  contentId: string;
  createdAt: string;
  read: boolean;
}

/** The comment_removed kind — the actorless notice a moderator's deletion
 * emits. It navigates to the CONTENT page: the comment it names is gone, so
 * a `#comment-<id>` fragment would only hit the deleted-target fallback. */
export interface CommentRemovedNotificationItem {
  id: string;
  kind: "comment_removed";
  /** Always null — the moderator is never named. */
  actorUsername: null;
  /** Joined at read time; null when the content is gone. */
  contentTitle: string | null;
  /** The URL segment the content lives under: "blog-posts" | "projects". */
  contentKind: "blog-posts" | "projects";
  contentId: string;
  /** The removed comment's own id (polymorphic ref — the row is gone). */
  commentId: string;
  createdAt: string;
  read: boolean;
}

/** The draft_activity kind — the staff-only notice about UNPUBLISHED
 * content. Same field set as the content-level kinds, plus `draftAction`
 * (always present), which picks the line's verb. The navigation target is
 * the STAFF editor: unpublished content has no public page. */
export interface DraftActivityNotificationItem {
  id: string;
  kind: "draft_activity";
  actorUsername: string | null;
  /** Joined at read time; null when the content is gone. */
  contentTitle: string | null;
  /** The URL segment the content lives under: "blog-posts" | "projects". */
  contentKind: "blog-posts" | "projects";
  contentId: string;
  /** What the actor did to the draft — always present on this kind. */
  draftAction: "created" | "updated" | "unpublished";
  createdAt: string;
  read: boolean;
}

export type NotificationItem =
  | AccountNotificationItem
  | CommentNotificationItem
  | HeartNotificationItem
  | CommentRemovedNotificationItem
  | ContentUpdatedNotificationItem
  | NewContentNotificationItem
  | DraftActivityNotificationItem;

/** Keyset collection envelope — one page plus its continuation metadata. */
export interface NotificationList {
  items: NotificationItem[];
  pageInfo: {
    hasNextPage: boolean;
    endCursor: string | null;
    /** Filtered row count at read time — the pager's page-count numerator. */
    total: number;
  };
}

export interface PushSubscriptionItem {
  id: string;
  endpoint: string;
  createdAt: string;
}

export interface PushSubscriptionList {
  subscriptions: PushSubscriptionItem[];
}

/** The browser's PushSubscription JSON shape (endpoint + keys). */
export interface PushSubscriptionCreate {
  endpoint: string;
  keys: {
    p256dh: string;
    auth: string;
  };
}

/** The seven shipped notification kinds. */
export type PushNotificationKind =
  | "heart"
  | "comment_reply"
  | "comment"
  | "content_updated"
  | "new_content"
  | "comment_removed"
  | "draft_activity";

export interface NotificationPreference {
  kind: PushNotificationKind;
  pushEnabled: boolean;
}

export interface NotificationPreferenceList {
  preferences: NotificationPreference[];
}

/** The self-test send result: how many of the caller's devices accepted the
 * test message, plus one outcome per attempted endpoint, so THIS device can
 * tell whether it was the refusal. `service` is a coarse push-service family
 * and `id` is the caller's own row; the server never sends the endpoint URL
 * or the key material. */
export interface PushTestEndpoint {
  id: string;
  service: "mozilla" | "fcm" | "apple" | "other";
  accepted: boolean;
  /** The push service's status; absent when no response arrived. */
  status?: number;
  /** The dead-endpoint rule deleted this row during the send. */
  removed?: boolean;
}

export interface PushTestResult {
  delivered: number;
  endpoints: PushTestEndpoint[];
}
