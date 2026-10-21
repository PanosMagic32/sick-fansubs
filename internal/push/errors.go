package push

import "errors"

// The push package's error vocabulary. Sentinels are tested with errors.Is and
// name the send outcome (docs/patterns/go/errors.md).
var (
	// errPayloadOverBudget reports a body that cannot fit even after the title
	// is trimmed away — a bound drifted without the ceiling, so the send fails
	// closed rather than letting a push service answer 413.
	errPayloadOverBudget = errors.New("push: payload exceeds the bridged record budget with an empty title")
)
