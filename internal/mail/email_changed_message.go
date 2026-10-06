package mail

// EmailChangedNotice builds the security notice sent to an account's PREVIOUS
// address after an email change: plain text, Greek copy, no link and no token.
func EmailChangedNotice(newEmail string) (subject, body string) {
	subject = "Αλλαγή email — Sick Fansubs"
	// Polite register — the app's auth pages use the formal plural
	// ("Συνδεθείτε", "Ξεχάσατε τον κωδικό σας"), and the email copy
	// matches it.
	body = "Το email του λογαριασμού σας στο Sick Fansubs άλλαξε σε:\n\n" +
		newEmail + "\n\n" +
		"Αν κάνατε εσείς την αλλαγή, μπορείτε να αγνοήσετε αυτό το μήνυμα.\n" +
		"Αν δεν κάνατε εσείς την αλλαγή, αλλάξτε αμέσως τον κωδικό πρόσβασης από τη σελίδα του λογαριασμού σας. Αν δεν μπορείτε να συνδεθείτε, επικοινωνήστε με τους διαχειριστές.\n\n" +
		"— Sick Fansubs"
	return subject, body
}
