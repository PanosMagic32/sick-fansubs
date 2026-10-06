package mail

// ResetEmail builds the forgot-password message: plain text, Greek copy, one
// reset link. The link is constructed by the caller (service layer) from
// PUBLIC_BASE_URL + the base64url token; only that validated URL and the token
// ever reach the message, so header injection is impossible by construction.
func ResetEmail(link string) (subject, body string) {
	subject = "Επαναφορά κωδικού πρόσβασης — Sick Fansubs"
	// Polite register — the app's auth pages use the formal plural
	// ("Συνδεθείτε", "Ξεχάσατε τον κωδικό σας"), and the email copy
	// matches it.
	body = "Ζητήθηκε επαναφορά του κωδικού πρόσβασης για τον λογαριασμό σας στο Sick Fansubs.\n\n" +
		"Ανοίξτε τον παρακάτω σύνδεσμο για να ορίσετε νέο κωδικό:\n\n" +
		link + "\n\n" +
		"Ο σύνδεσμος ισχύει για 30 λεπτά και μπορεί να χρησιμοποιηθεί μία φορά.\n" +
		"Αν δεν ζητήσατε εσείς την επαναφορά, μπορείτε να αγνοήσετε αυτό το μήνυμα — ο λογαριασμός σας παραμένει ασφαλής.\n\n" +
		"— Sick Fansubs"
	return subject, body
}
