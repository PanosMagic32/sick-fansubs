package handler

import (
	"bytes"
	"encoding/json"
	"errors"
)

// errNullOptionalField rejects an explicit JSON null for a field the contract
// documents as an optional STRING. Absent means "use the default"; null is a
// present value of the wrong type. The strict body reader maps the returned
// error to its generic invalid-body 400, the same class as any other type
// mismatch.
var errNullOptionalField = errors.New("null is not an accepted value for this field")

// optionalString distinguishes an absent JSON key (set=false, value zero) from
// a present string. Decoding null fails instead of collapsing into the absent
// arm — a client that serializes an unset optional field as null must not
// silently publish content or keep a stale default.
type optionalString struct {
	value string
	set   bool
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errNullOptionalField
	}
	o.set = true
	return json.Unmarshal(data, &o.value)
}
