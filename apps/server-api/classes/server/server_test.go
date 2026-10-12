package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/apitls"
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
	if err := dbopen.Migrate(ctx, config.DB{}, nil); err != dbopen.ErrNoMigrator {
		t.Fatalf("Migrate: got %v, want ErrNoMigrator", err)
	}
	ran := false
	dbopen.RegisterMigrator(func(context.Context, config.DB, io.Writer) error { ran = true; return nil })
	t.Cleanup(func() { dbopen.RegisterMigrator(nil) })
	if err := dbopen.Migrate(ctx, config.DB{}, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !ran {
		t.Fatalf("the registered migrator did not run")
	}
}
