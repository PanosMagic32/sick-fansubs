package routes

import (
	"database/sql"
	"net/http"
	"time"

	"sick-fansubs/internal/handler"
	"sick-fansubs/internal/middleware"
	"sick-fansubs/internal/push"
)

// Comments registers the comments endpoints under
// /api/v1/blog-posts/{id}/comments and /api/v1/projects/{id}/comments.
//
// Routes (blog and projects mirror exactly):
//
//	GET    /api/v1/{segment}/{id}/comments                     — public keyset thread list (sort/limit/after/focus)
//	GET    /api/v1/{segment}/{id}/comments/count               — public total count
//	GET    /api/v1/{segment}/{id}/comments/{commentId}/replies — public keyset page of a comment's replies (limit/after)
//	POST   /api/v1/{segment}/{id}/comments                     — create top-level (auth + CSRF)
//	POST   /api/v1/{segment}/{id}/comments/{commentId}/replies — reply (auth + CSRF; + notification in-tx)
//	PATCH  /api/v1/{segment}/{id}/comments/{commentId}         — author-only edit (auth + CSRF)
//	DELETE /api/v1/{segment}/{id}/comments/{commentId}         — author or moderator+ hard delete (auth + CSRF)
//	PUT    /api/v1/{segment}/{id}/comments/{commentId}/heart   — heart (auth + CSRF)
//	DELETE /api/v1/{segment}/{id}/comments/{commentId}/heart   — unheart (auth + CSRF)
//
// Chains: the public list resolves the session OPTIONALLY through
// optionalSessionChain — RequestID → TrustedOrigin → Session, deliberately
// WITHOUT CSRF (a GET) and WITHOUT the forced-change gate (the read is
// public; a flagged user must still read comments). The count is
// session-independent (publicChain), and the replies page reads on the same
// chain as the list. The six write paths use authenticatedChain; the
// forced-change gate applies to all comment writes.
//
// Method-less exact and subtree fallbacks answer every other method/path
// in the namespace with the JSON 404 (the notifications/users precedent —
// the method-aware mux's 405 text/plain never reaches the client).
//
// Rate limits: every write takes a user-keyed
// handler-level bucket (the pwChangeLimiter precedent) — create and
// reply share 10/15 min, edit 20/15 min, heart 15/5 min, delete 20/15 min —
// answering 429 + Retry-After. ONE instance per bucket is shared by both
// content kinds: the budget is per user, not per segment.
func Comments(mux *http.ServeMux, db *sql.DB, secure bool, trustedOrigin, publicBaseURL string, notify push.Notifier) {

	createLimiter := middleware.NewRateLimiter(10, 15*time.Minute, time.Minute)
	editLimiter := middleware.NewRateLimiter(20, 15*time.Minute, time.Minute)
	heartLimiter := middleware.NewRateLimiter(15, 5*time.Minute, time.Minute)
	deleteLimiter := middleware.NewRateLimiter(20, 15*time.Minute, time.Minute)

	register := func(segment string, list, count, replies, create, reply, update, del, heartOn, heartOff http.Handler) {
		base := "/api/v1/" + segment + "/{id}/comments"

		mux.Handle("GET "+base, list)
		mux.Handle("GET "+base+"/count", count)
		mux.Handle("GET "+base+"/{commentId}/replies", replies)

		mux.Handle("POST "+base, create)
		mux.Handle("POST "+base+"/{commentId}/replies", reply)
		mux.Handle("PATCH "+base+"/{commentId}", update)
		mux.Handle("DELETE "+base+"/{commentId}", del)
		mux.Handle("PUT "+base+"/{commentId}/heart", heartOn)
		mux.Handle("DELETE "+base+"/{commentId}/heart", heartOff)

		// JSON 404 for every other method/path in the namespace (exact +
		// subtree, the notifications precedent).
		mux.Handle(base, notFound())
		mux.Handle(base+"/", notFound())
		mux.Handle(base+"/{commentId}", notFound())
		mux.Handle(base+"/{commentId}/", notFound())
	}

	register("blog-posts",
		optionalSessionChain(handler.CommentsList(handler.BlogCommentsKind, db, publicBaseURL), db, secure, trustedOrigin),
		publicChain(handler.CommentsCount(handler.BlogCommentsKind, db)),
		optionalSessionChain(handler.CommentsReplies(handler.BlogCommentsKind, db, publicBaseURL), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsCreate(handler.BlogCommentsKind, db, notify, nil, createLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsReply(handler.BlogCommentsKind, db, notify, nil, createLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsUpdate(handler.BlogCommentsKind, db, publicBaseURL, nil, editLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsDelete(handler.BlogCommentsKind, db, notify, nil, deleteLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsHeart(handler.BlogCommentsKind, db, notify, nil, true, heartLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsHeart(handler.BlogCommentsKind, db, notify, nil, false, heartLimiter), db, secure, trustedOrigin),
	)
	register("projects",
		optionalSessionChain(handler.CommentsList(handler.ProjectCommentsKind, db, publicBaseURL), db, secure, trustedOrigin),
		publicChain(handler.CommentsCount(handler.ProjectCommentsKind, db)),
		optionalSessionChain(handler.CommentsReplies(handler.ProjectCommentsKind, db, publicBaseURL), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsCreate(handler.ProjectCommentsKind, db, notify, nil, createLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsReply(handler.ProjectCommentsKind, db, notify, nil, createLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsUpdate(handler.ProjectCommentsKind, db, publicBaseURL, nil, editLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsDelete(handler.ProjectCommentsKind, db, notify, nil, deleteLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsHeart(handler.ProjectCommentsKind, db, notify, nil, true, heartLimiter), db, secure, trustedOrigin),
		authenticatedChain(handler.CommentsHeart(handler.ProjectCommentsKind, db, notify, nil, false, heartLimiter), db, secure, trustedOrigin),
	)
}
