/**
 * Wire DTOs for the public blog list + detail — mirror the Go
 * handlers in internal/handler/blog.go and blog_detail.go and the OpenAPI
 * operations listBlogPosts/getBlogPost. Update this file and the Go
 * handlers together when a shape changes (web/src/shared AGENTS.md: types
 * live in the feature's data-access/).
 */

/** Public user reference on content: id + username + avatar — never role. */
export interface BlogPostUserRef {
  id: string;
  username: string;
  /** Absolute avatar URL, or null when neither the user's nor the uploader
   * fallback's picture resolves (a guard-rejected stored value counts as
   * absent). */
  avatarUrl: string | null;
}

/** Public card projection of one blog post in the list response. */
export interface BlogPostListItem {
  id: string;
  title: string;
  /** May be empty — the schema defaults subtitle to "". */
  subtitle: string;
  /** Plain text, not markup — the UI clamps it on cards. */
  description: string;
  thumbnailUrl: string;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  publishedAt: string;
  updatedAt: string;
  /** Total comments (top-level + replies) — live-derived indicator. */
  commentCount: number;
  /** Total favorites — live-derived indicator. */
  favoriteCount: number;
  /** Null only when neither the creator nor the server's uploader fallback resolves. */
  creator: BlogPostUserRef | null;
}

/** Keyset-pagination continuation metadata. */
export interface BlogPostListPageInfo {
  hasNextPage: boolean;
  /** Opaque versioned cursor for the next page, or null on the final page. */
  endCursor: string | null;
  /** Filtered row count at read time — the pager's page-count numerator. */
  total: number;
}

/** Collection envelope: one page of items plus its continuation metadata. */
export interface BlogPostList {
  items: BlogPostListItem[];
  pageInfo: BlogPostListPageInfo;
}

// ── Detail ──────────────────────────────────────────────────────
// Mirrors internal/handler/blog_detail.go and the OpenAPI operation
// getBlogPost + schemas BlogPostDetail/BlogPostUserRef/BlogPostDownload.

/** One download choice, ordered by stored position. */
export interface BlogPostDownload {
  resolution: string;
  /** Null when absent or masked by the read-boundary link guard. */
  magnetUrl: string | null;
  torrentUrl: string | null;
}

/** Public detail of one published blog post. */
export interface BlogPostDetail {
  id: string;
  title: string;
  /** May be empty. */
  subtitle: string;
  /** Plain text, not markup. */
  description: string;
  thumbnailUrl: string;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  publishedAt: string;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  updatedAt: string;
  /** Total comments (top-level + replies) — live-derived indicator. */
  commentCount: number;
  /** Total favorites — live-derived indicator. */
  favoriteCount: number;
  /** Null only when neither the creator nor the server's uploader fallback resolves. */
  creator: BlogPostUserRef | null;
  /** The last editor — the same uploader fallback applies. */
  updater: BlogPostUserRef | null;
  downloads: BlogPostDownload[];
}
