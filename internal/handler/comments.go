package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"unicode/utf8"

	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/store"
)

const (
	// Comment body bound: 5,000 runes.
	maxCommentBodyRunes = 5000

	commentsDefaultLimit = 20
	commentsMaxLimit     = 100

	// The replies operation's page size is the
	// shared collection default — aliased, not re-declared, so the two can
	// never drift. The INLINE window is smaller (store.inlineReplyWindow =
	// 3); a reader's first append simply returns the rest at this size.
	commentRepliesDefaultLimit = commentsDefaultLimit

	// The accepted sort values. The store carries the same
	// strings as CommentSort constants; the handler parses the wire value
	// against these (the pin test keeps them in lockstep).
	sortTop    = "top"
	sortNewest = "newest"
	sortOldest = "oldest"
)

// commentKind bundles the per-content-type differences so one handler
// family serves both types without a polymorphic store interface. The
// content descriptor is the store's — the wire names (URL segment, cursor
// namespaces, log label) are the handler's own.
type commentKind struct {
	label       string // log-only discriminator: "blog" | "project"
	segment     string // the URL segment: "blog-posts" | "projects"
	prefix      string // cursor namespace (bc1. / pc1.)
	replyPrefix string // reply cursor namespace (br1. / pr1.)

	content store.ContentKind
}

var (
	// BlogCommentsKind / ProjectCommentsKind select the content kind's
	// comment storage, cursor namespaces, URL segment, and log label.
	BlogCommentsKind = commentKind{
		label:       "blog",
		segment:     store.BlogContent.Kind(),
		prefix:      blogCommentCursorPrefix,
		replyPrefix: blogReplyCursorPrefix,
		content:     store.BlogContent,
	}
	ProjectCommentsKind = commentKind{
		label:       "project",
		segment:     store.ProjectContent.Kind(),
		prefix:      projectCommentCursorPrefix,
		replyPrefix: projectReplyCursorPrefix,
		content:     store.ProjectContent,
	}
)

// threadURL is the deep-link resource for one comment: the
// thread list with the focus query — the Location of creates/replies and
// the fragment target the frontend resolves.
func (k commentKind) threadURL(contentID, commentID string) string {
	return "/api/v1/" + k.segment + "/" + contentID + "/comments?focus=" + commentID
}

// commentAuthorDTO is the wire author projection: id, username,
// avatar, and the derived staff state for the badge. Regular authors expose
// the badge alone; STAFF authors additionally carry StaffRole (the scoped
// supersession of the "never the raw role"
// stance — that stance still holds for everyone below moderator).
type commentAuthorDTO struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	AvatarURL *string `json:"avatarUrl"`
	IsStaff   bool    `json:"isStaff"`
	// StaffRole is the derived role for STAFF authors only (the badge colors
	// on it). Non-staff authors OMIT the
	// key (the pointer separates "absent" from an empty string, the replies
	// convention); the never-the-raw-role stance still holds for every
	// non-staff author.
	StaffRole *string `json:"staffRole,omitempty"`
}

// staffRoleOrNil exposes the derived staff role, and only for staff
// authors: a non-staff author's author
// object carries no `staffRole` key at all (the replies-pointer convention
// — absent is not the same as empty).
func staffRoleOrNil(role string) *string {
	if role == "" {
		return nil
	}
	return &role
}

// commentDTO is one comment item. Replies is present ONLY on top-level
// items ("replies = same minus replies"): top-level items emit
// "replies": [] when none; reply items omit the key entirely.
//
// ReplyCount and RepliesEndCursor describe the top-level item's reply window
// and are ABSENT on reply items, like Replies
// — the pointers separate "absent" from a zero/empty value, so the depth
// discriminator stays unambiguous. RepliesEndCursor is the reader's
// continuation for the replies operation (absent = the window is complete).
type commentDTO struct {
	ID               string           `json:"id"`
	Body             string           `json:"body"`
	Author           commentAuthorDTO `json:"author"`
	CreatedAt        string           `json:"createdAt"`
	UpdatedAt        string           `json:"updatedAt"`
	HeartsCount      int64            `json:"heartsCount"`
	Hearted          bool             `json:"hearted"`
	Replies          *[]commentDTO    `json:"replies,omitempty"`
	ReplyCount       *int64           `json:"replyCount,omitempty"`
	RepliesEndCursor *string          `json:"repliesEndCursor,omitempty"`
}

// itemDTO projects one store comment. withReplies selects the top-level
// shape (the pointer distinguishes "absent" from "[]" — the wire emits the
// empty array only for top-level items without replies); it also mints the
// item's replies continuation from the window's last reply, because only
// this layer owns the cursor namespace.
func (k commentKind) itemDTO(publicBaseURL string, c store.Comment, withReplies bool) commentDTO {
	dto := commentDTO{
		ID:   c.ID,
		Body: c.Body,
		Author: commentAuthorDTO{
			ID:        c.Author.ID,
			Username:  c.Author.Username,
			AvatarURL: avatarURLOrNull(publicBaseURL, c.Author.AvatarURL),
			IsStaff:   c.Author.IsStaff,
			StaffRole: staffRoleOrNil(c.Author.StaffRole),
		},
		CreatedAt:   formatAPITime(c.CreatedAtMS),
		UpdatedAt:   formatAPITime(c.UpdatedAtMS),
		HeartsCount: c.HeartsCount,
		Hearted:     c.Hearted,
	}
	if withReplies {
		replies := make([]commentDTO, 0, len(c.Replies))
		for _, r := range c.Replies {
			replies = append(replies, k.itemDTO(publicBaseURL, r, false))
		}
		dto.Replies = &replies
		count := c.ReplyCount
		dto.ReplyCount = &count
		if c.HasMoreReplies && len(c.Replies) > 0 {
			last := c.Replies[len(c.Replies)-1]
			cursor := encodeCursor(k.replyPrefix, last.CreatedAtMS, last.ID)
			dto.RepliesEndCursor = &cursor
		}
	}
	return dto
}

// commentCountResponse is the standalone count endpoint's payload: every
// comment (top-level + replies), unlike pageInfo.total's top-level rows.
type commentCountResponse struct {
	Count int64 `json:"count"`
}

// commentBodyRequest is the create/edit body.
type commentBodyRequest struct {
	Body string `json:"body"`
}

// validateCommentBody applies the plain-text body contract and
// returns the normalized body: CRLF (and lone CR) normalized to LF,
// trimmed, non-empty, ≤ 5,000 runes, and free of control characters (LF
// and TAB excepted — newlines are preserved). Violations: required
// (empty), maxLength (> 5,000), invalidFormat (control characters —
// rejected, never silently stripped).
func validateCommentBody(w http.ResponseWriter, r *http.Request, raw string) (string, bool) {
	body := strings.ReplaceAll(raw, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.TrimSpace(body)

	if body == "" {
		writeValidationErrors(w, r, []Violation{{Field: "body", Code: "required"}})
		return "", false
	}
	if utf8.RuneCountInString(body) > maxCommentBodyRunes {
		writeValidationErrors(w, r, []Violation{{Field: "body", Code: "maxLength"}})
		return "", false
	}
	for _, ru := range body {
		if isCommentControlRune(ru) {
			writeValidationErrors(w, r, []Violation{{Field: "body", Code: "invalidFormat"}})
			return "", false
		}
	}
	return body, true
}

// isCommentControlRune reports whether r is outside the body contract's
// character set: LF and TAB are the only control characters kept; every
// other C0 control, DEL, and the C1 block is rejected.
func isCommentControlRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20, r == 0x7F, r >= 0x80 && r <= 0x9F:
		return true
	default:
		return false
	}
}

// parseCommentListParams applies the strict query contract:
// known parameters sort/limit/after/focus, each at most once; sort is
// top|newest|oldest (default top, unknown → 422 {sort, invalidValue} — the
// enumerated-filter precedent); after and focus are mutually exclusive
// (400); the after cursor decodes SORT-BOUND under the active sort (a
// foreign-sort cursor is 400); focus validates the public id shape (400).
func parseCommentListParams(w http.ResponseWriter, r *http.Request, prefix string) (sort string, limit int, after *commentCursor, focus string, ok bool) {
	query, ok := strictQueryParams(w, r, "sort", "limit", "after", "focus")
	if !ok {
		return "", 0, nil, "", false
	}

	sort = sortTop
	if raw, present := query["sort"]; present {
		switch raw[0] {
		case sortTop, sortNewest, sortOldest:
			sort = raw[0]
		default:
			writeValidationErrors(w, r, []Violation{{Field: "sort", Code: "invalidValue"}})
			return "", 0, nil, "", false
		}
	}

	limit, ok = parseLimitParam(w, r, query, commentsDefaultLimit, commentsMaxLimit)
	if !ok {
		return "", 0, nil, "", false
	}

	if raw, present := query["after"]; present {
		if _, hasFocus := query["focus"]; hasFocus {
			writeBadRequest(w, r, "after and focus are mutually exclusive")
			return "", 0, nil, "", false
		}
		c, err := decodeCommentCursor(raw[0], prefix, sort)
		if err != nil {
			writeBadRequest(w, r, "invalid after cursor")
			return "", 0, nil, "", false
		}
		after = &c
	}

	if raw, present := query["focus"]; present {
		if !publicIDPattern.MatchString(raw[0]) {
			writeBadRequest(w, r, "invalid focus comment id")
			return "", 0, nil, "", false
		}
		focus = raw[0]
	}

	return sort, limit, after, focus, true
}

// CommentsList returns GET /api/v1/{segment}/{id}/comments — one keyset
// page of top-level comments, each with its first inline reply window and
// the continuation for the rest. The session
// is OPTIONAL: present sessions fill the viewer-aware hearted field; an
// anonymous read never 401s.
func CommentsList(kind commentKind, db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su := middleware.GetSession(r.Context())
		var viewer *string
		if su != nil {
			viewer = &su.UserID
		}

		sort, limit, after, focus, ok := parseCommentListParams(w, r, kind.prefix)
		if !ok {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}

		writeKeysetPage(w, r, kind.label+" comments list", keysetPage[store.Comment, commentDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountTopLevelComments(ctx, db, kind.content, id)
			},
			read: func(ctx context.Context) ([]store.Comment, bool, error) {
				if focus != "" {
					return store.ListCommentsFocus(ctx, db, kind.content, id, viewer, store.CommentSort(sort), limit, focus)
				}
				var key *store.CommentPageKey
				if after != nil {
					key = &store.CommentPageKey{CreatedAtMS: after.CreatedAtMS, Hearts: after.Hearts, ID: after.ID}
				}
				return store.ListComments(ctx, db, kind.content, id, viewer, store.CommentSort(sort), limit, key)
			},
			item: func(it store.Comment) commentDTO {
				return kind.itemDTO(publicBaseURL, it, true)
			},
			next: func(last store.Comment) string {
				// The hearts slot only exists in the top sort's tuple — under
				// newest/oldest the cursor must carry 0 or the decoder rejects
				// the server's own cursor.
				hearts := int64(0)
				if sort == sortTop {
					hearts = last.HeartsCount
				}
				return encodeCommentCursor(kind.prefix, sort, last.CreatedAtMS, hearts, last.ID)
			},
		})
	}
}

// CommentsCount returns GET /api/v1/{segment}/{id}/comments/count — the
// total comment count (top-level + replies), session-independent,
// published-only.
func CommentsCount(kind commentKind, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowNoQueryParams(w, r) {
			return
		}
		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}

		n, err := store.CommentCount(r.Context(), db, kind.content, id)
		if err != nil {
			writeStoreError(w, r, err, kind.label+" comments count", "error", err)
			return
		}
		writeJSON(w, r, http.StatusOK, commentCountResponse{Count: n})
	}
}

// CommentsReplies returns GET /api/v1/{segment}/{id}/comments/{commentId}/replies
// — one ascending keyset page of a TOP-LEVEL comment's replies: the
// reader appends the pages after the item's
// inline window. The session is OPTIONAL, exactly like the list (the
// viewer-aware `hearted` field); the cursor is NOT sort-bound (the reply
// order is fixed: created_at_ms, id ascending) and lives in its own
// namespace, so a thread cursor is a 400 here.
func CommentsReplies(kind commentKind, db *sql.DB, publicBaseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su := middleware.GetSession(r.Context())
		var viewer *string
		if su != nil {
			viewer = &su.UserID
		}

		_, limit, after, ok := parseKeysetParams(w, r, commentRepliesDefaultLimit, commentsMaxLimit, kind.replyPrefix)
		if !ok {
			return
		}

		id := r.PathValue("id")
		if !publicIDPattern.MatchString(id) {
			writeBadRequest(w, r, "invalid "+kind.label+" content id")
			return
		}
		commentID := r.PathValue("commentId")
		if !publicIDPattern.MatchString(commentID) {
			writeBadRequest(w, r, "invalid comment id")
			return
		}

		writeKeysetPage(w, r, kind.label+" comment replies", keysetPage[store.Comment, commentDTO]{
			count: func(ctx context.Context) (int, error) {
				return store.CountReplies(ctx, db, kind.content, id, commentID)
			},
			read: func(ctx context.Context) ([]store.Comment, bool, error) {
				var key *store.CommentPageKey
				if after != nil {
					key = &store.CommentPageKey{CreatedAtMS: after.PublishedAtMS, ID: after.ID}
				}
				return store.ListCommentReplies(ctx, db, kind.content, id, commentID, viewer, limit, key)
			},
			item: func(it store.Comment) commentDTO {
				// Reply items carry no reply window of their own (the depth
				// discriminator), so withReplies is false throughout.
				return kind.itemDTO(publicBaseURL, it, false)
			},
			next: func(last store.Comment) string {
				return encodeCursor(kind.replyPrefix, last.CreatedAtMS, last.ID)
			},
		})
	}
}
