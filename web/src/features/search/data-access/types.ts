/**
 * Wire DTOs for the public search endpoint — mirror the Go
 * handler in internal/handler/search.go and the OpenAPI operation
 * searchContent + schemas SearchList/SearchListItem. Update this file and
 * the Go handler together when a shape changes (web/src/shared AGENTS.md:
 * types live in the feature's data-access/).
 */

/** Which content types a search covers. */
export type SearchType = "all" | "posts" | "projects";

/** Result ordering: "date" is
 * newest first (the default), "oldest" is oldest first, "title" is the
 * Greek-aware alphabetical order. */
export type SearchSort = "date" | "oldest" | "title";

/** One merged search hit — type discriminates post vs project. */
export interface SearchResultItem {
  type: "post" | "project";
  id: string;
  title: string;
  /** The blog post's display subtitle; empty for projects. */
  subtitle: string;
  /** Plain text, not markup. */
  description: string;
  /** Absolute thumbnail URL, or null when absent/masked by the scheme guard. */
  thumbnailUrl: string | null;
  /** Canonical UTC RFC 3339 with exactly millisecond precision. */
  publishedAt: string;
  /** Total comments (top-level + replies) — live-derived indicator. */
  commentCount: number;
  /** Total favorites — live-derived indicator. */
  favoriteCount: number;
}

/** Keyset-pagination continuation metadata. */
export interface SearchListPageInfo {
  hasNextPage: boolean;
  /** Opaque versioned cursor for the next page, or null on the final page. */
  endCursor: string | null;
  /** Filtered row count at read time — the pager's page-count numerator. */
  total: number;
}

/** Collection envelope: one page of items plus its continuation metadata. */
export interface SearchResultList {
  items: SearchResultItem[];
  pageInfo: SearchListPageInfo;
}
