package pairing

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// The values of the test installation: the API's Noise key and TLS identity,
// this controller's key pair, and the name it pairs under.
const (
	apiFill    = 0xA1
	apiTLSFill = 0xE1
	otherFill  = 0xE2 // the TLS key of another server: its pin is a foreign one
	ctlFill    = 0xC1
	apiHost    = "api-1.test"
	ctlName    = "ctl-1"
)

// anchor is the trust list version a code pins.
func anchor() trust.Anchor {
	return trust.Anchor{Seq: 3, Hash: bytes.Repeat([]byte{9}, sha256.Size)}
}

// codeFor builds the pairing code an API would hand out for srv.
func codeFor(srv *httptest.Server, key, pin, secret []byte) tunnel.PairCode {
	return tunnel.PairCode{
		URL:    "wss://" + strings.TrimPrefix(srv.URL, "https://") + "/tunnel",
		Key:    key,
		TLS:    pin,
		Secret: secret,
		Anchor: anchor(),
	}
}

// api starts a test API with the TLS identity of trusttest.APITLS(apiTLSFill,
// apiHost). It counts the pairing requests that reach it and answers each with
// status, so a test can show that nothing was sent at all.
func api(t *testing.T, status int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	cert, _ := trusttest.APITLS(apiTLSFill, apiHost)
	var seen atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		w.WriteHeader(status)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	// A refused handshake is what some of these tests want; its line belongs
	// in no test output.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &seen
}

// input is a pairing that works, for tests that change one value of it.
func input(code tunnel.PairCode) Input {
	ctl, _ := trusttest.NodeKeys(ctlFill)
	return Input{Code: code.String(), Name: ctlName, Roles: []string{tunnel.RoleServices}, Noise: ctl}
}

func TestDoPairs(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(apiFill)
	cert, pin := trusttest.APITLS(apiTLSFill, apiHost)
	secret := bytes.Repeat([]byte{7}, tunnel.SecretSize)

	var got tunnel.PairRequest
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tunnel.PathPair || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		// The query is half the request, and this stand used to ignore it: the
		// API spends the one-time value the query NAMES (controllers/pair.go,
		// "?secret=<base64url of the SHA-256 of the pairing secret>") and
		// without it answers E_PROTOCOL. A pairing with no query therefore
		// cannot come off against a real API, while a stand that reads only
		// path and body calls it good -- which is what happened until
		// 17.10.2026, measured on a real Debian.
		sum := sha256.Sum256(secret)
		if want := enc.B64.EncodeToString(sum[:]); r.URL.Query().Get("secret") != want {
			t.Errorf("secret of the query = %q, want the hash of the pairing secret", r.URL.Query().Get("secret"))
		}
		b, _ := io.ReadAll(io.LimitReader(r.Body, tunnel.MaxPairRequest+1))
		var err error
		if got, err = tunnel.ParsePairRequest(b); err != nil {
			t.Errorf("ParsePairRequest: %v", err)
		}
		if err := got.Verify(secret, apiNoise.Public); err != nil {
			t.Errorf("Verify: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()

	code := codeFor(srv, apiNoise.Public, pin, secret)
	in := input(code)
	res, err := Do(t.Context(), in)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	// Close waits for the request, so what the handler wrote is here to read.
	srv.Close()

	if got.Name != ctlName || !bytes.Equal(got.Noise, in.Noise.Public) {
		t.Errorf("request = %+v, want the name and key of this controller", got)
	}
	if len(res.APIs) != 1 {
		t.Fatalf("APIs = %v, want the one API of the code", res.APIs)
	}
	if a := res.APIs[0]; a.URL != code.URL || !bytes.Equal(a.Key, apiNoise.Public) || !bytes.Equal(a.TLS, pin) {
		t.Errorf("APIs[0] = %+v, want the address, key and pin of the code", a)
	}
	if res.Anchor.Seq != anchor().Seq || !bytes.Equal(res.Anchor.Hash, anchor().Hash) {
		t.Errorf("Anchor = %+v, want the anchor of the code", res.Anchor)
	}
}

// The pin of the code is the only thing that says which server may answer:
// with a foreign one the handshake ends before the request is written, so the
// server never learns that a controller tried to pair with it.
func TestDoRefusesWrongPin(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(apiFill)
	_, otherPin := trusttest.APITLS(otherFill, apiHost)
	srv, seen := api(t, http.StatusNoContent)

	code := codeFor(srv, apiNoise.Public, otherPin, bytes.Repeat([]byte{7}, tunnel.SecretSize))
	if _, err := Do(t.Context(), input(code)); err == nil {
		t.Fatal("Do = nil, want an error")
	}
	if n := seen.Load(); n != 0 {
		t.Errorf("the API saw %d requests, want none", n)
	}
}

// Only 204 pairs. Anything else may come from a server that never saw the
// request as a pairing request at all.
func TestDoRefusesAnyOtherAnswer(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(apiFill)
	_, pin := trusttest.APITLS(apiTLSFill, apiHost)
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusForbidden, http.StatusInternalServerError} {
		srv, seen := api(t, status)
		code := codeFor(srv, apiNoise.Public, pin, bytes.Repeat([]byte{7}, tunnel.SecretSize))
		if _, err := Do(t.Context(), input(code)); !errors.Is(err, tunnel.ErrPair) {
			t.Errorf("Do with status %d = %v, want tunnel.ErrPair", status, err)
		}
		if n := seen.Load(); n != 1 {
			t.Errorf("the API saw %d requests, want one", n)
		}
	}
}

// Everything Do is handed is checked before the one-time secret is spent: a
// pairing that could never be written to controller.json must not use up the
// code. Which error comes back says which check turned it away: the contract
// owns the code (tunnel.ErrPairCode), this package owns the rest (ErrInput).
func TestDoRefusesBadInput(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(apiFill)
	_, pin := trusttest.APITLS(apiTLSFill, apiHost)
	srv, seen := api(t, http.StatusNoContent)
	code := codeFor(srv, apiNoise.Public, pin, bytes.Repeat([]byte{7}, tunnel.SecretSize))

	for name, c := range map[string]struct {
		change func(*Input)
		want   error
	}{
		"no name":         {func(in *Input) { in.Name = "" }, ErrInput},
		"name not a name": {func(in *Input) { in.Name = "Controller 1" }, ErrInput},
		"no roles":        {func(in *Input) { in.Roles = nil }, ErrInput},
		"unknown role":    {func(in *Input) { in.Roles = []string{"root"} }, ErrInput},
		"role twice":      {func(in *Input) { in.Roles = []string{tunnel.RoleServices, tunnel.RoleServices} }, ErrInput},
		"too many roles": {func(in *Input) {
			in.Roles = []string{tunnel.RoleWeb, tunnel.RoleAPI, tunnel.RoleDB, tunnel.RoleServices, tunnel.RoleWeb}
		}, ErrInput},
		"no key":           {func(in *Input) { in.Noise.Public = nil }, ErrInput},
		"key too short":    {func(in *Input) { in.Noise.Public = bytes.Repeat([]byte{1}, 31) }, ErrInput},
		"no code":          {func(in *Input) { in.Code = "" }, tunnel.ErrPairCode},
		"a join code":      {func(in *Input) { in.Code = tunnel.JoinCode(code).String() }, tunnel.ErrPairCode},
		"code cut short":   {func(in *Input) { in.Code = code.String()[:len(code.String())-4] }, tunnel.ErrPairCode},
		"something to say": {func(in *Input) { in.Code = "ngc-pair:" + strings.Repeat("a", tunnel.MaxPairCode) }, tunnel.ErrPairCode},
	} {
		in := input(code)
		c.change(&in)
		if _, err := Do(t.Context(), in); !errors.Is(err, c.want) {
			t.Errorf("Do with %s = %v, want %v", name, err, c.want)
		}
	}
	if n := seen.Load(); n != 0 {
		t.Errorf("the API saw %d requests, want none", n)
	}
}

// The code is a one-time permission to pair a controller, and the controller
// runs as root: no error it returns may carry it, however the pairing failed.
func TestDoKeepsTheCodeQuiet(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(apiFill)
	_, pin := trusttest.APITLS(apiTLSFill, apiHost)
	srv, _ := api(t, http.StatusForbidden)
	live := codeFor(srv, apiNoise.Public, pin, bytes.Repeat([]byte{7}, tunnel.SecretSize))

	// A server that is gone: the error of net/http names what it was given.
	gone, _ := api(t, http.StatusNoContent)
	goneCode := codeFor(gone, apiNoise.Public, pin, bytes.Repeat([]byte{7}, tunnel.SecretSize))
	gone.Close()

	for _, code := range []tunnel.PairCode{live, goneCode} {
		in := input(code)
		_, err := Do(t.Context(), in)
		if err == nil {
			t.Fatal("Do = nil, want an error")
		}
		for _, quiet := range []string{in.Code, enc.B64.EncodeToString(code.Secret)} {
			if strings.Contains(err.Error(), quiet) {
				t.Errorf("Do error = %q, want the code kept out of it", err)
			}
		}
	}
}

// Show is what the admin compares with the shell; it names the two
// fingerprints and nothing else. It is handed the anchor alone, so the code
// cannot reach it: a code passed to fmt would print itself.
func TestShowNamesTheKeyAndTheAnchor(t *testing.T) {
	ctl, _ := trusttest.NodeKeys(ctlFill)
	got := Show(anchor(), ctl.Public)
	want := "Fingerabdruck dieses Controllers: " + tunnel.Fingerprint(ctl.Public) + "\n" +
		"Anker: Fassung 3, " + tunnel.Fingerprint(anchor().Hash) + "\n"
	if got != want {
		t.Errorf("Show = %q, want %q", got, want)
	}
	if strings.Contains(got, tunnel.PairPrefix) {
		t.Errorf("Show = %q, want no code in it", got)
	}
}

// The address Do posts to comes from the code and from nowhere else: no path,
// no user, no query of a made-up address may reach the request.
func FuzzAddress(f *testing.F) {
	f.Add("wss://api-1.test:7443/tunnel")
	f.Add("wss://127.0.0.1:8443/tunnel")
	f.Add("wss://[2001:db8::1]:7443/tunnel")
	f.Add("wss://evil@api-1.test:7443/tunnel")
	f.Add("wss://api-1.test:7443/tunnel/../pair")
	f.Add("wss://api-1.test:7443/tunnel?x=1")
	f.Add("wss://api-1.test/tunnel")
	f.Add("wss://:7443/tunnel")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		host, port, ok := address(s)
		if !ok {
			if trust.ValidTunnelURL(s) {
				t.Fatalf("address(%q) refused an address of the contract", s)
			}
			return
		}
		hp := net.JoinHostPort(host, port)
		if !trust.ValidHostPort(hp) {
			t.Fatalf("address(%q) = %q, %q, which is no host and port", s, host, port)
		}
		// It has to be the address of the code, not just some address: the
		// request goes where these two say, and nowhere else.
		if want := strings.TrimSuffix(strings.TrimPrefix(s, "wss://"), "/tunnel"); hp != want {
			t.Fatalf("address(%q) = %q, %q, want the address %q of the code", s, host, port, want)
		}
		raw := "https://" + hp + tunnel.PathPair
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("address(%q) built %q, which is no URL: %v", s, raw, err)
		}
		if u.Scheme != "https" || u.Host != hp || u.Path != tunnel.PathPair ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			t.Fatalf("address(%q) built %q, which asks %+v", s, raw, u)
		}
	})
}

// A pairing goes to the address of the code and nowhere else. An API that
// answers with a redirect gets no second request, least of all one in clear
// text: the answer carries the name and key of this server under the MAC of
// the one-time secret.
func TestDoFollowsNoRedirect(t *testing.T) {
	apiNoise, _ := trusttest.NodeKeys(apiFill)
	cert, pin := trusttest.APITLS(apiTLSFill, apiHost)
	var elsewhere atomic.Int64
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer plain.Close()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 307 keeps method and body, so a client that follows it hands the
		// pairing request on.
		http.Redirect(w, r, plain.URL+tunnel.PathPair, http.StatusTemporaryRedirect)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	defer srv.Close()

	code := codeFor(srv, apiNoise.Public, pin, bytes.Repeat([]byte{7}, tunnel.SecretSize))
	// The redirect is taken as the answer it is, and only 204 pairs, so it
	// comes back as a refused pairing and not as some transport error.
	if _, err := Do(t.Context(), input(code)); !errors.Is(err, tunnel.ErrPair) {
		t.Fatalf("Do = %v, want tunnel.ErrPair", err)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the other server saw %d requests, want none", n)
	}
}
