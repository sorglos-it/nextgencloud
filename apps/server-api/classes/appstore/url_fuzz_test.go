package appstore

import (
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// sharedGrammar is the contract's own grammar for a signed download link,
// store.validHTTPSURL (shared/store/catalog.go:208). It is written out here
// because it is not exported -- which is the whole of point 25. It serves as
// the oracle: what the contract says about an address.
//
// Kept byte for byte with the contract: "https://", a host of trust, then
// nothing, or "/" and a path and query of 0x21..0x7e without " ' < > \ and #;
// at most 512 bytes.
func sharedGrammar(u string) bool {
	if len(u) > 512 {
		return false
	}
	rest, ok := strings.CutPrefix(u, "https://")
	if !ok {
		return false
	}
	authority, pathQuery, hasPath := strings.Cut(rest, "/")
	if !trust.ValidHost(authority) && !trust.ValidHostPort(authority) {
		return false
	}
	if !hasPath {
		return true
	}
	for i := 0; i < len(pathQuery); i++ {
		c := pathQuery[i]
		if c < 0x21 || c > 0x7e || c == '"' || c == '\'' || c == '\\' || c == '<' || c == '>' || c == '#' {
			return false
		}
	}
	return true
}

// FuzzPackageURL feeds whatever a catalog may carry as a package address into
// PackageURL and pins what the fetcher does guarantee today: an accepted
// address is https, carries no "#", is at most maxDownloadURL bytes and has a
// host trust accepts, and a relative address only ever grows into its base.
// The path is deliberately not asserted here -- nobody checks it, and that is
// measured in TestPackageURLAcceptsWhatTheContractRefuses below.
//
// The base is fixed to a valid store address, so every failure is about u.
func FuzzPackageURL(f *testing.F) {
	const base = "https://store.ngc.test:8443"

	// Something valid, and something a hair off.
	f.Add("apps/mail/1.2.3.ngc")
	f.Add("apps/mail/1.2.3.ngc\x00")
	f.Add("https://store.ngc.test:8443/apps/mail/1.2.3.ngc?token=x")
	f.Add("https://127.0.0.1:1/x.ngc")

	f.Fuzz(func(t *testing.T, u string) {
		full, ok := PackageURL(base, u)
		if !ok {
			if full != "" {
				t.Fatalf("refused %q but returned %q", u, full)
			}
			return
		}
		if !strings.HasPrefix(full, "https://") {
			t.Fatalf("accepted %q -> %q: not https", u, full)
		}
		if strings.Contains(full, "#") {
			t.Fatalf("accepted %q -> %q: carries a fragment", u, full)
		}
		if len(full) > maxDownloadURL {
			t.Fatalf("accepted %q -> %q: %d bytes", u, full, len(full))
		}
		rest, _ := strings.CutPrefix(full, "https://")
		authority, _, _ := strings.Cut(rest, "/")
		if !trust.ValidHost(authority) && !trust.ValidHostPort(authority) {
			t.Fatalf("accepted %q -> %q: host %q", u, full, authority)
		}
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(full, base+"/") {
			t.Fatalf("relative %q left the base: %q", u, full)
		}
	})
}

// TestPackageURLAcceptsWhatTheContractRefuses nails down the gap of point 25
// and point 45 as a measured fact instead of a sentence: the fetcher's own
// grammar (downloadURL, fetch.go:383) cuts the address at the first "/" and
// never looks at the rest, while the contract's grammar walks every byte of
// path and query.
//
// The cause is one line of downloadURL: the authority is cut off the address
// and the remainder is dropped. Fixing it belongs in the frozen shared/ -- an
// exported store.ValidDownloadURL in place of store.validHTTPSURL
// (shared/store/catalog.go:208). Until that exists this test is green; the day
// it exists this test goes red and should be deleted together with the gap.
func TestPackageURLAcceptsWhatTheContractRefuses(t *testing.T) {
	const base = "https://store.ngc.test:8443"
	// Every one of these carries a byte store.validHTTPSURL refuses in a path.
	for _, u := range []string{
		"https://a/ ",
		"https://a/\x00",
		"https://a/\x7f",
		"https://a/x\"y",
		"https://a/x'y",
		"https://a/x<y",
		"https://a/x>y",
		"https://a/x\\y",
		"https://a/x\ny",
	} {
		full, ok := PackageURL(base, u)
		if !ok {
			t.Fatalf("%q: the fetcher refuses it after all -- the gap is closed, delete this test", u)
		}
		if sharedGrammar(full) {
			t.Fatalf("%q: the contract accepts it after all, so it is no gap", u)
		}
	}
	// The counter-check that this is about the absolute branch only: the
	// relative branch does go through store.ValidPath, so the same bytes fail.
	for _, u := range []string{"a/ ", "a/\x00", "a/x<y"} {
		if store.ValidPath(u) {
			t.Fatalf("store.ValidPath accepts %q", u)
		}
		if _, ok := PackageURL(base, u); ok {
			t.Fatalf("relative %q got through", u)
		}
	}
}
