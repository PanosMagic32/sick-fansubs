/**
 * Wire DTOs for the public projects list + detail — mirror the Go projects
 * handlers and the OpenAPI operations listProjects/getProject. Update this
 * file and the Go handlers together when a shape changes (web/src/shared
 * AGENTS.md: types live in the feature's data-access/).
 */

/** Public user reference on a project: id + username + avatar — never role. */
export interface ProjectUserRef {
  id: string;
  username: string;
  /** Absolute avatar URL, or null when neither the user's nor the uploader
   * fallback's picture resolves (a guard-rejected stored value counts as
   * absent). */
  avatarUrl: string | null;
}

/** Public card projection of one project in the list response. */
export interface ProjectListItem {
  id: string;
  title: string;
  /** Plain text, not markup — may be empty; the UI clamps it on cards. */
  description: string;
  /**
   * Latin transliterated slug — DATA ONLY, never an address key: project
   * URLs are ID-addressed exactly like blog posts. The public UI never
   * renders it; the staff list shows it as the row's secondary line and
   * the edit form exposes it to admin+.
   */
  slug: string;
  /**
   * Null when the stored value fails the scheme guard — the wire
   * emits null, never a fake URL. The list renders a neutral placeholder.
   */
  thumbnailUrl: string | null;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  publishedAt: string;
  updatedAt: string;
  /** Total comments (top-level + replies) — live-derived indicator. */
  commentCount: number;
  /** Total favorites — live-derived indicator. */
  favoriteCount: number;
  /** Null only when neither the creator nor the server's uploader fallback resolves. */
  creator: ProjectUserRef | null;
}

/** Keyset-pagination continuation metadata. */
export interface ProjectListPageInfo {
  hasNextPage: boolean;
  /**
   * Opaque versioned cursor for the next page (the `pv1.` namespace —
   * cursors never cross endpoints), or null on the final page.
   */
  endCursor: string | null;
  /** Filtered row count at read time — the pager's page-count numerator. */
  total: number;
}

/** Collection envelope: one page of items plus its continuation metadata. */
export interface ProjectList {
  items: ProjectListItem[];
  pageInfo: ProjectListPageInfo;
}

// ── Detail ────────────────────────────────────────────────────────
// Mirrors the Go projects-detail handler and the OpenAPI operation
// getProject + schemas ProjectDetail/ProjectUserRef/ProjectDownload.

/** One download batch, ordered by stored position. */
export interface ProjectDownload {
  /** The batch's display name. */
  name: string;
  /** Null when absent or masked by the link guard. */
  magnetUrl: string | null;
  torrentUrl: string | null;
}

/** Public detail of one published project. */
export interface ProjectDetail {
  id: string;
  title: string;
  /** May be empty. */
  description: string;
  /** Data only — never an address key. */
  slug: string;
  /**
   * Null when the stored value fails the scheme guard — the wire
   * emits null, never a fake URL.
   */
  thumbnailUrl: string | null;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  publishedAt: string;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  updatedAt: string;
  /** Total comments (top-level + replies) — live-derived indicator. */
  commentCount: number;
  /** Total favorites — live-derived indicator. */
  favoriteCount: number;
  /** Null only when neither the creator nor the server's uploader fallback resolves. */
  creator: ProjectUserRef | null;
  /** The last editor — the same uploader fallback applies. */
  updater: ProjectUserRef | null;
  downloads: ProjectDownload[];
}
