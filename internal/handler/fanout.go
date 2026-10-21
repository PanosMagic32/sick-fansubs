package handler

import (
	"context"

	"sick-fansubs/internal/push"
	"sick-fansubs/internal/store"
)

// fanOutPush hands each committed notification event to the push channel.
// A nil notifier means delivery is disabled. actor is the session username;
// the actorless comment_removed notice passes "" so the payload never names
// the moderator.
func fanOutPush(ctx context.Context, notify push.Notifier, events []store.NotificationEvent, actor string) {
	if notify == nil {
		return
	}
	for _, ev := range events {
		notify.NotifyPush(ctx, pushEvent(ev, actor))
	}
}

// pushEvent is the one NotificationEvent to push.Event projection. Every
// fan-out carries the full event so a new field crosses the seam once.
func pushEvent(ev store.NotificationEvent, actor string) push.Event {
	return push.Event{
		RecipientID:   ev.RecipientID,
		Kind:          ev.Kind,
		ActorUsername: actor,
		ContentTitle:  ev.ContentTitle,
		ContentKind:   ev.ContentKind,
		ContentID:     ev.ContentID,
		CommentID:     ev.CommentID,
		DraftAction:   ev.DraftAction,
	}
}
