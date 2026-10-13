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
 * Dispatched by <content-card>'s interactive favorite indicator (the chip
 * lightened to a button) when it is clicked — the likers modal's trigger on
 * the blog/project detail pages. No detail: the surface that enabled the
 * action knows its own content.
 */
export const FAVORITE_COUNT_CLICK_EVENT = "sf-favorite-count-click";

/**
 * Dispatched by <likers-modal> when the user dismisses the dialog (its close
 * control or Escape) — the host flips `open` back down. Lit event bindings
 * spell this as a static template attribute (`@sf-likers-close`), so hosts
 * keep the literal there; the element's dispatches use this constant.
 */
export const LIKERS_CLOSE_EVENT = "sf-likers-close";

/**
 * Dispatched on the window after a CONFIRMED avatar upload success only
 * (avatar slice). The header's icon-only account link listens and
 * refetches the profile so the avatar chip refreshes without a session
 * state change (the avatar is not part of the session projection).
 */
export const AVATAR_CHANGED_EVENT = "sf-avatar-changed";
