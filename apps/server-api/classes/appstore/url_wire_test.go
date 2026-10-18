package appstore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// TestWhatReallyGetsPastTheGap measures the second half of point 25. The first
// half is measured next door: downloadURL (fetch.go:383) cuts the address at
// the first "/" and never looks at path or query, so nine bytes the contract's
// own grammar refuses walk past it.
//
// Passing that grammar is not the same as reaching a server. Between the gap
// and the wire sits net/http, which has a grammar of its own. This case says,
// byte for byte, which of the nine actually leave the machine and in what
// shape -- so the next reader argues about a measured line and not about a
// list of characters.
//
// Measured on 18.10.2026, go1.27.2: of the nine, two reach the wire.
func TestWhatReallyGetsPastTheGap(t *testing.T) {
	const host = "https://store.ngc.test:8443"
	type outcome struct {
		name string
		path string
	}
	cases := []outcome{
		{"space", "/p q"},
		{"NUL", "/p\x00q"},
		{"DEL", "/p\x7fq"},
		{"quote", "/p\"q"},
		{"apostrophe", "/p'q"},
		{"less", "/p<q"},
		{"greater", "/p>q"},
		{"backslash", "/p\\q"},
		{"newline", "/p\nq"},
	}
	for _, c := range cases {
		u := host + c.path
		if !downloadURL(u) {
			t.Fatalf("%s: the fetcher refuses it after all -- the gap is closed, redo this measurement", c.name)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			t.Logf("%-11s net/http refuses the address: %v", c.name, err)
			continue
		}
		var buf bytes.Buffer
		if err := req.Write(&buf); err != nil {
			t.Logf("%-11s net/http refuses to write it: %v", c.name, err)
			continue
		}
		line, _, _ := strings.Cut(buf.String(), "\r\n")
		t.Logf("%-11s REACHES THE WIRE as %q", c.name, line)
	}
}

// TestTheSurvivingBytesAtARealServer takes the bytes that survive net/http and
// asks a real TLS server what arrived. Download is the caller under test, with
// nothing but a client in it: the address is all that decides here.
func TestTheSurvivingBytesAtARealServer(t *testing.T) {
	body := []byte("package")
	sum := sha256.Sum256(body)

	got := make(chan string, 4)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.RequestURI
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	st := &Stores{Client: srv.Client()}
	for _, c := range []struct{ name, path string }{
		{"space", "/p q"},
		{"backslash", "/p\\q"},
		{"plain", "/pq"},
	} {
		u := srv.URL + c.path
		if !downloadURL(u) {
			t.Fatalf("%s: %q does not even pass the fetcher's grammar", c.name, u)
		}
		var w bytes.Buffer
		err := st.Download(context.Background(), u, int64(len(body)), sum[:], &w)
		select {
		case arrived := <-got:
			t.Logf("%-10s ARRIVED at the server as %q (download err=%v)", c.name, arrived, err)
		default:
			t.Logf("%-10s never reached the server (download err=%v)", c.name, err)
		}
	}
	_ = bufio.NewReader
}

// TestWhatThePathMayDoAtTheStore asks the other half of the same gap: not
// which bytes survive, but what a path may SAY. store.ValidPath refuses dot
// segments for a relative address; an absolute one never meets it. What a
// store server then sees is measured here, not assumed.
func TestWhatThePathMayDoAtTheStore(t *testing.T) {
	body := []byte("package")
	sum := sha256.Sum256(body)

	got := make(chan string, 8)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.RequestURI
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	st := &Stores{Client: srv.Client()}
	for _, c := range []struct{ name, path string }{
		{"dot segments", "/apps/../../../etc/passwd"},
		{"encoded dots", "/apps/%2e%2e/%2e%2e/x"},
		{"query", "/x.ngc?token=secret&a=b"},
		{"double slash", "//evil.example/x.ngc"},
		{"semicolon", "/x.ngc;jsessionid=1"},
	} {
		u := srv.URL + c.path
		if !downloadURL(u) {
			t.Logf("%-13s the fetcher refuses it", c.name)
			continue
		}
		var w bytes.Buffer
		err := st.Download(context.Background(), u, int64(len(body)), sum[:], &w)
		select {
		case arrived := <-got:
			t.Logf("%-13s ARRIVED as %q (err=%v)", c.name, arrived, err)
		default:
			t.Logf("%-13s never reached the server (err=%v)", c.name, err)
		}
	}
}

// TestTheProposedFixWouldNotCloseTheTraversal is the finding that matters most
// about point 25, and it is about the proposed cure, not the disease.
//
// Point 25 says the fix is an exported store.ValidDownloadURL -- the same
// grammar as store.validHTTPSURL, written large. Measured on 18.10.2026: that
// grammar refuses nine BYTES and says nothing about dot segments, while
// store.ValidPath, which guards the relative branch, refuses them outright.
// So the day the proposed fix lands, "/apps/../../../etc/passwd" still walks
// through on the absolute branch and is still refused on the relative one --
// the two halves of one contract keep disagreeing, only more quietly.
//
// Whoever builds store.ValidDownloadURL takes the dot segments with it.
func TestTheProposedFixWouldNotCloseTheTraversal(t *testing.T) {
	const traversal = "apps/../../../etc/passwd"

	if store.ValidPath(traversal) {
		t.Fatalf("store.ValidPath accepts %q -- the relative branch has changed, redo this", traversal)
	}
	abs := "https://store.ngc.test:8443/" + traversal
	if !sharedGrammar(abs) {
		t.Fatalf("the contract grammar refuses %q after all -- it learnt about dot segments, delete this case", abs)
	}
	if !downloadURL(abs) {
		t.Fatalf("the fetcher refuses %q after all", abs)
	}
	t.Logf("the same path: refused as relative, accepted as absolute by BOTH the fetcher and the contract grammar")
}
