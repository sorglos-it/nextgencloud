package enc

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"math/big"
	"runtime"
	"strconv"
	"strings"
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
	// A string with escapes takes the slower path and means the same.
	escaped := `"` + "\x5c" + `u0041Pv_"` // "APv_" with the A escaped
	var viaEscape Bytes
	if err := json.Unmarshal([]byte(escaped), &viaEscape); err != nil || string(viaEscape) != string(in) {
		t.Fatalf("escaped: %x %v", viaEscape, err)
	}
}

func TestBytesRejectsNonCanonical(t *testing.T) {
	// The last five differ from "APv_" only in one byte outside the
	// alphabet; encoding/base64 alone would skip the line breaks.
	for _, s := range []string{`"AA=="`, `"+/8"`, `"AB"`, `"AP\nv_"`, `"AP\rv_"`, `"AP v_"`, `"AP\tv_"`, `"APv_="`} {
		var b Bytes
		if err := json.Unmarshal([]byte(s), &b); err == nil {
			t.Errorf("%s: accepted", s)
		}
	}
}

func TestB64Alphabet(t *testing.T) {
	if b, err := B64.DecodeString("APv_"); err != nil || string(b) != "\x00\xfb\xff" {
		t.Fatalf("APv_: %x %v", b, err)
	}
	for _, s := range []string{"AP\nv_", "AP\rv_", "AP v_", "AP\tv_", "APv_=", "APv_\n"} {
		if _, err := B64.DecodeString(s); err == nil {
			t.Errorf("DecodeString %q: accepted", s)
		}
		if _, err := B64.Decode(make([]byte, 8), []byte(s)); err == nil {
			t.Errorf("Decode %q: accepted", s)
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

// budgetDoc is a document with every kind of container a Budget knows.
type budgetDoc struct {
	A    []int           `json:"a,omitempty"`
	M    map[string]int  `json:"m,omitempty"`
	Keys []budgetKey     `json:"keys,omitempty"`
	R    json.RawMessage `json:"r,omitempty"`
	N    int64           `json:"n,omitempty"`
	S    string          `json:"s,omitempty"`
}

type budgetKey struct {
	K []int          `json:"k,omitempty"`
	M map[string]int `json:"m,omitempty"`
}

var testBudget = Budget{Size: 1000, Items: map[string]int{"a": 3, "m": 2, "keys": 2, "keys/*/k": 1, "r": Raw}}

func TestBudget(t *testing.T) {
	nested := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	escapedA := `"` + "\x5c" + `u0061"` // the name "a" written as an escape
	is := func(want error) func(error) bool { return func(err error) bool { return errors.Is(err, want) } }
	cases := []struct {
		name  string
		in    string
		check func(error) bool
	}{
		{"valid", `{"a":[1,2,3],"m":{"x":1,"y":2},"keys":[{"k":[1]},{"k":[]}],"r":{"q":[1,2,3,4]},"n":9007199254740991}`, is(nil)},
		{"array at its budget", `{"a":[1,2,3]}`, is(nil)},
		{"array over its budget", `{"a":[1,2,3,4]}`, is(ErrBudget)},
		{"array without a budget", `{"b":[1]}`, is(ErrBudget)},
		{"map over its budget", `{"m":{"x":1,"y":2,"z":3}}`, is(ErrBudget)},
		{"outer array over its budget", `{"keys":[{},{},{}]}`, is(ErrBudget)},
		{"inner array over its budget", `{"keys":[{"k":[1]},{"k":[1,2]}]}`, is(ErrBudget)},
		{"raw value is not counted", `{"r":[[1,2,3,4,5],{"x":[6,7,8,9]}]}`, is(nil)},
		{"escaped member name", `{` + escapedA + `:[1,2,3]}`, is(nil)},
		{"member name with a slash", `{"keys/*/k":[1]}`, is(ErrName)},
		{"member name *", `{"*":1}`, is(ErrName)},
		{"member name of MaxName bytes", `{"` + strings.Repeat("x", MaxName) + `":1}`, is(jsonv2.ErrUnknownName)},
		{"member name longer than MaxName", `{"` + strings.Repeat("x", MaxName+1) + `":1}`, is(ErrName)},
		{"long member names in a map", `{"m":{"` + strings.Repeat("x", 100) + `":1,"a/*":2}}`, is(nil)},
		{"any member names in a raw value", `{"r":{"` + strings.Repeat("x", 100) + `":1,"a/b":2,"*":3}}`, is(nil)},
		{"null member", `{"s":null}`, is(ErrNull)},
		{"null in an array", `{"a":[1,null]}`, is(ErrNull)},
		{"null in a raw value", `{"r":{"x":null}}`, is(ErrNull)},
		{"depth 32", `{"r":` + nested(31) + `}`, is(nil)},
		{"depth 33", `{"r":` + nested(32) + `}`, is(ErrDepth)},
		{"smallest safe number", `{"n":-9007199254740991}`, is(nil)},
		{"number beyond MaxSafeInteger", `{"n":9007199254740992}`, is(ErrNumber)},
		{"negative number beyond MaxSafeInteger", `{"n":-9007199254740992}`, is(ErrNumber)},
		{"number beyond MaxSafeInteger in a raw value", `{"r":[1e16]}`, is(ErrNumber)},
		{"fraction on top of MaxSafeInteger", `{"r":9007199254740991.5}`, is(ErrNumber)},
		{"larger than Size", `{"s":"` + strings.Repeat("x", 1000) + `"}`, is(ErrTooLarge)},
		{"repeated member, left to DecodeStrict", `{"a":[1],"a":[2]}`, is(jsontext.ErrDuplicateName)},
		{"unknown member, left to DecodeStrict", `{"x":1}`, is(jsonv2.ErrUnknownName)},
	}
	for _, c := range cases {
		var d budgetDoc
		if err := testBudget.Decode([]byte(c.in), &d); !c.check(err) {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
	// A struct object may have MaxMembers members in the first pass (the
	// second one refuses the unknown ones), but not one more.
	members := func(n int) []byte {
		var b bytes.Buffer
		b.WriteString(`{"x0":0`)
		for i := 1; i < n; i++ {
			b.WriteString(`,"x` + strconv.Itoa(i) + `":0`)
		}
		b.WriteString(`}`)
		return b.Bytes()
	}
	if err := testBudget.check(members(MaxMembers)); err != nil {
		t.Errorf("struct object with MaxMembers members: %v", err)
	}
	if err := testBudget.check(members(MaxMembers + 1)); err != ErrBudget {
		t.Errorf("struct object with MaxMembers+1 members: got %v, want %v", err, ErrBudget)
	}
	// A limit below 0 other than Raw is a mistake in the budget: even a
	// document without the array is refused.
	wrong := Budget{Size: 1000, Items: map[string]int{"a": -2}}
	if err := wrong.Decode([]byte(`{}`), &budgetDoc{}); err != errLimit {
		t.Errorf("negative limit: got %v, want %v", err, errLimit)
	}
	// 0 allows only an empty array.
	empty := Budget{Size: 1000, Items: map[string]int{"a": 0}}
	if err := empty.Decode([]byte(`{"a":[]}`), &budgetDoc{}); err != nil {
		t.Errorf("empty array at limit 0: %v", err)
	}
	if err := empty.Decode([]byte(`{"a":[1]}`), &budgetDoc{}); err != ErrBudget {
		t.Errorf("one element at limit 0: got %v, want %v", err, ErrBudget)
	}
}

// totalsBudget caps the k arrays and m maps of all keys together.
var totalsBudget = Budget{Size: 1000,
	Items:  map[string]int{"a": 3, "keys": 4, "keys/*/k": 3, "keys/*/m": 3, "r": Raw},
	Totals: map[string]int{"keys/*/k": 4, "keys/*/m": 3},
}

func TestBudgetTotals(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"total at its limit", `{"keys":[{"k":[1,2]},{"k":[3,4]}]}`, nil},
		{"total over its limit across arrays", `{"keys":[{"k":[1,2]},{"k":[3]},{"k":[4,5]}]}`, ErrBudget},
		{"empty arrays count nothing", `{"keys":[{"k":[]},{"k":[]},{"k":[]},{"k":[1,2,3]}]}`, nil},
		{"map total at its limit", `{"keys":[{"m":{"x":1}},{"m":{"y":2,"z":3}}]}`, nil},
		{"map total over its limit across maps", `{"keys":[{"m":{"x":1,"y":2}},{"m":{"z":3,"w":4}}]}`, ErrBudget},
		{"each total on its own", `{"keys":[{"k":[1,2],"m":{"x":1}},{"k":[3,4],"m":{"y":2,"z":3}}]}`, nil},
		{"a path without a total", `{"a":[1,2,3],"keys":[{"k":[1]}]}`, nil},
		{"raw value is not counted", `{"r":{"keys":[{"k":[1,2,3,4,5,6]}]},"keys":[{"k":[1,2,3]}]}`, nil},
	}
	for _, c := range cases {
		var d budgetDoc
		if err := totalsBudget.Decode([]byte(c.in), &d); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	// 0 allows only empty arrays at the path.
	zero := Budget{Size: 1000, Items: map[string]int{"keys": 4, "keys/*/k": 3}, Totals: map[string]int{"keys/*/k": 0}}
	if err := zero.Decode([]byte(`{"keys":[{"k":[]},{"k":[]}]}`), &budgetDoc{}); err != nil {
		t.Errorf("empty arrays at total 0: %v", err)
	}
	if err := zero.Decode([]byte(`{"keys":[{"k":[]},{"k":[1]}]}`), &budgetDoc{}); err != ErrBudget {
		t.Errorf("one element at total 0: got %v, want %v", err, ErrBudget)
	}
	// A total the budget cannot keep is a mistake in the budget: even a
	// document without the path is refused.
	nine := map[string]int{}
	for i := range 9 {
		nine["p"+strconv.Itoa(i)] = 1
	}
	nineItems := map[string]int{}
	for p := range nine {
		nineItems[p] = 1
	}
	for name, b := range map[string]Budget{
		"total for a path without a limit": {Size: 1000, Items: map[string]int{"a": 3}, Totals: map[string]int{"b": 1}},
		"total for a raw path":             {Size: 1000, Items: map[string]int{"r": Raw}, Totals: map[string]int{"r": 1}},
		"total below 0":                    {Size: 1000, Items: map[string]int{"a": 3}, Totals: map[string]int{"a": -1}},
		"nine totals":                      {Size: 1000, Items: nineItems, Totals: nine},
	} {
		if err := b.Decode([]byte(`{}`), &budgetDoc{}); err != errLimit {
			t.Errorf("%s: got %v, want %v", name, err, errLimit)
		}
	}
}

func TestSafeNumber(t *testing.T) {
	cases := map[string]bool{
		"0": true, "-0": true, "0.0": true, "0e999": true, "1": true, "-1": true, "-0.5": true,
		"9007199254740991": true, "-9007199254740991": true, "9007199254740991.0": true, "9007199254740990.9": true,
		"9007199254740992": false, "-9007199254740992": false, "9007199254740991.5": false,
		"1e15": true, "1E+15": true, "1e16": false, "1e-400": true, "1E400": false,
		"9.007199254740991e15": true, "9.007199254740992e15": false,
		"90071992547409910e-1": true, "90071992547409911e-1": false,
		"18446744073709551615": false, "999999999999999": true, "9999999999999999": false,
		"1e" + strings.Repeat("9", 100): false, "1e-" + strings.Repeat("9", 100): true,
		"0." + strings.Repeat("0", 1000) + "9007199254740992e1016": false,
		"0." + strings.Repeat("0", 1000) + "9007199254740991e1016": true,
	}
	for s, want := range cases {
		if got := safeNumber([]byte(s)); got != want {
			t.Errorf("%s: got %v, want %v", s, got, want)
		}
		if exact, ok := exactlySafe(s); ok && exact != want {
			t.Errorf("%s: math/big says %v", s, exact)
		}
	}
}

// exactlySafe decides the rule of safeNumber with math/big, as a second
// opinion. The precision keeps every difference to MaxSafeInteger that s can
// express.
func exactlySafe(s string) (safe, ok bool) {
	f, _, err := big.ParseFloat(s, 10, uint(64+4*len(s)), big.ToNearestEven)
	if err != nil {
		return false, false
	}
	return f.IsInf() == false && f.Abs(f).Cmp(new(big.Float).SetInt64(MaxSafeInteger)) <= 0, true
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// fill returns prefix, then elem repeated with sep, then suffix, padded with
// spaces to exactly size bytes.
func fill(prefix, elem, sep, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + elem)
	for b.Len()+len(sep)+len(elem)+len(suffix) <= size {
		b.WriteString(sep + elem)
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestBudgetAllocation feeds hostile documents of the full size and checks
// that refusing them allocates next to nothing.
func TestBudgetAllocation(t *testing.T) {
	const size = 32 << 20
	b := Budget{Size: size, Items: map[string]int{"a": 64, "m": 64, "r": Raw}}
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"array of empty objects", fill(`{"a":[`, `{}`, `,`, `]}`, size), ErrBudget},
		{"array without a budget", fill(`{"b":[`, `{}`, `,`, `]}`, size), ErrBudget},
		{"deep nesting", fill(`{"r":`, `[`, ``, `]}`, size), ErrDepth},
		{"members of a struct", fill(`{`, `"x":1`, `,`, `}`, size), ErrBudget},
		{"one long member name", fill(`{"`, `x`, ``, `":1}`, size), ErrName},
		{"numbers in a raw value", fill(`{"r":[`, `1e5`, `,`, `,null]}`, size), ErrNull},
		{"a map of many members", fill(`{"m":{`, `"x":1`, `,`, `}}`, size), ErrBudget},
		{"many small arrays over a total", fill(`{"keys":[`, `{"k":[1]}`, `,`, `]}`, size), ErrBudget},
	}
	// Each array keeps its own limit; only the total refuses the document.
	b.Items["keys"], b.Items["keys/*/k"] = size, 1
	b.Totals = map[string]int{"keys/*/k": 1000}
	for _, c := range cases {
		var d budgetDoc
		var err error
		n := allocated(func() { err = b.Decode(c.in, &d) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
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

// Run with -fuzzminimizetime=1s: the fuzzer's minimizer stalls on this target otherwise.
func FuzzBudget(f *testing.F) {
	f.Add(`{"a":[1,2,3],"m":{"x":1},"keys":[{"k":[1]}],"r":{"q":[1,{"z":2}]},"n":1}`)
	f.Add(`{"a":[1,2,3,4]}`)
	f.Add(`{"r":[[[[[[]]]]]]}`)
	f.Add(`{"n":9007199254740992}`)
	f.Add(`{"s":null}`)
	f.Add(`{"keys":[{"k":[1,2],"m":{"x":1}},{"k":[3,4],"m":{"y":2,"z":3}}]}`)
	f.Fuzz(func(t *testing.T, s string) {
		// With totals, what passes keeps them.
		var capped budgetDoc
		if totalsBudget.Decode([]byte(s), &capped) == nil {
			k, m := 0, 0
			for _, key := range capped.Keys {
				k, m = k+len(key.K), m+len(key.M)
			}
			if k > totalsBudget.Totals["keys/*/k"] || m > totalsBudget.Totals["keys/*/m"] {
				t.Fatalf("over a total for %q: %d, %d", s, k, m)
			}
		}
		var d budgetDoc
		if testBudget.Decode([]byte(s), &d) != nil {
			return
		}
		// What the first pass lets through stays within the budget...
		if len(d.A) > 3 || len(d.M) > 2 || len(d.Keys) > 2 {
			t.Fatalf("over budget for %q", s)
		}
		for _, k := range d.Keys {
			if len(k.K) > 1 {
				t.Fatalf("over budget for %q", s)
			}
		}
		// ...and encodes to a document the budget accepts again (escaping
		// may make it longer, which is not under test here).
		out, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var back budgetDoc
		if err := (Budget{Size: len(out), Items: testBudget.Items}).Decode(out, &back); err != nil {
			t.Fatalf("re-decode failed for %q -> %q: %v", s, out, err)
		}
	})
}

func FuzzSafeNumber(f *testing.F) {
	f.Add("9007199254740991")
	f.Add("9007199254740991.5")
	f.Add("-1e16")
	f.Add("0.000123e20")
	f.Fuzz(func(t *testing.T, s string) {
		v := jsontext.Value(s)
		if len(s) > 1000 || strings.TrimSpace(s) != s || !v.IsValid() || v.Kind() != '0' {
			return
		}
		if exact, ok := exactlySafe(s); ok && exact != safeNumber(v) {
			t.Fatalf("%s: safeNumber says %v, math/big %v", s, !exact, exact)
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
