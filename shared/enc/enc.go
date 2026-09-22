// Package enc holds the encodings shared by all NGC contracts.
package enc

import (
	"bytes"
	"encoding/base64"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io"
)

// B64 is the only binary-to-text encoding in NGC contracts: base64url without
// padding (RFC 4648, section 5). Decoding is strict: every byte must be in the
// alphabet A-Z a-z 0-9 - _ (encoding/base64 alone would skip CR and LF), and
// non-zero trailing bits are refused, so every byte string has exactly one
// encoding. B64 has the methods EncodeToString, EncodedLen, DecodedLen,
// Decode and DecodeString of encoding/base64.
var B64 = strictBase64{base64.RawURLEncoding.Strict()}

// strictBase64 wraps the encoding with only the methods the contracts use,
// so that no way to decode skips the alphabet check.
type strictBase64 struct{ enc *base64.Encoding }

func (s strictBase64) EncodeToString(src []byte) string { return s.enc.EncodeToString(src) }
func (s strictBase64) EncodedLen(n int) int             { return s.enc.EncodedLen(n) }
func (s strictBase64) DecodedLen(n int) int             { return s.enc.DecodedLen(n) }

// Decode decodes src into dst, as base64.Encoding.Decode does, once every
// byte of src is known to be in the alphabet.
func (s strictBase64) Decode(dst, src []byte) (int, error) {
	if i := outsideAlphabet(src); i >= 0 {
		return 0, base64.CorruptInputError(i)
	}
	return s.enc.Decode(dst, src)
}

// DecodeString decodes str, as base64.Encoding.DecodeString does, once every
// byte of str is known to be in the alphabet.
func (s strictBase64) DecodeString(str string) ([]byte, error) {
	if i := outsideAlphabet(str); i >= 0 {
		return nil, base64.CorruptInputError(i)
	}
	return s.enc.DecodeString(str)
}

// outsideAlphabet returns the index of the first byte of s outside the
// base64url alphabet, or -1.
func outsideAlphabet[S ~[]byte | ~string](s S) int {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_') {
			return i
		}
	}
	return -1
}

// Bytes is a byte slice that encodes as a base64url string in JSON.
type Bytes []byte

// MarshalJSON implements json.Marshaler.
func (b Bytes) MarshalJSON() ([]byte, error) {
	return jsonv1.Marshal(B64.EncodeToString(b))
}

// UnmarshalJSON implements json.Unmarshaler.
func (b *Bytes) UnmarshalJSON(data []byte) error {
	// base64url needs no escapes, so a string without a backslash is decoded
	// straight from the input: a copy first would double the cost of a large
	// payload. The alphabet is checked before the output is allocated.
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' && bytes.IndexByte(data, '\\') < 0 {
		src := data[1 : len(data)-1]
		if i := outsideAlphabet(src); i >= 0 {
			return base64.CorruptInputError(i)
		}
		v := make([]byte, B64.DecodedLen(len(src)))
		n, err := B64.enc.Decode(v, src)
		if err != nil {
			return err
		}
		*b = v[:n]
		return nil
	}
	var s string
	if err := jsonv1.Unmarshal(data, &s); err != nil {
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
// inputs longer than max bytes, unknown object members, members whose name
// differs from the Go field's JSON name (also only in case), duplicate member
// names at any depth (also inside json.RawMessage values), invalid UTF-8 and
// trailing data. json.RawMessage values keep the input bytes exactly, which
// noisep.E2EPrologue relies on. Go 1.27 enables encoding/json/v2 by default;
// never build with GOEXPERIMENT=nojsonv2.
//
// DecodeStrict does not bound what decoding allocates; parsers of documents
// from outside use a Budget.
func DecodeStrict(data []byte, max int, v any) error {
	if len(data) > max {
		return ErrTooLarge
	}
	// On a bytes.Buffer the decoder parses in place instead of copying the
	// input into a buffer of its own.
	dec := jsontext.NewDecoder(bytes.NewBuffer(data))
	if err := json.UnmarshalDecode(dec, v, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); err != io.EOF {
		return ErrTrailingData
	}
	return nil
}

// Rules of every document decoded through a Budget, on top of those of
// DecodeStrict. No contract needs deeper nesting, integers that JavaScript
// cannot hold exactly, null, or structs with more or longer member names
// than these, so Go and the browser can refuse the same documents.
const (
	MaxDepth       = 32        // arrays and objects nested in each other; the top-level value is level 1
	MaxSafeInteger = 1<<53 - 1 // largest magnitude of a number: JavaScript's Number.MAX_SAFE_INTEGER
	MaxMembers     = 64        // most members of a struct object
	MaxName        = 64        // longest member name of a struct object, in bytes after unescaping
	Raw            = -1        // Budget.Items value for a member that stays raw JSON (json.RawMessage)
)

var (
	// ErrBudget reports an array or object with more elements (members) than
	// its budget allows, or an array the budget does not list.
	ErrBudget = errors.New("enc: array or object beyond its budget")
	// ErrDepth reports nesting deeper than MaxDepth.
	ErrDepth = errors.New("enc: nested deeper than MaxDepth")
	// ErrNumber reports a number whose magnitude exceeds MaxSafeInteger.
	ErrNumber = errors.New("enc: number beyond MaxSafeInteger")
	// ErrNull reports a null.
	ErrNull = errors.New("enc: null")
	// ErrName reports a member name no struct field can have.
	ErrName = errors.New("enc: member name no struct field has")

	// errLimit reports a budget with a limit below 0 other than Raw, or a
	// total it cannot keep: a mistake in the contract, not in the document.
	errLimit = errors.New("enc: budget with a negative limit other than Raw or a wrong total")
)

// maxTotals is the most totals one budget may have.
const maxTotals = 8

// Budget bounds one kind of JSON document from outside. Decode checks the
// document against it in a first pass that builds nothing and allocates a
// small fixed amount whatever the input, so no array or map is decoded with
// more elements than its contract allows, and nothing large is copied for a
// document that is refused anyway:
//
//   - Size: the document has at most Size bytes.
//   - Items: every array, and every object that decodes into a Go map, is
//     listed by its path with the most elements (members of a map) it may
//     hold; 0 allows only an empty one. A path joins the member names from the
//     top-level value with "/"; "*" stands for each element of an array and
//     each member of a map, so "users/*/keys" is the keys array of every user.
//     An array whose path is not listed is refused. A limit below 0 other than
//     Raw is a mistake in the budget itself: Decode then refuses every
//     document.
//   - An object whose path is not listed is a struct: at most MaxMembers
//     members, whose names have at most MaxName bytes, contain no "/" and are
//     not "*". DecodeStrict refuses unknown and repeated members. The limits
//     keep a map whose path is missing from the budget small, and keep long
//     names from being copied into the error of a refused member.
//   - Totals: a path of Items may also cap all its arrays (maps) together: the
//     elements (members) of every array (map) at that path in the document,
//     added up. For "apps/*/versions" that is the versions of all apps. A
//     total for a path Items does not list as an array or map, a total below
//     0, or more than eight totals are a mistake in the budget itself: Decode
//     then refuses every document.
//   - Every path of a json.RawMessage must be listed as Raw. The first pass
//     counts nothing inside a raw value and does not check its member names,
//     but a raw value is not free: DecodeStrict keeps the member names of
//     each object in it to refuse repeated ones, which costs up to about 15
//     times the value's size. So a raw member needs its own small size bound
//     in its contract; a small Size of the whole document is one.
//   - Everywhere, raw values included: at most MaxDepth levels of nesting,
//     no number whose exact decimal value exceeds MaxSafeInteger in magnitude
//     (9007199254740991.0 passes; 1e16 does not, nor does 9007199254740991.4,
//     although a float64 rounds it down to MaxSafeInteger), and no null: a
//     member without a value is left out instead.
type Budget struct {
	Size   int
	Items  map[string]int
	Totals map[string]int
}

// Decode checks data against b, then decodes it into v with DecodeStrict.
func (b Budget) Decode(data []byte, v any) error {
	if err := b.check(data); err != nil {
		return err
	}
	return DecodeStrict(data, b.Size, v)
}

var anySegment = []byte("*")

// frame is one open array or object in the first pass.
type frame struct {
	base   int  // length of the path that names this container
	limit  int  // most elements or members; -1 inside a raw value, which is not counted
	n      int  // elements or members so far
	object bool // names and values alternate
	mapped bool // an object counted like an array, whose members' path is "*"
	raw    bool // inside a raw value
	name   bool // an object that expects a name or its end next
	total  int  // 1 + the index of this container's total in check's totals, or 0
}

// total counts the elements of all containers at one path of Budget.Totals.
type total struct {
	path     string
	limit, n int
}

// count adds one element or member to the total of f, if it has one.
func (f *frame) count(totals []total) bool {
	if f.total == 0 {
		return true
	}
	t := &totals[f.total-1]
	t.n++
	return t.n <= t.limit
}

// check is the first pass of Decode. It reads data in place (a
// jsontext.Decoder on a bytes.Buffer does not copy) and keeps no member
// names: DecodeStrict finds repeated names in the second pass.
func (b Budget) check(data []byte) error {
	// Checked first, so a wrong budget fails its very first test.
	for _, limit := range b.Items {
		if limit < 0 && limit != Raw {
			return errLimit
		}
	}
	if len(b.Totals) > maxTotals {
		return errLimit
	}
	var store [maxTotals]total
	totals := store[:0]
	for p, limit := range b.Totals {
		if items, ok := b.Items[p]; !ok || items == Raw || limit < 0 {
			return errLimit
		}
		totals = append(totals, total{path: p, limit: limit})
	}
	if len(data) > b.Size {
		return ErrTooLarge
	}
	dec := jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true))
	var (
		stack [MaxDepth]frame
		depth int
		path  = make([]byte, 0, MaxDepth*(MaxName+1))
		name  = make([]byte, 0, 6*MaxName)
	)
	for {
		k := dec.PeekKind()
		if depth > 0 {
			f := &stack[depth-1]
			if f.object && f.name && k == '"' {
				v, err := dec.ReadValue()
				if err != nil {
					return err
				}
				f.name = false
				f.n++
				switch {
				case f.limit >= 0 && f.n > f.limit, !f.count(totals):
					return ErrBudget
				case f.raw:
					continue
				case f.mapped:
					path = appendSegment(path[:f.base], anySegment)
					continue
				case len(v) > 6*MaxName+2:
					// An escape takes at most six bytes (\u00XX) for each
					// byte it stands for, so this name is longer than MaxName.
					return ErrName
				}
				name, err = jsontext.AppendUnquote(name[:0], v)
				if err != nil || len(name) > MaxName || string(name) == "*" || bytes.IndexByte(name, '/') >= 0 {
					return ErrName
				}
				path = appendSegment(path[:f.base], name)
				continue
			}
			// Count an element before reading it, so the first one too many
			// is refused unread.
			if !f.object && k != ']' {
				f.n++
				if (f.limit >= 0 && f.n > f.limit) || !f.count(totals) {
					return ErrBudget
				}
			}
		}
		switch k {
		case '{', '[':
			if depth == MaxDepth {
				return ErrDepth
			}
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			fr := frame{base: len(path), limit: -1, object: k == '{', name: k == '{'}
			limit, listed := b.Items[string(path)]
			switch {
			case depth > 0 && stack[depth-1].raw, listed && limit == Raw:
				fr.raw = true
			case listed:
				fr.limit, fr.mapped = limit, k == '{'
				for i := range totals {
					if totals[i].path == string(path) {
						fr.total = i + 1
					}
				}
			case k == '[':
				return ErrBudget
			default:
				fr.limit = MaxMembers
			}
			stack[depth] = fr
			depth++
			if k == '[' && !fr.raw {
				path = appendSegment(path, anySegment)
			}
			continue
		case '}', ']':
			if _, err := dec.ReadToken(); err != nil {
				return err
			}
			depth--
			path = path[:stack[depth].base]
		case 'n':
			return ErrNull
		case '0':
			v, err := dec.ReadValue()
			if err != nil {
				return err
			}
			if !safeNumber(v) {
				return ErrNumber
			}
		default: // strings, true, false, and whatever the decoder refuses
			if _, err := dec.ReadValue(); err != nil {
				return err
			}
		}
		if depth == 0 {
			return nil // DecodeStrict refuses anything after the value
		}
		if f := &stack[depth-1]; f.object {
			f.name = true
		}
	}
}

func appendSegment(path, segment []byte) []byte {
	if len(path) > 0 {
		path = append(path, '/')
	}
	return append(path, segment...)
}

// safeNumber reports whether the exact decimal value of the JSON number v
// has a magnitude of at most MaxSafeInteger. It works on the digits, not on
// a float64, so that it never rounds and never allocates, and JavaScript can
// decide it the same way from the text.
func safeNumber(v []byte) bool {
	i := 0
	if v[i] == '-' {
		i++
	}
	start := i
	for i < len(v) && v[i] >= '0' && v[i] <= '9' {
		i++
	}
	intDigits := v[start:i]
	var fracDigits []byte
	if i < len(v) && v[i] == '.' {
		i++
		start = i
		for i < len(v) && v[i] >= '0' && v[i] <= '9' {
			i++
		}
		fracDigits = v[start:i]
	}
	var exp int64
	if i < len(v) && (v[i] == 'e' || v[i] == 'E') {
		i++
		negative := v[i] == '-'
		if v[i] == '+' || v[i] == '-' {
			i++
		}
		// Far beyond the digit count of any input that fits in memory, so
		// capping the exponent there changes no answer.
		for ; i < len(v); i++ {
			if exp <= 1<<40 {
				exp = exp*10 + int64(v[i]-'0')
			}
		}
		if negative {
			exp = -exp
		}
	}
	// The digits of the integer and the fraction, read as one sequence.
	n := len(intDigits) + len(fracDigits)
	digit := func(k int) byte {
		switch {
		case k < len(intDigits):
			return intDigits[k]
		case k < n:
			return fracDigits[k-len(intDigits)]
		}
		return '0'
	}
	first := 0
	for first < n && digit(first) == '0' {
		first++
	}
	if first == n {
		return true // zero
	}
	// Digits before the decimal point, counted from the first non-zero one.
	whole := int64(len(intDigits)) + exp - int64(first)
	const limit = "9007199254740991" // MaxSafeInteger
	switch {
	case whole < int64(len(limit)):
		return true
	case whole > int64(len(limit)):
		return false
	}
	for j := 0; j < len(limit); j++ {
		if d := digit(first + j); d != limit[j] {
			return d < limit[j]
		}
	}
	for k := first + len(limit); k < n; k++ {
		if digit(k) != '0' {
			return false // a fraction on top of MaxSafeInteger
		}
	}
	return true
}
