package tests

import (
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// FuzzSessionParseLoginStart feeds the first message of a login. The browser
// writes it, so nothing in it is trusted. An accepted message must carry a
// login name the contract accepts -- a name that gets through here reaches the
// database lookup.
func FuzzSessionParseLoginStart(f *testing.F) {
	f.Add([]byte(`{"name":"admin"}`))
	f.Add([]byte(`{"name":""}`))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := session.ParseLoginStart(b)
		if err != nil {
			return
		}
		if s.Name == "" {
			t.Fatalf("accepted an empty name from %d bytes", len(b))
		}
	})
}

// FuzzSessionParseTrustHint feeds the hint a peer sends about the trust list
// it holds. An accepted hint must name a sequence of at least 1: zero would
// mean a list before the first one, and the contract says 1 is the first.
func FuzzSessionParseTrustHint(f *testing.F) {
	f.Add([]byte(`{"seq":1}`))
	f.Add([]byte(`{"seq":0}`))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, b []byte) {
		h, err := session.ParseTrustHint(b)
		if err != nil {
			return
		}
		if h.Seq < 1 {
			t.Fatalf("accepted seq %d", h.Seq)
		}
	})
}

// FuzzSessionParseFrom feeds the query parameter "from" of the trust path: a
// decimal number without sign and without a leading zero. The oracle is
// strconv: whatever ParseFrom accepts must be the same number, and it must be
// inside the range the contract names, so no second reader of the same string
// can arrive at a different number.
func FuzzSessionParseFrom(f *testing.F) {
	f.Add("1")
	f.Add("01")
	f.Add("")
	f.Add("9007199254740991")

	f.Fuzz(func(t *testing.T, s string) {
		n, err := session.ParseFrom(s)
		if err != nil {
			if n != 0 {
				t.Fatalf("refused %q but returned %d", s, n)
			}
			return
		}
		if n > enc.MaxSafeInteger {
			t.Fatalf("accepted %q as %d", s, n)
		}
		again, cerr := strconv.ParseUint(s, 10, 64)
		if cerr != nil || again != n {
			t.Fatalf("%q: ParseFrom %d, strconv %d (%v)", s, n, again, cerr)
		}
		if s != strconv.FormatUint(n, 10) {
			t.Fatalf("%q reads as %d, which writes as %q", s, n, strconv.FormatUint(n, 10))
		}
	})
}

// FuzzStorePredicates feeds one string into the three predicates of the store
// contract that judge text coming off a signed catalog: the relative path of a
// package, its version and its target. What is pinned is that nothing they
// accept can leave the place it is pasted into -- a path with "..", a
// separator, a query, a fragment, a space or a control character would do
// exactly that, and every one of the three ends up in an address or a file
// name.
func FuzzStorePredicates(f *testing.F) {
	f.Add("apps/mail/1.2.3.ngc")
	f.Add("apps/../mail")
	f.Add("1.2.3")
	f.Add("1.2.3.4")
	f.Add("linux-amd64")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		if store.ValidPath(s) {
			if len(s) == 0 || len(s) > 200 {
				t.Fatalf("path %q: %d bytes", s, len(s))
			}
			for i := 0; i < len(s); i++ {
				c := s[i]
				if c < 0x21 || c > 0x7e || c == '"' || c == '\'' || c == '\\' ||
					c == '<' || c == '>' || c == '#' || c == '?' || c == ':' {
					t.Fatalf("path %q holds byte %#x at %d", s, c, i)
				}
			}
			if s[0] == '/' || hasDotDot(s) {
				t.Fatalf("path %q climbs or is absolute", s)
			}
		}
		if store.ValidVersion(s) {
			for i := 0; i < len(s); i++ {
				if c := s[i]; (c < '0' || c > '9') && c != '.' {
					t.Fatalf("version %q holds %#x", s, c)
				}
			}
		}
		if store.ValidTarget(s) {
			// Either one of the four fixed names, or an app id that is not a
			// core app -- and nothing else, however it is spelled.
			fixed := s == trust.AutoWeb || s == trust.AutoAPI || s == trust.AutoController || s == trust.AutoSystem
			if !fixed {
				if !wire.ValidAppID(s) || wire.CoreApp(s) {
					t.Fatalf("target %q is neither a fixed name nor an outside app id", s)
				}
				for i := 0; i < len(s); i++ {
					c := s[i]
					if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
						t.Fatalf("target %q holds %#x", s, c)
					}
				}
			}
		}
	})
}

// hasDotDot reports whether p has a ".." of its own as a path element.
func hasDotDot(p string) bool {
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			if p[start:i] == ".." {
				return true
			}
			start = i + 1
		}
	}
	return false
}
