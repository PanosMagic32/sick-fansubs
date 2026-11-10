package handler

import (
	"bytes"
	"encoding/json"
)

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
