package search

import "errors"

// ErrNoTokens reports a query that sanitizes down to nothing: only stripped
// specials, reserved words, or tokens without a searchable letter or number
// remained. Handlers map it to a 422 violation {q, invalidFormat}.
var ErrNoTokens = errors.New("search: query contains no searchable tokens")
