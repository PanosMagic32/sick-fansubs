package mail

// VerifyEmail builds the registration-verification message: plain text,
// Greek copy, one verification link. The link is constructed by the caller
// (service layer) from PUBLIC_BASE_URL + the base64url token; only that
// validated URL and the token ever reach the message.
func VerifyEmail(link string) (subject, body string) {
	subject = "Επιβεβαίωση email — Sick Fansubs"
	// Polite register, matching the reset message.
	body = "Καλώς ήρθατε στο Sick Fansubs!\n\n" +
		"Ανοίξτε τον παρακάτω σύνδεσμο για να επιβεβαιώσετε τη διεύθυνση email σας:\n\n" +
		link + "\n\n" +
		"Ο σύνδεσμος ισχύει για 7 ημέρες και μπορεί να χρησιμοποιηθεί μία φορά.\n" +
		"Αν δεν δημιουργήσατε εσείς αυτόν τον λογαριασμό, μπορείτε να αγνοήσετε αυτό το μήνυμα.\n\n" +
		"— Sick Fansubs"
	return subject, body
}
