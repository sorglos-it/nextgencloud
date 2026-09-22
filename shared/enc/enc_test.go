package enc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"io"
	"math/big"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/internal/golden"
)

var update = flag.Bool("update", false, "rewrite shared/testdata")

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

// vectorDoc is the document of json.json (shared/README.md, "Striktes JSON").
type vectorDoc struct {
	N    int64            `json:"n,omitempty"`
	U    uint32           `json:"u,omitempty"`
	S    string           `json:"s,omitempty"`
	T    bool             `json:"t,omitempty"`
	B    Bytes            `json:"b,omitempty"`
	List []int64          `json:"list,omitempty"`
	Map  map[string]int64 `json:"map,omitempty"`
	Sub  *vectorSub       `json:"sub,omitempty"`
	Subs []vectorSub      `json:"subs,omitempty"`
	Raw  json.RawMessage  `json:"raw,omitempty"`
}

type vectorSub struct {
	X    int64   `json:"x,omitempty"`
	List []int64 `json:"list,omitempty"`
}

var vectorBudget = Budget{Size: 1024,
	Items:  map[string]int{"list": 4, "map": 2, "sub/list": 4, "subs": 3, "subs/*/list": 4, "raw": Raw},
	Totals: map[string]int{"subs/*/list": 6}}

// vectorSchema describes vectorDoc to an implementation in another language,
// which builds the same document type from it: "[T]" is a list of T,
// "{string:T}" a map, an object a struct, "[sub]" a list of structs like
// sub, and "raw" any JSON value, kept as it stands.
const vectorSchema = `{"n":"int64","u":"uint32","s":"string","t":"bool","b":"bytes","list":"[int64]",` +
	`"map":"{string:int64}","sub":{"x":"int64","list":"[int64]"},"subs":"[sub]","raw":"raw"}`

// jsonVectors is the content of json.json.
type jsonVectors struct {
	Schema json.RawMessage `json:"schema"`
	Size   int             `json:"size"`
	Items  map[string]int  `json:"items"`
	Totals map[string]int  `json:"totals"`
	Cases  []jsonVector    `json:"cases"`
}

// jsonVector is one case of json.json. Doc is base64url because a BOM and
// invalid UTF-8 cannot stand in a JSON string.
type jsonVector struct {
	Name  string `json:"name"`
	Doc   Bytes  `json:"doc"`
	Valid bool   `json:"valid"`
}

// jsonCase is a document of json.json with the reason Go refuses it: refused
// is nil for a valid document.
type jsonCase struct {
	name    string
	doc     string
	refused func(error) bool
}

// jsonCases: valid documents, then invalid ones that each break exactly one
// rule. refused names that rule as Go reports it, so a case cannot pass by
// breaking another one by mistake.
func jsonCases() []jsonCase {
	nested := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	// A document of exactly n bytes.
	sized := func(n int) string { return `{"s":"` + strings.Repeat("x", n-len(`{"s":""}`)) + `"}` }
	// The JSON escape \u and hex, spelled with "\x5c" so that no tool on the
	// way turns it into the character itself.
	escape := func(hex string) string { return "\x5c" + "u" + hex }
	is := func(want error) func(error) bool { return func(err error) bool { return errors.Is(err, want) } }
	duplicate := is(jsontext.ErrDuplicateName)
	unknown := is(jsonv2.ErrUnknownName)
	syntax := func(err error) bool {
		var e *jsontext.SyntacticError
		return errors.As(err, &e) && !duplicate(err)
	}
	// A JSON kind that the Go type cannot hold, such as a number for a string.
	kind := func(err error) bool {
		var e *jsonv2.SemanticError
		return errors.As(err, &e) && e.Err == nil
	}
	notBase64url := func(err error) bool {
		var e base64.CorruptInputError
		return errors.As(err, &e)
	}
	return []jsonCase{
		{"empty object", `{}`, nil},
		{"every field", `{"n":-5,"u":7,"s":"ä","t":true,"b":"AQID","list":[1,2,3,4],"map":{"a":1,"b":2},` +
			`"sub":{"x":1,"list":[1]},"subs":[{"list":[1,2]},{"list":[3,4]},{"list":[5,6]}],"raw":{"k":[1,{"z":"y"}],"K":2}}`, nil},
		{"white space around", " \t\r\n{}\n ", nil},
		{"largest safe integer", `{"n":9007199254740991}`, nil},
		{"smallest safe integer", `{"n":-9007199254740991}`, nil},
		{"minus zero in a signed field", `{"n":-0}`, nil},
		{"unsigned zero", `{"u":0}`, nil},
		{"decimal point on a safe integer in raw", `{"raw":9007199254740991.0}`, nil},
		{"names differing in case in raw", `{"raw":{"k":1,"K":2}}`, nil},
		{"escaped control character in a string", `{"s":"a` + escape("0001") + `b"}`, nil},
		{"bytes with zero trailing bits", `{"b":"AQ"}`, nil},
		{"depth 32", `{"raw":` + nested(31) + `}`, nil},
		{"document of 1024 bytes", sized(1024), nil},

		{"empty document", ``, is(io.EOF)},
		{"only white space", " \t\r\n", is(io.EOF)},
		{"byte order mark", "\xef\xbb\xbf{}", syntax},
		{"trailing data", `{} {}`, is(ErrTrailingData)},
		{"array as the document", `[]`, is(ErrBudget)},
		{"unknown field", `{"x":1}`, unknown},
		{"field name in other case", `{"N":1}`, unknown},
		{"duplicate field", `{"n":1,"n":2}`, duplicate},
		{"duplicate after unescaping", `{"n":1,"` + escape("006e") + `":2}`, duplicate},
		{"duplicate inside raw", `{"raw":{"k":1,"k":2}}`, duplicate},
		{"duplicate inside raw after unescaping", `{"raw":{"k":1,"` + escape("006b") + `":2}}`, duplicate},
		{"invalid UTF-8", "{\"s\":\"\xff\"}", syntax},
		{"raw control character in a string", "{\"s\":\"a\x01b\"}", syntax},
		{"lone surrogate escape", `{"s":"` + escape("d800") + `"}`, syntax},
		{"null field", `{"n":null}`, is(ErrNull)},
		{"null in raw", `{"raw":[null]}`, is(ErrNull)},
		{"decimal point in an integer field", `{"n":1.0}`, is(strconv.ErrSyntax)},
		{"exponent in an integer field", `{"n":1e2}`, is(strconv.ErrSyntax)},
		{"minus zero in an unsigned field", `{"u":-0}`, is(strconv.ErrSyntax)},
		{"negative unsigned", `{"u":-1}`, is(strconv.ErrSyntax)},
		{"uint32 overflow", `{"u":4294967296}`, is(strconv.ErrRange)},
		{"integer beyond 2^53-1", `{"n":9007199254740992}`, is(ErrNumber)},
		{"fraction on the largest safe integer in raw", `{"raw":9007199254740991.5}`, is(ErrNumber)},
		{"exponent beyond 2^53-1 in raw", `{"raw":1e16}`, is(ErrNumber)},
		{"number for a string", `{"s":1}`, kind},
		{"string for a number", `{"n":"1"}`, kind},
		{"number for a bool", `{"t":1}`, kind},
		{"bytes with padding", `{"b":"AQ=="}`, notBase64url},
		{"bytes with a line feed", `{"b":"AQ\n"}`, notBase64url},
		{"bytes with non-zero trailing bits", `{"b":"AR"}`, notBase64url},
		{"bytes in the standard alphabet", `{"b":"+/8"}`, notBase64url},
		{"list over its limit", `{"list":[1,2,3,4,5]}`, is(ErrBudget)},
		{"map over its limit", `{"map":{"a":1,"b":2,"c":3}}`, is(ErrBudget)},
		{"list in sub over its limit", `{"sub":{"list":[1,2,3,4,5]}}`, is(ErrBudget)},
		{"total over its limit", `{"subs":[{"list":[1,2,3]},{"list":[4,5,6]},{"list":[7]}]}`, is(ErrBudget)},
		{"depth 33", `{"raw":` + nested(32) + `}`, is(ErrDepth)},
		{"document of 1025 bytes", sized(1025), is(ErrTooLarge)},
	}
}

// TestJSONVectors writes json.json: the strict reader and the budget of the
// web interface decode each case into the document of the schema and must
// refuse exactly the cases Go refuses.
func TestJSONVectors(t *testing.T) {
	v := jsonVectors{Schema: json.RawMessage(vectorSchema), Size: vectorBudget.Size, Items: vectorBudget.Items,
		Totals: vectorBudget.Totals}
	for _, c := range jsonCases() {
		err := vectorBudget.Decode([]byte(c.doc), &vectorDoc{})
		switch {
		case c.refused == nil && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.refused != nil && !c.refused(err):
			t.Errorf("%s: refused for another reason, or not at all: %v", c.name, err)
		}
		v.Cases = append(v.Cases, jsonVector{Name: c.name, Doc: Bytes(c.doc), Valid: c.refused == nil})
	}
	golden.Check(t, *update, "json.json", v)
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
