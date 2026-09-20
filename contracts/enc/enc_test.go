package enc

import (
	"encoding/json"
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
		A int `json:"a"`
	}
	var d doc
	if err := DecodeStrict([]byte(`{"a":1}`), 100, &d); err != nil || d.A != 1 {
		t.Fatalf("valid: %v %v", err, d)
	}
	if err := DecodeStrict([]byte(`{"a":1,"b":2}`), 100, &d); err == nil {
		t.Error("unknown field accepted")
	}
	if err := DecodeStrict([]byte(`{"a":1} {}`), 100, &d); err != ErrTrailingData {
		t.Errorf("trailing data: %v", err)
	}
	if err := DecodeStrict([]byte(`{"a":1}`), 3, &d); err != ErrTooLarge {
		t.Errorf("too large: %v", err)
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
		A int   `json:"a"`
		B Bytes `json:"b"`
	}
	f.Add(`{"a":1,"b":"APv_"}`)
	f.Add(`{"a":1,"b":"APv_","c":2}`)
	f.Add(`{"a":1,"b":"APv_"} {}`)
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
		var back doc
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("re-decode failed for %q -> %q: %v", s, out, err)
		}
	})
}
