/**
 * Cross-feature window-event contracts — the content
 * indicators' live-count sync.
 *
 * The two content types share one kind union here: the producing elements
 * (`favorite-toggle` in features/auth, `comments-thread` in
 * features/comments) and the consuming detail pages all import from
 * shared/ — these contracts live here so a consumer never needs the
 * producer's module (the recorded cross-feature edges are components.md
 * rule 2). The events ride the
 * window so any page can listen without an element reference; the detail
 * payload carries kind + contentId so a listener can match its own content
 * exactly.
 */

/** The content kind discriminator (the URL segment name). */
export type ContentKind = "blog-posts" | "projects";

/**
 * Dispatched by <favorite-toggle> after a CONFIRMED toggle success only —
 * pending and failed toggles emit nothing (the confirmed-update
 * machine). The detail pages adjust their public favoriteCount indicator
 * from it.
 */
export const FAVORITE_CHANGED_EVENT = "sf-favorite-changed";

export interface FavoriteChangedDetail {
  kind: ContentKind;
  contentId: string;
  favorited: boolean;
}

/**
 * Dispatched by <comments-thread> after every successful count refetch —
 * mount, create/reply/delete, and focus jumps. The detail pages match
 * kind/contentId before applying the count to their public indicator.
 */
export const COMMENT_COUNT_CHANGED_EVENT = "sf-comment-count-changed";

export interface CommentCountChangedDetail {
  kind: ContentKind;
  contentId: string;
  count: number;
}

/**
 * Dispatched on the window after a CONFIRMED avatar upload success only
 * (avatar slice). The header's icon-only account link listens and
 * refetches the profile so the avatar chip refreshes without a session
 * state change (the avatar is not part of the session projection).
 */
export const AVATAR_CHANGED_EVENT = "sf-avatar-changed";
