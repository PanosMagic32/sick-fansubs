package auth

import "strings"

// sessionClientLabelMaxLen bounds the stored device label. It is a guard
// rail, not a truncation point: the closed vocabulary below is far shorter,
// so a value longer than this can only mean the vocabulary grew past its
// contract — in which case the label is withheld rather than stored
// unbounded.
const sessionClientLabelMaxLen = 32

// clientLabelSeparator joins the browser and platform halves of a label.
const clientLabelSeparator = " · "

// browserTokens maps a User-Agent substring to its browser-family label.
//
// ORDER IS THE CONTRACT: every Chromium-based browser also sends "Safari/"
// and most also send "Chrome/", so the more specific families must be tested
// before the generic ones. Matching is a plain case-sensitive substring test
// — the tokens are stable user-agent registrations, not free text.
var browserTokens = []struct{ token, label string }{
	{"Edg", "Edge"}, // desktop "Edg/", Android "EdgA/", iOS "EdgiOS/"
	{"OPiOS", "Opera"},
	{"OPR", "Opera"},
	{"SamsungBrowser", "Samsung Internet"},
	{"CriOS", "Chrome"},  // iOS Chrome names itself "CriOS/"
	{"FxiOS", "Firefox"}, // iOS Firefox names itself "FxiOS/"
	{"Chrome", "Chrome"},
	{"Firefox", "Firefox"},
	{"Safari", "Safari"},
}

// platformTokens maps a User-Agent substring to its platform-family label.
// Order matters here too: iPhone/iPad UAs also carry "like Mac OS X", so the
// iOS tokens must come first.
//
// Known limits of the source, not of the classifier: iPadOS 13+ and recent
// macOS Safari build their user-agent from the desktop Safari template, so
// both can classify as macOS. A UA with no recognized platform still yields
// the browser half alone.
var platformTokens = []struct{ token, label string }{
	{"Windows", "Windows"},
	{"Android", "Android"},
	{"iPhone", "iOS"},
	{"iPad", "iOS"},
	{"iPod", "iOS"},
	{"Mac OS X", "macOS"},
	{"Macintosh", "macOS"},
	{"Linux", "Linux"},
}

// sessionClientLabel classifies a User-Agent request header into the bounded
// device label stored with a new session.
//
// The privacy contract IS the function: the raw user-agent string is never
// stored, never logged, and never returned. The result is either the empty
// string — stored as SQL NULL and rendered as "unknown device" — or a string
// assembled from the two token tables above, so client text can never reach
// the database through this path.
//
// The label carries a browser family and/or a platform family joined by
// " · ", for example "Chrome · Android". Only the halves that were
// recognized appear; a UA that matches neither yields "".
func sessionClientLabel(userAgent string) string {
	if userAgent == "" {
		return ""
	}

	var parts []string
	if b := matchToken(userAgent, browserTokens); b != "" {
		parts = append(parts, b)
	}
	if p := matchToken(userAgent, platformTokens); p != "" {
		parts = append(parts, p)
	}

	label := strings.Join(parts, clientLabelSeparator)
	if len(label) > sessionClientLabelMaxLen {
		// Unreachable with the current vocabulary (pinned by test). Withhold
		// rather than store an unbounded value.
		return ""
	}
	return label
}

// matchToken returns the first table entry whose token appears in ua.
func matchToken(ua string, table []struct{ token, label string }) string {
	for _, entry := range table {
		if strings.Contains(ua, entry.token) {
			return entry.label
		}
	}
	return ""
}
