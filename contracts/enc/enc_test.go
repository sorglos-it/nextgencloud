package enc

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"strconv"
	"testing"
)

func TestBytesRoundTrip(t *testing.T) {
	in := Bytes{0x00, 0xfb, 0xff}
	out, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"APv_"` {
		t.Fatalf("got %s", out)
	}
	var back Bytes
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if string(back) != string(in) {
		t.Fatalf("round trip: %x", back)
	}
}

func TestBytesRejectsNonCanonical(t *testing.T) {
	for _, s := range []string{`"AA=="`, `"+/8"`, `"AB"`} {
		var b Bytes
		if err := json.Unmarshal([]byte(s), &b); err == nil {
			t.Errorf("%s: accepted", s)
		}
	}
}

func TestDecodeStrict(t *testing.T) {
	type doc struct {
		A int             `json:"a"`
		R json.RawMessage `json:"r,omitempty"`
		S string          `json:"s,omitempty"`
		U uint8           `json:"u,omitempty"`
	}
	// Each check names the reason an input must fail, so a case cannot pass
	// by failing for some other rule.
	accepted := func(err error) bool { return err == nil }
	duplicate := func(err error) bool { return errors.Is(err, jsontext.ErrDuplicateName) }
	unknown := func(err error) bool { return errors.Is(err, jsonv2.ErrUnknownName) }
	syntax := func(err error) bool {
		var e *jsontext.SyntacticError
		return errors.As(err, &e) && !duplicate(err)
	}
	outOfRange := func(err error) bool { return errors.Is(err, strconv.ErrRange) }
	is := func(want error) func(error) bool { return func(err error) bool { return err == want } }
	cases := []struct {
		name  string
		in    string
		max   int
		check func(error) bool
	}{
		{"valid", `{"a":1}`, 100, accepted},
		{"unknown field", `{"a":1,"b":2}`, 100, unknown},
		{"duplicate member", `{"a":1,"a":2}`, 100, duplicate},
		{"member differs only in case", `{"A":1}`, 100, unknown},
		{"second member differs only in case", `{"a":1,"A":2}`, 100, unknown},
		{"duplicate inside a raw value", `{"a":1,"r":{"x":1,"x":2}}`, 100, duplicate},
		{"raw value without duplicates", `{"a":1,"r":{"x":1,"y":2}}`, 100, accepted},
		{"invalid UTF-8", "{\"a\":1,\"s\":\"\xff\"}", 100, syntax},
		{"valid UTF-8", `{"a":1,"s":"Käse"}`, 100, accepted},
		{"trailing data", `{"a":1} {}`, 100, is(ErrTrailingData)},
		{"trailing whitespace", "{\"a\":1} \n", 100, accepted},
		{"number out of range", `{"a":1,"u":256}`, 100, outOfRange},
		{"largest number in range", `{"a":1,"u":255}`, 100, accepted},
		{"too large", `{"a":1}`, 3, is(ErrTooLarge)},
	}
	for _, c := range cases {
		var d doc
		if err := DecodeStrict([]byte(c.in), c.max, &d); !c.check(err) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}

	var d doc
	if err := DecodeStrict([]byte(`{"a":1}`), 100, &d); err != nil || d.A != 1 {
		t.Fatalf("valid: %v %v", err, d)
	}
	// "raw value kept byte for byte": whitespace and characters that
	// encoding/json would escape survive unchanged.
	if err := DecodeStrict([]byte(`{"a":1,"r":{ "x" : "<&>" }}`), 100, &d); err != nil || string(d.R) != `{ "x" : "<&>" }` {
		t.Errorf("raw value kept byte for byte: %v %q", err, d.R)
	}
}

func FuzzBytes(f *testing.F) {
	f.Add(`"APv_"`)
	f.Add(`"AA=="`)
	f.Fuzz(func(t *testing.T, s string) {
		var b Bytes
		if json.Unmarshal([]byte(s), &b) != nil {
			return
		}
		out, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		var back Bytes
		if err := json.Unmarshal(out, &back); err != nil || string(back) != string(b) {
			t.Fatalf("round trip failed for %q", s)
		}
	})
}

func FuzzDecodeStrict(f *testing.F) {
	type doc struct {
		A int             `json:"a"`
		B Bytes           `json:"b"`
		R json.RawMessage `json:"r,omitempty"`
	}
	f.Add(`{"a":1,"b":"APv_"}`)
	f.Add(`{"a":1,"b":"APv_","c":2}`)
	f.Add(`{"a":1,"b":"APv_"} {}`)
	f.Add(`{"a":1,"a":2}`)
	f.Add(`{"A":1}`)
	f.Add(`{"a":1,"r":{ "x" : "<&>" }}`)
	f.Add(`not json`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, s string) {
		var d doc
		if DecodeStrict([]byte(s), 100, &d) != nil {
			return
		}
		out, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		// The size limit is not under test here: the output may name members
		// the input left out.
		var back doc
		if err := DecodeStrict(out, len(out), &back); err != nil {
			t.Fatalf("re-decode failed for %q -> %q: %v", s, out, err)
		}
	})
}
