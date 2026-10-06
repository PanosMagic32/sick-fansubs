/**
 * Comments wire types — the blog/project thread
 * contracts. Mirrors the Go handler DTOs (internal/handler/comments.go):
 * commentAuthorDTO / commentDTO / commentCountResponse — the list envelope
 * is the shared collection (internal/handler/collection.go). Update both
 * sides together (shared AGENTS convention).
 *
 * Timestamps are canonical UTC RFC 3339 with exactly millisecond
 * precision. The top-level items always carry
 * `replies` (possibly empty); reply items OMIT the key on the wire
 * (the Go `omitempty`), so the optional field is the depth discriminator.
 */

/** The URL segment the two mirrored content kinds live under. */
export type CommentKind = "blog-posts" | "projects";

/** The three sort tabs; "top" is the default. */
export type CommentSort = "top" | "newest" | "oldest";

/** The staff roles the badge can color. */
export type StaffRole = "moderator" | "admin" | "super-admin";

export interface CommentAuthor {
  id: string;
  username: string;
  /** Absolute avatar URL, or null when the account has none. */
  avatarUrl: string | null;
  /** Derived staff badge (moderator and above). */
  isStaff: boolean;
  /**
   * The derived staff role, ABSENT for every non-staff author (the
   * server omits the key, so `undefined` is the non-staff case, not "").
   */
  staffRole?: StaffRole;
}

export interface CommentItem {
  id: string;
  body: string;
  author: CommentAuthor;
  createdAt: string;
  updatedAt: string;
  heartsCount: number;
  /** Viewer-aware — false for anonymous readers. */
  hearted: boolean;
  /** Present on top-level items only ([] when empty); reply items omit it. */
  replies?: CommentItem[];
  /**
   * Total replies — present on top-level items only. The `replies` array
   * holds the inline window (3), not necessarily all of them.
   */
  replyCount?: number;
  /**
   * Opaque continuation for this item's replies, present only while
   * more replies exist after the inline window — the `after` argument of
   * the replies operation. Absent means the window is complete.
   */
  repliesEndCursor?: string;
}

/** Keyset collection envelope — one page plus its continuation metadata;
 * the standalone count endpoint owns the unread/thread counts. The cursor is
 * sort-bound. */
export interface CommentList {
  items: CommentItem[];
  pageInfo: {
    hasNextPage: boolean;
    endCursor: string | null;
    /** Filtered row count at read time. */
    total: number;
  };
}

/** GET …/comments/count — top-level + replies. */
export interface CommentCount {
  count: number;
}
