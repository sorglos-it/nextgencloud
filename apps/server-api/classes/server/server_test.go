package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/apitls"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/controllers"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/join"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
	"github.com/sorglos-it/nextgencloud/shared/session"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// TestHandlerServesEveryPath walks every path of the contract -- the shell's
// and the tunnel's -- and an unknown one.
//
// What it asserts is deliberately narrow: that the path is WIRED, not what its
// handler decides. "Wired" is measurable as "the answer is not E_NOTFOUND",
// and the opposite, an unknown path, has to be exactly that. Without the
// second half the case would pass against a router that answers everything.
func TestHandlerServesEveryPath(t *testing.T) {
	srv, _, _ := newServer(t)
	h := srv.Handler()
	paths := []struct {
		name, method, path string
	}{
		{"setup get", http.MethodGet, session.PathSetup},
		{"setup post", http.MethodPost, session.PathSetup},
		{"login start", http.MethodPost, session.PathLoginStart},
		{"login finish", http.MethodPost, session.PathLoginFinish},
		{"logout", http.MethodPost, session.PathLogout},
		{"trust", http.MethodGet, session.PathTrust},
		{"socket", http.MethodGet, session.PathSocket},
		{"tunnel", http.MethodGet, tunnel.PathTunnel},
		{"pair", http.MethodPost, tunnel.PathPair},
		{"join", http.MethodPost, tunnel.PathJoin},
		{"keys", http.MethodGet, join.PathKeys},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(p.method, p.path, nil))
			// "Wired" is: the answer is NOT the router's own unknown-path
			// answer. That one is the error of the contract with E_NOTFOUND
			// as its body; a handler that answers a bare 404 of its own
			// (controllers.Tunnel for a key nobody knows, join.Keys before
			// there is a trust list) has a body no error parses from, and
			// that is exactly the difference being measured here.
			if isRouterNotFound(rec) {
				t.Fatalf("%s %s: the router answered E_NOTFOUND, so nothing is wired to it",
					p.method, p.path)
			}
		})
	}
	t.Run("unknown path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil))
		if got := codeOf(t, rec); got != wire.CodeNotFound {
			t.Fatalf("code: got %q, want %q", got, wire.CodeNotFound)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", rec.Code)
		}
	})
	t.Run("a tunnel path with the wrong method", func(t *testing.T) {
		// POST on the keys path and not GET on the pair path: a pair handler
		// reached by GET answers E_PROTOCOL of its own accord, over the
		// missing body, so that pair would measure the later wall and pass
		// with no method check at all. join.Keys reached by POST answers a
		// bare 404 instead, which is not an error of the contract.
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, join.PathKeys, nil))
		if got := codeOf(t, rec); got != wire.CodeProtocol {
			t.Fatalf("code: got %q, want %q", got, wire.CodeProtocol)
		}
	})
	t.Run("a path that would have to be cleaned", func(t *testing.T) {
		// Not through httptest.NewRequest: it cleans the path itself, and the
		// case would then measure its own helper instead of the router.
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.URL.Path = "//api/v1/setup"
		r.RequestURI = "//api/v1/setup"
		h.ServeHTTP(rec, r)
		if got := codeOf(t, rec); got != wire.CodeNotFound {
			t.Fatalf("code: got %q, want %q -- a redirect of the mux got out", got, wire.CodeNotFound)
		}
	})
	t.Run("a tunnel path that would have to be cleaned", func(t *testing.T) {
		// The tunnel paths are matched exactly, so //tunnel/pair is NOT the
		// pair path. Nothing behind this router cleans a path either, so if
		// this one did, a POST to it would reach the pairing handler.
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.URL.Path = "//tunnel/pair"
		r.RequestURI = "//tunnel/pair"
		h.ServeHTTP(rec, r)
		if got := codeOf(t, rec); got != wire.CodeNotFound {
			t.Fatalf("code: got %q, want %q -- a cleaned path reached a tunnel handler",
				got, wire.CodeNotFound)
		}
	})
}

// TestBuildRefusesABrokenConfiguration: every broken half stops the start with
// a German message, and nothing of it runs anyway.
func TestBuildRefusesABrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		want string
		// broken changes the configuration; it may also take the database
		// away. It returns nothing: what it breaks is what the case is about.
		broken func(t *testing.T, cfg *config.API)
	}{
		{"no database", "Datenbank", func(t *testing.T, cfg *config.API) {
			dbopen.Register(nil)
		}},
		{"secret key unusable", "geheime Schlüssel", func(t *testing.T, cfg *config.API) {
			cfg.SecretKey = config.NewSecret(bytes.Repeat([]byte{1}, 16))
		}},
		{"challenge key unusable", "challengeKey", func(t *testing.T, cfg *config.API) {
			cfg.ChallengeKey = config.NewSecret(nil)
		}},
		{"url is no tunnel address", "url", func(t *testing.T, cfg *config.API) {
			cfg.URL = "https://api-1.test/tunnel"
		}},
		{"certificate unreadable", "Zertifikat", func(t *testing.T, cfg *config.API) {
			// A directory where the certificate belongs: apitls cannot read
			// it and cannot write over it either, so this is the half that
			// stops a start rather than being mended by a new certificate.
			if err := os.Remove(cfg.Cert); err != nil {
				t.Fatalf("remove: %v", err)
			}
			if err := os.Mkdir(cfg.Cert, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := memstore.New()
			withStore(t, db)
			withDirs(t)
			cfg := newConfig(t)
			mustEnsure(t, cfg) // the good pair first, so only c.broken breaks it
			c.broken(t, cfg)
			srv, err := Build(ctx, cfg, "9.9.9")
			if err == nil {
				t.Fatalf("Build went through although %s", c.name)
			}
			if srv != nil {
				t.Fatalf("Build gave a server back next to its error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("message %q does not name %q", err, c.want)
			}
			if !isGerman(err.Error()) {
				t.Fatalf("message %q is not the German one an admin reads", err)
			}
		})
	}
}

// isRouterNotFound reports whether rec is the router's own answer for a path
// it has no route for.
func isRouterNotFound(rec *httptest.ResponseRecorder) bool {
	if rec.Code != http.StatusNotFound {
		return false
	}
	var e struct {
		Code wire.Code `json:"code"`
	}
	return json.Unmarshal(rec.Body.Bytes(), &e) == nil && e.Code == wire.CodeNotFound
}

// isGerman is the crude half of the assertion above: the messages of Build are
// the ones an admin reads on his console, and every one of them is a German
// sentence. Measuring that properly is not possible here, so this measures
// what can be measured -- that it is not one of Go's English error strings --
// and the case names the part of the sentence it expects beside it.
func isGerman(s string) bool {
	for _, w := range []string{"ist", "lässt", "kann", "sich"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// mustEnsure makes the certificate and the key of cfg, as a first start does.
func mustEnsure(t *testing.T, cfg *config.API) {
	t.Helper()
	host, err := apitls.Host(cfg.URL)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	if _, _, err := apitls.Ensure(cfg.Cert, cfg.CertKey, host, time.Now()); err != nil {
		t.Fatalf("certificate: %v", err)
	}
}

// TestRunStopsCleanly: an aborted context ends the listener and the background
// rounds, and Run comes back.
func TestRunStopsCleanly(t *testing.T) {
	srv, _, _ := newServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// The rounds beat fast, so a stop has to get past a running round too.
	oldMinute, oldHour := Minute, Hour
	Minute, Hour = 2*time.Millisecond, 2*time.Millisecond
	defer func() { Minute, Hour = oldMinute, oldHour }()

	rctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- srv.serve(rctx, ln) }()
	// Wait until it really answers: a Run that had not started yet would stop
	// trivially, and the case would measure nothing.
	waitUntilServing(t, ln.Addr().String())
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("serve did not come back within five seconds")
	}
	// And the listener really is shut: a connection after the stop fails.
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		_ = c.Close()
		t.Fatalf("the listener still takes connections after the stop")
	}
}

// waitUntilServing dials addr until the server answers.
func waitUntilServing(t *testing.T, addr string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the server never answered on %s", addr)
}

// TestTheSpoolIsEmptiedAtEveryStart is the crash case of Punkt 30: a run that
// is killed between writing a package and removing it leaves the file behind,
// because appstore removes it with defer and defer does not run on SIGKILL.
func TestTheSpoolIsEmptiedAtEveryStart(t *testing.T) {
	db := memstore.New()
	withStore(t, db)
	_, spool := withDirs(t)
	if _, err := Build(ctx, newConfig(t), "9.9.9"); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// What a killed run leaves: a package and a directory beside it.
	left := filepath.Join(spool, "ngc-package-123")
	if err := os.WriteFile(left, []byte("half a package"), 0o600); err != nil {
		t.Fatalf("leftover: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(spool, "sub", "deeper"), 0o700); err != nil {
		t.Fatalf("leftover directory: %v", err)
	}
	if got := len(filesIn(t, spool)); got != 2 {
		t.Fatalf("the case did not set up what it measures: %d entries, want 2", got)
	}
	srv, err := Build(ctx, newConfig(t), "9.9.9")
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if got := filesIn(t, spool); len(got) != 0 {
		t.Fatalf("the spool still holds %v after a start", got)
	}
	// And the one thing that must NOT happen beside it: the spool is the one
	// appstore writes to, not some other directory.
	if srv.st.Spool != spool {
		t.Fatalf("appstore spools to %q, the server sweeps %q", srv.st.Spool, spool)
	}
}

// TestBuildRefusesASpoolItCannotUse is the sixth refusal of Build, and the one
// that had no case: a mutant that threw the error of sweepSpool away killed
// nothing (m-sweep-ignored of the closing review, measured LIVING). The other
// five refusals each have a case, this one did not -- and a start that goes on
// without a spool has nowhere to put an upload later.
//
// The spool is put under a path that is a FILE, so MkdirAll cannot make it.
// That is the same fault a read-only or full disk makes, and the only one a
// case can make on any system.
func TestBuildRefusesASpoolItCannotUse(t *testing.T) {
	db := memstore.New()
	withStore(t, db)
	_, spool := withDirs(t)
	// Control first: with a usable spool the same Build goes through, so the
	// refusal below is the spool and not a wall in front of it.
	if _, err := Build(ctx, newConfig(t), "9.9.9"); err != nil {
		t.Fatalf("control: %v", err)
	}
	blocker := spool + "-blocked"
	if err := os.WriteFile(blocker, []byte("a file where a directory belongs"), 0o600); err != nil {
		t.Fatalf("blocker: %v", err)
	}
	SpoolDir = filepath.Join(blocker, "spool")
	// The case checks its own setup: if the path were usable after all, the
	// refusal below would never come and the case would have measured nothing.
	if err := os.MkdirAll(SpoolDir, 0o700); err == nil {
		t.Fatalf("the blocked path %q can be made after all", SpoolDir)
	}
	srv, err := Build(ctx, newConfig(t), "9.9.9")
	if err == nil {
		t.Fatalf("Build took a spool it cannot use: %v", srv)
	}
	if srv != nil {
		t.Fatalf("Build answered an error AND a server: %v", srv)
	}
}

// TestOnlyOneRoundAtATime nails the decision in Server.round: a round that
// outlives its tick does not get a second one on top of itself.
func TestOnlyOneRoundAtATime(t *testing.T) {
	srv := &Server{}
	inside := make(chan struct{})
	release := make(chan struct{})
	go srv.once(func() { close(inside); <-release })
	<-inside
	ran := false
	srv.once(func() { ran = true })
	if ran {
		t.Fatalf("a second round started while the first was still running")
	}
	close(release)
	// And when the first is done, the next tick is served again -- without
	// this half the case would also pass against a round that never runs.
	// srv.round is free again only after the goroutine has left once, so the
	// case waits for that instead of racing it.
	for i := 0; !ran && i < 500; i++ {
		srv.once(func() { ran = true })
		if !ran {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !ran {
		t.Fatalf("no round ran after the first was done")
	}
}

// TestTheStoreIsWired: task 22 left appstore.Stores.Send nil and said every
// step would answer E_INTERNAL until the server wired it. This is the case
// that says it is wired now.
func TestTheStoreIsWired(t *testing.T) {
	srv, _, _ := newServer(t)
	if srv.st.Send == nil {
		t.Fatalf("appstore.Stores.Send is still nil, so activate answers E_INTERNAL")
	}
	// A node without a tunnel is "away" and not an error of its own.
	if _, ok := srv.st.Send.Node("ctl-1"); ok {
		t.Fatalf("a node with no tunnel was reported as reachable")
	}
	body, c := srv.st.Send.Call(ctx, "ctl-1", core.ActDeactivate, nil, nil, nil, nil)
	if c != wire.CodeUnreachable || body != nil {
		t.Fatalf("call to a node without a tunnel: got %q/%v, want E_UNREACHABLE/nil", c, body)
	}
	_, c = srv.st.Send.Transfer(ctx, "ctl-1", core.ActActivate, nil, core.Transfer{}, nil, nil, nil, nil)
	if c != wire.CodeUnreachable {
		t.Fatalf("transfer to a node without a tunnel: got %q, want E_UNREACHABLE", c)
	}
}

// TestALicenceForAnAbsentControllerIsNoCrash: pushLicense is called from the
// hello of a controller, where nothing may fail the hello.
func TestALicenceForAnAbsentControllerIsNoCrash(t *testing.T) {
	srv, _, _ := newServer(t)
	if err := srv.pushLicense("ctl-away", "demo.app", sign.Envelope{}); err == nil {
		t.Fatalf("a licence for a controller that is away went through")
	}
}

// TestBuildClosesTheSetupWithATrustList is the cross-check that Build really
// runs EnsureCode and not merely builds something that could: with a trust
// list there is no setup any more, so no code file is written.
func TestBuildClosesTheSetupWithATrustList(t *testing.T) {
	db := memstore.New()
	withStore(t, db)
	code, _ := withDirs(t)
	signedIn(t, db)
	if _, err := Build(ctx, newConfig(t), "9.9.9"); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := os.Stat(code); !os.IsNotExist(err) {
		t.Fatalf("a setup code was written although the installation has a trust list")
	}
	// The other side: without a trust list the code IS written. Without this
	// half the case would pass against a Build that never writes one.
	db2 := memstore.New()
	withStore(t, db2)
	if _, err := Build(ctx, newConfig(t), "9.9.9"); err != nil {
		t.Fatalf("Build without a trust list: %v", err)
	}
	if _, err := os.Stat(code); err != nil {
		t.Fatalf("no setup code although the installation has no trust list: %v", err)
	}
}

// TestMigrateSaysSoWithoutAMigrator: ngc-api migrate is this program's
// subcommand (Verträge V1.2, Punkt 28), and a build without the database part
// says that plainly instead of doing nothing quietly.
func TestMigrateSaysSoWithoutAMigrator(t *testing.T) {
	dbopen.RegisterMigrator(nil)
	if err := dbopen.Migrate(ctx, config.DB{}, "ngc-api/n1", nil); err != dbopen.ErrNoMigrator {
		t.Fatalf("Migrate: got %v, want ErrNoMigrator", err)
	}
	ran := false
	dbopen.RegisterMigrator(func(context.Context, config.DB, string, io.Writer) error { ran = true; return nil })
	t.Cleanup(func() { dbopen.RegisterMigrator(nil) })
	if err := dbopen.Migrate(ctx, config.DB{}, "ngc-api/n1", nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !ran {
		t.Fatalf("the registered migrator did not run")
	}
}

// --- the one hop between Apache and this API -------------------------------

// tlsAnswer hands cfg to a listener of its own, dials it with a client whose
// highest version is max, and returns the state of the connection or the error
// of the handshake. Nothing is shared between cases: the listener closes when
// the case ends.
//
// InsecureSkipVerify because the certificate this API makes is signed by
// itself and trust on this hop hangs on its fingerprint and not on a chain
// (tunnel.PinnedTLS) -- what is measured here is the version and the suite,
// not who the peer is.
func tlsAnswer(t *testing.T, cfg *tls.Config, max uint16) (tls.ConnectionState, error) {
	return tlsAnswerOn(t, cfg, max, nil)
}

// tlsAnswerOn is tlsAnswer with the key share groups of the client in the
// case's hand. CurvePreferences IS read under TLS 1.3, on both sides, which is
// what makes the group list of TLSConfig measurable where its suite list is
// not.
func tlsAnswerOn(t *testing.T, cfg *tls.Config, max uint16, curves []tls.CurveID) (tls.ConnectionState, error) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = c.(*tls.Conn).HandshakeContext(ctx)
				_ = c.Close()
			}()
		}
	}()
	c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		ServerName: "api-1", InsecureSkipVerify: true,
		MinVersion: tls.VersionTLS12, MaxVersion: max, CurvePreferences: curves,
	})
	if err != nil {
		return tls.ConnectionState{}, err
	}
	defer c.Close()
	return c.ConnectionState(), nil
}

// TestTLSConfigTakesTLS13Only holds the half of TLSConfig that really is a
// bound: nothing below TLS 1.3 gets a connection. The 1.3 client first, as the
// intermediate check -- a case that only measured the refusal would stay green
// against a listener that refuses everybody.
func TestTLSConfigTakesTLS13Only(t *testing.T) {
	srv, _, _ := newServer(t)
	st, err := tlsAnswer(t, srv.TLSConfig(), tls.VersionTLS13)
	if err != nil {
		t.Fatalf("a TLS 1.3 client got no connection: %v", err)
	}
	if st.Version != tls.VersionTLS13 {
		t.Fatalf("version %x", st.Version)
	}
	if _, err := tlsAnswer(t, srv.TLSConfig(), tls.VersionTLS12); err == nil {
		t.Fatal("a client whose highest version is TLS 1.2 got a connection")
	}
	// The group list is the other half of TLSConfig that really bounds: P-384
	// is not on it, and a client that offers nothing else gets no connection.
	// The intermediate check is the P-256 round -- the list names two groups,
	// and a case that only measured the refusal would stay green against a
	// listener that refuses every group.
	if _, err := tlsAnswerOn(t, srv.TLSConfig(), tls.VersionTLS13,
		[]tls.CurveID{tls.CurveP256}); err != nil {
		t.Fatalf("a client offering only P-256 got no connection: %v", err)
	}
	if _, err := tlsAnswerOn(t, srv.TLSConfig(), tls.VersionTLS13,
		[]tls.CurveID{tls.CurveP384}); err == nil {
		t.Fatal("a client offering only P-384 got a connection")
	}
}

// TestTLSConfigNamesNoCipherSuite holds what the comment of TLSConfig says
// about the suite, and it is on purpose a case about the FIELD and not about
// the wire: the design asks for AES-256-GCM alone on this hop (Entwurf 4.5),
// and the promise this case keeps is that no field here pretends to be that
// bound. Go ignores CipherSuites under TLS 1.3, so a listener that named the
// one suite would hand out the other two all the same -- measured on
// 12.10.2026 with openssl s_client 3.5.7, one suite per run: AES-256-GCM,
// CHACHA20-POLY1305 and AES-128-GCM all connected. The bound lives on the
// client, which is Apache (SSLProxyCipherSuite, shared/ulh.md).
//
// The second half is the wire after all, and it is what makes this more than a
// string comparison: the same configuration with a list that holds NO TLS 1.3
// suite at all still gets a connection. If Go read the field, that handshake
// could not come about.
func TestTLSConfigNamesNoCipherSuite(t *testing.T) {
	srv, _, _ := newServer(t)
	if cs := srv.TLSConfig().CipherSuites; cs != nil {
		t.Fatalf("TLSConfig names cipher suites again (%v); Go ignores them under TLS 1.3, "+
			"so the field reads like a bound and is none", cs)
	}
	bent := srv.TLSConfig()
	bent.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA}
	st, err := tlsAnswer(t, bent, tls.VersionTLS13)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if st.CipherSuite != tls.TLS_AES_128_GCM_SHA256 && st.CipherSuite != tls.TLS_AES_256_GCM_SHA384 &&
		st.CipherSuite != tls.TLS_CHACHA20_POLY1305_SHA256 {
		t.Fatalf("suite %s is none of the three of TLS 1.3", tls.CipherSuiteName(st.CipherSuite))
	}
	t.Logf("TLS 1.3 on %s, although CipherSuites named none of its suites",
		tls.CipherSuiteName(st.CipherSuite))
}

// --- the notices of a controller -------------------------------------------

// TestANoticeOfAControllerLeavesALineWhereNoShellTakesIt goes in the way a
// controller's notice comes in -- through the hook Build set -- and measures
// what comes out of it, not that the hook is there.
//
// Where it cannot reach: the delivery to an OPEN shell is measured in package
// browser (TestRegistryHoldsASessionWhileItRuns: NotifyAll counts 1 and the
// frame arrives on the link), because a session is made by the socket server
// out of a login and this package has no shell to log in. What joins the two
// halves is the registry, and that is held here by IDENTITY: a notice sent to
// a registry no shell is ever added to would reach nobody, and "both are not
// nil" would pass that.
func TestANoticeOfAControllerLeavesALineWhereNoShellTakesIt(t *testing.T) {
	srv, _, _ := newServer(t)
	if srv.ctl == nil || srv.ctl.OnNotice == nil {
		t.Fatal("nothing takes the notices of a controller: ctl.OnNotice is nil, " +
			"so every wire.CtlNotify falls on the floor")
	}
	if srv.sessions == nil || srv.sessions != srv.core.Sessions {
		t.Fatalf("the notices go to registry %p and the shells are in %p -- it has to be ONE",
			srv.sessions, srv.core.Sessions)
	}
	// Nobody is logged in, so this notice can reach no shell. That is the case
	// that used to disappear without a trace.
	var said bytes.Buffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&said, nil)))
	t.Cleanup(func() { slog.SetDefault(before) })
	srv.ctl.OnNotice(ctx, &controllers.Tunnel{Node: "ctl-1"},
		core.Notice{Level: core.NoticeError, Text: "Mein Dienst ist gestorben", App: "ngc.test"})
	line := said.String()
	if !strings.Contains(line, "reached no shell") || !strings.Contains(line, "ctl-1") ||
		!strings.Contains(line, core.NoticeError) {
		t.Fatalf("a notice nobody could take left no line behind: %q", line)
	}
	// And what must NOT be in it: the text comes from another machine, and the
	// line is about the notice having come, not about what it said.
	if strings.Contains(line, "gestorben") {
		t.Fatalf("the text of the notice is in the operator log: %q", line)
	}
}
