// Package enc holds the encodings shared by all NGC contracts.
package enc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// B64 is the only binary-to-text encoding in NGC contracts: base64url without
// padding (RFC 4648, section 5). Decoding is strict: non-zero trailing bits
// are rejected, so every byte string has exactly one encoding.
var B64 = base64.RawURLEncoding.Strict()

// Bytes is a byte slice that encodes as a base64url string in JSON.
type Bytes []byte

// MarshalJSON implements json.Marshaler.
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(B64.EncodeToString(b))
}

// UnmarshalJSON implements json.Unmarshaler.
func (b *Bytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := B64.DecodeString(s)
	if err != nil {
		return err
	}
	*b = v
	return nil
}

var (
	// ErrTooLarge reports input above the allowed size.
	ErrTooLarge = errors.New("enc: input too large")
	// ErrTrailingData reports bytes after the first JSON value.
	ErrTrailingData = errors.New("enc: trailing data after JSON value")
)

// DecodeStrict decodes exactly one JSON value from data into v. It rejects
// inputs longer than max bytes, unknown object fields and trailing data.
func DecodeStrict(data []byte, max int, v any) error {
	if len(data) > max {
		return ErrTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrTrailingData
	}
	return nil
}
