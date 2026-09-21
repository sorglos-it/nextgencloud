package tunnel

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/session"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

// now lies within the client certificates of these tests.
const now = 1790000100

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testCode() PairCode {
	return PairCode{URL: "wss://api-1.test:7443/tunnel", Key: bytes.Repeat([]byte{0xA1}, 32),
		Secret: bytes.Repeat([]byte{0x5E}, SecretSize), Anchor: trust.Anchor{Seq: 3, Hash: bytes.Repeat([]byte{0xAB}, 32)}}
}

func TestPairCode(t *testing.T) {
	c := testCode()
	got, err := ParsePairCode(c.String())
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if got, err := ParsePairCode(" \n" + c.String() + "\n"); err != nil || !reflect.DeepEqual(got, c) {
		t.Errorf("pasted with white space: %+v, %v", got, err)
	}
	short := c
	short.Secret = short.Secret[:8]
	http := c
	http.URL = "https://api-1.test:7443/tunnel"
	long := PairPrefix + strings.Repeat("A", MaxPairCode)
	for _, s := range []string{"", PairPrefix, PairPrefix + "!!", strings.TrimPrefix(c.String(), PairPrefix), short.String(),
		http.String(), long, JoinCode(c).String(), c.String() + "\nA"} {
		if _, err := ParsePairCode(s); err != ErrPairCode {
			t.Errorf("%.40q: %v", s, err)
		}
	}
}

func TestPairRequest(t *testing.T) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	r := PairRequest{Name: "dienste", Noise: ctl, MAC: PairMAC(c.Secret, ctl, c.Key)}
	got, err := ParsePairRequest(mustJSON(t, r))
	if err != nil || got.Verify(c.Secret, c.Key) != nil {
		t.Fatalf("valid request: %v", err)
	}
	swapped := r
	swapped.Noise = bytes.Repeat([]byte{0xC2}, 32)
	if swapped.Verify(c.Secret, c.Key) != ErrPair {
		t.Error("other controller key accepted")
	}
	if r.Verify(c.Secret, bytes.Repeat([]byte{0xA2}, 32)) != ErrPair {
		t.Error("other API key accepted")
	}
	if r.Verify(bytes.Repeat([]byte{1}, SecretSize), c.Key) != ErrPair {
		t.Error("other secret accepted")
	}
	for name, mutate := range map[string]func(*PairRequest){
		"name with a space":     func(r *PairRequest) { r.Name = "a b" },
		"host name, upper case": func(r *PairRequest) { r.Name = "Dienste" },
		"host name with a dot":  func(r *PairRequest) { r.Name = "dienste.ngc.test" },
		"name of 33 characters": func(r *PairRequest) { r.Name = "d" + strings.Repeat("x", 32) },
		"noise of 31 bytes":     func(r *PairRequest) { r.Noise = r.Noise[:31] },
		"mac of 31 bytes":       func(r *PairRequest) { r.MAC = r.MAC[:31] },
		"no noise":              func(r *PairRequest) { r.Noise = nil },
		"no mac":                func(r *PairRequest) { r.MAC = nil },
	} {
		bad := r
		mutate(&bad)
		if _, err := ParsePairRequest(mustJSON(t, bad)); err != ErrPair {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParsePairRequest([]byte(`{"name":"dienste","name":"dienste"}`)); err != ErrPair {
		t.Errorf("duplicate member: %v", err)
	}
}

// pairChain returns version 1 (trusttest.Genesis) and version 2, in which
// the admin adds the user alice and the second API server api-2.
func pairChain(t testing.TB) []trust.Signed {
	admin := trusttest.NewSigner(1)
	g := admin.Sign(t, trusttest.Genesis(admin))
	l2 := trusttest.Next(t, g)
	l2.Roles = append(l2.Roles, trust.Role{Name: "user", Level: trust.LevelCritical})
	l2.Users = append(l2.Users, trust.User{ID: trusttest.ID(0x22), Name: "alice", Roles: []string{"user"},
		Keys: []proof.UserKey{trusttest.NewSigner(2).Key}})
	dh, sig := trusttest.NodeKeys(0xA2)
	api2 := trust.Node{ID: "api-2", Kind: trust.NodeAPI, Name: "API 2", Noise: dh.Public,
		Sign: enc.Bytes(sig.Public().(ed25519.PublicKey)), URL: "wss://api-2.test:7443/tunnel"}
	l2.Nodes = slices.Insert(l2.Nodes, 1, api2) // api-1, api-2, ctl-1
	return []trust.Signed{g, admin.Sign(t, l2)}
}

// newest opens chain from the anchor of its first version, as the shell does.
func newest(t testing.TB, chain []trust.Signed) trust.Version {
	t.Helper()
	v, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(chain[0])}, chain)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type checkCase struct {
	name  string
	code  code
	valid bool
}

// checkCases: codes as the shell may get them while its newest verified
// version is version 2 of pairChain. Each invalid one breaks one rule.
func checkCases(t testing.TB, chain []trust.Signed) []checkCase {
	v := newest(t, chain)
	node := func(id string) trust.Node {
		n, ok := v.List.Node(id)
		if !ok {
			t.Fatalf("no node %s", id)
		}
		return n
	}
	api1, api2, ctl1 := node("api-1"), node("api-2"), node("ctl-1")
	secret := bytes.Repeat([]byte{0x5E}, SecretSize)
	newestAnchor := trust.Anchor{Seq: v.List.Seq, Hash: v.Hash}
	otherHash := slices.Clone(v.Hash)
	otherHash[len(otherHash)-1] ^= 1
	c := func(url string, key []byte, a trust.Anchor) code {
		return code{URL: url, Key: key, Secret: secret, Anchor: a}
	}
	return []checkCase{
		{"anchor is the newest version, key and URL of api-1", c(api1.URL, api1.Noise, newestAnchor), true},
		{"anchor is the newest version, key and URL of api-2", c(api2.URL, api2.Noise, newestAnchor), true},
		{"anchor is an older version", c(api1.URL, api1.Noise, trust.Anchor{Seq: 1, Hash: trust.Hash(chain[0])}), false},
		{"anchor names a version the shell does not know", c(api1.URL, api1.Noise, trust.Anchor{Seq: 3, Hash: v.Hash}), false},
		{"anchor hash differs", c(api1.URL, api1.Noise, trust.Anchor{Seq: v.List.Seq, Hash: otherHash}), false},
		{"key of a controller", c(api1.URL, ctl1.Noise, newestAnchor), false},
		{"URL of no API node", c("wss://api-9.test:7443/tunnel", api1.Noise, newestAnchor), false},
		{"key of one API node, URL of another", c(api2.URL, api1.Noise, newestAnchor), false},
	}
}

func TestPairCodeCheck(t *testing.T) {
	chain := pairChain(t)
	v := newest(t, chain)
	for _, c := range checkCases(t, chain) {
		got, err := ParsePairCode(PairCode(c.code).String())
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		err = got.Check(v)
		if c.valid && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.valid && err != ErrCodeTrust {
			t.Errorf("%s: got %v, want %v", c.name, err, ErrCodeTrust)
		}
	}
	if err := PairCode(checkCases(t, chain)[0].code).Check(trust.Version{}); err != ErrCodeTrust {
		t.Errorf("no verified version: got %v, want %v", err, ErrCodeTrust)
	}
}

func testJoinCode(t testing.TB) JoinCode {
	return JoinCode(checkCases(t, pairChain(t))[0].code)
}

// joinKeys returns the Noise and signing keys of a new API server.
func joinKeys() (noise, sign []byte) {
	dh, sig := trusttest.NodeKeys(0xA3)
	return dh.Public, sig.Public().(ed25519.PublicKey)
}

func TestJoinCode(t *testing.T) {
	c := testJoinCode(t)
	got, err := ParseJoinCode(c.String())
	if err != nil || !reflect.DeepEqual(got, c) || !strings.HasPrefix(c.String(), JoinPrefix) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	short := c
	short.Key = short.Key[:31]
	for _, s := range []string{"", JoinPrefix, JoinPrefix + "!!", PairCode(c).String(), short.String(),
		JoinPrefix + strings.Repeat("A", MaxPairCode)} {
		if _, err := ParseJoinCode(s); err != ErrJoinCode {
			t.Errorf("%.40q: %v", s, err)
		}
	}

	chain := pairChain(t)
	v := newest(t, chain)
	for _, cc := range checkCases(t, chain) {
		err := JoinCode(cc.code).Check(v)
		if cc.valid && err != nil {
			t.Errorf("check %s: %v", cc.name, err)
		}
		if !cc.valid && err != ErrCodeTrust {
			t.Errorf("check %s: got %v, want %v", cc.name, err, ErrCodeTrust)
		}
	}

	noise, sig := joinKeys()
	r := JoinRequest{Node: "api-3", Noise: noise, Sign: sig, MAC: JoinMAC(c.Secret, noise, sig, c.Key)}
	parsed, err := ParseJoinRequest(mustJSON(t, r))
	if err != nil || parsed.Verify(c.Secret, c.Key) != nil {
		t.Fatalf("valid join request: %v", err)
	}
	other := bytes.Repeat([]byte{0x77}, 32)
	for name, bad := range map[string]JoinRequest{
		"other noise key":        {Node: r.Node, Noise: other, Sign: r.Sign, MAC: r.MAC},
		"other sign key":         {Node: r.Node, Noise: r.Noise, Sign: other, MAC: r.MAC},
		"noise and sign swapped": {Node: r.Node, Noise: r.Sign, Sign: r.Noise, MAC: r.MAC},
	} {
		if bad.Verify(c.Secret, c.Key) != ErrJoin {
			t.Errorf("%s accepted", name)
		}
	}
	if r.Verify(c.Secret, other) != ErrJoin {
		t.Error("other API key accepted")
	}
	if r.Verify(bytes.Repeat([]byte{1}, SecretSize), c.Key) != ErrJoin {
		t.Error("other secret accepted")
	}
	if bytes.Equal(JoinMAC(c.Secret, noise, sig, c.Key), PairMAC(c.Secret, noise, c.Key)) {
		t.Error("join MAC equals the pairing MAC")
	}
	for name, mutate := range map[string]func(*JoinRequest){
		"node in upper case": func(r *JoinRequest) { r.Node = "API-3" },
		"empty node":         func(r *JoinRequest) { r.Node = "" },
		"noise of 31 bytes":  func(r *JoinRequest) { r.Noise = r.Noise[:31] },
		"sign of 33 bytes":   func(r *JoinRequest) { r.Sign = append(slices.Clone(r.Sign), 1) },
		"mac of 31 bytes":    func(r *JoinRequest) { r.MAC = r.MAC[:31] },
	} {
		bad := r
		mutate(&bad)
		if _, err := ParseJoinRequest(mustJSON(t, bad)); err != ErrJoin {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseJoinRequest([]byte(`{"node":"api-3","extra":1}`)); err != ErrJoin {
		t.Errorf("unknown member: %v", err)
	}
}

func testHello() Hello {
	return Hello{Version: "1.0.0", Contract: Contract, Node: "ctl-1", TrustSeq: 3, Arch: "arm64", OS: "debian-13",
		Roles: []string{RoleDB, RoleServices}}
}

func TestHello(t *testing.T) {
	for name, mutate := range map[string]func(*Hello){
		"as sent":        func(h *Hello) {},
		"all roles":      func(h *Hello) { h.Roles = []string{RoleWeb, RoleAPI, RoleDB, RoleServices} },
		"no roles":       func(h *Hello) { h.Roles = nil },
		"API side":       func(h *Hello) { h.Node, h.Arch, h.OS, h.Roles = "api-1", "", "", nil },
		"OS with a plus": func(h *Hello) { h.OS = "ubuntu-24.04+1" },
	} {
		h := testHello()
		mutate(&h)
		if _, err := ParseHello(mustJSON(t, h)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, mutate := range map[string]func(*Hello){
		"unknown arch":     func(h *Hello) { h.Arch = "riscv64" },
		"unknown role":     func(h *Hello) { h.Roles = []string{"mail"} },
		"old part name":    func(h *Hello) { h.Roles = []string{"ngc-controller"} },
		"duplicate role":   func(h *Hello) { h.Roles = []string{RoleServices, RoleServices} },
		"contract 0":       func(h *Hello) { h.Contract = 0 },
		"no version":       func(h *Hello) { h.Version = "" },
		"node":             func(h *Hello) { h.Node = "Ctl 1" },
		"OS with a space":  func(h *Hello) { h.OS = "debian 13" },
		"OS in upper case": func(h *Hello) { h.OS = "Debian-13" },
		"OS too long":      func(h *Hello) { h.OS = strings.Repeat("d", 33) },
	} {
		bad := testHello()
		mutate(&bad)
		if _, err := ParseHello(mustJSON(t, bad)); err != ErrHello {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, s := range []string{
		`{"version":"1.0.0","contract":1,"trustSeq":3,"roles":["web","api","db","services","web"]}`,
		`{"version":"1.0.0","contract":1,"trustSeq":3,"parts":["services"]}`,
		`{"version":"1.0.0","contract":1,"trustSeq":3,"roles":null}`,
		`{"version":"1.0.0","contract":1,"trustSeq":9007199254740992}`,
	} {
		if _, err := ParseHello([]byte(s)); err != ErrHello {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestComponents(t *testing.T) {
	all := []string{RoleWeb, RoleAPI, RoleDB, RoleServices}
	for mask := 0; mask < 1<<len(all); mask++ {
		var roles []string
		for i, r := range all {
			if mask&(1<<i) != 0 {
				roles = append(roles, r)
			}
		}
		want := []string{trust.AutoController}
		if slices.Contains(roles, RoleAPI) {
			want = []string{trust.AutoAPI, trust.AutoController}
		}
		if slices.Contains(roles, RoleWeb) {
			want = append(want, trust.AutoWeb)
		}
		if got := Components(roles); !slices.Equal(got, want) {
			t.Errorf("%v: got %v, want %v", roles, got, want)
		}
	}
}

func testStatus() Status {
	return Status{
		Apps: []AppStatus{
			{App: "org.sorglos.terminal", Version: "1.0.0", State: StateRunning},
			{App: "org.sorglos.files", Version: "2.1.0", State: StateStopped},
			{App: "org.sorglos.mail", Version: "0.9.3", State: StateFailed},
			{App: "org.sorglos.photos", Version: "1.2.0", State: StateUnlicensed},
		},
		Components: map[string]string{trust.AutoController: "1.0.0", trust.AutoAPI: "1.0.0", trust.AutoWeb: "1.0.0"},
		DiskFree:   1 << 40,
	}
}

func TestParseStatus(t *testing.T) {
	s := testStatus()
	got, err := ParseStatus(mustJSON(t, s))
	if err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	// A controller without apps and without components sends no null.
	empty := mustJSON(t, Status{})
	if string(empty) != `{"diskFree":0}` {
		t.Errorf("empty status encodes as %s", empty)
	}
	if _, err := ParseStatus(empty); err != nil {
		t.Errorf("empty status: %v", err)
	}
	apps := func(n int) []AppStatus {
		a := make([]AppStatus, n)
		for i := range a {
			a[i] = AppStatus{App: "org.sorglos.app" + strconv.Itoa(i), Version: "1.0.0", State: StateRunning}
		}
		return a
	}
	if _, err := ParseStatus(mustJSON(t, Status{Apps: apps(MaxStatusApps)})); err != nil {
		t.Errorf("%d apps: %v", MaxStatusApps, err)
	}
	for name, mutate := range map[string]func(*Status){
		"core app":              func(s *Status) { s.Apps[0].App = "ngc.store" },
		"invalid app id":        func(s *Status) { s.Apps[0].App = "Terminal" },
		"duplicate app":         func(s *Status) { s.Apps[1].App = s.Apps[0].App },
		"bad version":           func(s *Status) { s.Apps[0].Version = "1.0" },
		"unknown state":         func(s *Status) { s.Apps[0].State = "paused" },
		"unknown component":     func(s *Status) { s.Components["ngc-mail"] = "1.0.0" },
		"old part name":         func(s *Status) { s.Components = map[string]string{"controller": "1.0.0"} },
		"bad component version": func(s *Status) { s.Components[trust.AutoWeb] = "v1" },
		"negative diskFree":     func(s *Status) { s.DiskFree = -1 },
		"513 apps":              func(s *Status) { s.Apps = apps(MaxStatusApps + 1) },
	} {
		bad := testStatus()
		mutate(&bad)
		if _, err := ParseStatus(mustJSON(t, bad)); err != ErrStatus {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, s := range []string{
		`{"apps":null,"diskFree":0}`,
		`{"parts":{},"diskFree":0}`,
		`{"diskFree":0,"diskFree":1}`,
		`{"diskFree":9007199254740992}`,
		`{"diskFree":0.5}`,
	} {
		if _, err := ParseStatus([]byte(s)); err != ErrStatus {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// browserOpen returns the browser's OPEN of the terminal, as the shell wrote
// it: the white space inside params stays in the bytes the prologue binds.
func browserOpen() []byte {
	return []byte(`{"app":"org.sorglos.terminal","service":"terminal","action":"shell","params":{ "cols": 80, "rows": 24 },` +
		`"window":262144,"e2e":"` + enc.B64.EncodeToString(bytes.Repeat([]byte{7}, wire.HandshakeSize)) + `"}`)
}

// userProof returns a proof of the right shape; ParseOpen checks only its
// structure, VerifyOpen checks it for real.
func userProof() *proof.Proof {
	return &proof.Proof{Kind: proof.KindPassword, KeyID: enc.B64.EncodeToString(bytes.Repeat([]byte{0x4B}, 16)),
		Sig: bytes.Repeat([]byte{1}, ed25519.SignatureSize)}
}

func testOpen() Open {
	return Open{Open: browserOpen(), Window: wire.DefaultWindow, Client: []byte(`{"inst":"x"}`), Proof: userProof()}
}

// stageOpen is the OPEN of a channel without a user: the API's own.
func stageOpen(t testing.TB) Open {
	inner := wire.Open{App: "ngc.store", Service: "controller", Action: "stage",
		Params: json.RawMessage(`{"target":"ngc-controller","version":"1.1.0"}`), Window: wire.DefaultWindow}
	return Open{Open: mustJSON(t, inner), Window: wire.DefaultWindow}
}

func TestParseOpen(t *testing.T) {
	o, inner, err := ParseOpen(mustJSON(t, testOpen()))
	if err != nil {
		t.Fatalf("valid: %v", err)
	}
	if !bytes.Equal(o.Open, browserOpen()) || string(inner.Params) != `{ "cols": 80, "rows": 24 }` || inner.App != "org.sorglos.terminal" {
		t.Errorf("got %s and params %s", o.Open, inner.Params)
	}
	if _, inner, err := ParseOpen(mustJSON(t, stageOpen(t))); err != nil || inner.Action != "stage" {
		t.Errorf("stage without user: %v", err)
	}
	for name, mutate := range map[string]func(*Open){
		"client of MaxCert bytes": func(o *Open) { o.Client = bytes.Repeat([]byte{'x'}, session.MaxCert) },
		"window MinWindow":        func(o *Open) { o.Window = wire.MinWindow },
		"window MaxWindow":        func(o *Open) { o.Window = wire.MaxWindow },
	} {
		ok := testOpen()
		mutate(&ok)
		if _, _, err := ParseOpen(mustJSON(t, ok)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, mutate := range map[string]func(*Open){
		"inner OPEN invalid":     func(o *Open) { o.Open = bytes.Replace(o.Open, []byte(`262144`), []byte(`10`), 1) },
		"inner OPEN not JSON":    func(o *Open) { o.Open = []byte("open") },
		"no inner OPEN":          func(o *Open) { o.Open = nil },
		"window below MinWindow": func(o *Open) { o.Window = wire.MinWindow - 1 },
		"window above MaxWindow": func(o *Open) { o.Window = wire.MaxWindow + 1 },
		"client without proof":   func(o *Open) { o.Proof = nil },
		"proof without client":   func(o *Open) { o.Client = nil },
		"client over MaxCert":    func(o *Open) { o.Client = bytes.Repeat([]byte{'x'}, session.MaxCert+1) },
		"malformed proof":        func(o *Open) { o.Proof.Kind = "totp" },
	} {
		bad := testOpen()
		mutate(&bad)
		if _, _, err := ParseOpen(mustJSON(t, bad)); err != wire.ErrOpen {
			t.Errorf("%s: %v", name, err)
		}
	}
	valid := string(mustJSON(t, testOpen()))
	for name, s := range map[string]string{
		"unknown field user": strings.Replace(valid, `{"open":`, `{"user":"`+trusttest.ID(0x21)+`","open":`, 1),
		"duplicate field":    strings.Replace(valid, `{"open":`, `{"window":262144,"open":`, 1),
		"proof null":         strings.Replace(valid, `,"proof":{`, `,"proof":null,"x":{`, 1),
		"open not base64url": strings.Replace(valid, `{"open":"`, `{"open":"=`, 1),
		"unknown field":      strings.Replace(valid, `{"open":`, `{"extra":1,"open":`, 1),
	} {
		if _, _, err := ParseOpen([]byte(s)); err != wire.ErrOpen {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// maximalPasskeyProof has the largest parts proof.Proof.Validate allows.
func maximalPasskeyProof() *proof.Proof {
	return &proof.Proof{Kind: proof.KindPasskey, KeyID: enc.B64.EncodeToString(bytes.Repeat([]byte{0x4B}, 16)),
		Sig: bytes.Repeat([]byte{1}, proof.MaxSig), AuthData: bytes.Repeat([]byte{2}, proof.MaxAuthData),
		ClientData: bytes.Repeat([]byte{3}, proof.MaxClientData)}
}

// TestMaxOpenPayload builds the largest OPEN the tunnel must carry: the
// largest browser OPEN (an app channel with a task and a maximal
// confirmation), a client certificate of MaxCert bytes and a maximal proof.
func TestMaxOpenPayload(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	env, err := sign.Sign(priv, sign.PurposeTask, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	inner := wire.Open{App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Window: wire.MaxWindow,
		E2E: bytes.Repeat([]byte{7}, wire.HandshakeSize), Task: &env, Confirm: maximalPasskeyProof()}
	// Start a little below the payload that fills the OPEN, then grow it.
	env.Payload = nil
	var largest []byte
	for n := (wire.MaxBrowserOpen-len(mustJSON(t, inner)))*3/4 - 8; ; n++ {
		env.Payload = make([]byte, n)
		b := mustJSON(t, inner)
		if len(b) > wire.MaxBrowserOpen {
			break
		}
		largest = b
	}
	// base64url grows by one or two characters per byte.
	if len(largest) < wire.MaxBrowserOpen-1 {
		t.Fatalf("largest browser OPEN has %d bytes", len(largest))
	}
	if _, err := wire.ParseOpen(largest); err != nil {
		t.Fatalf("browser OPEN of %d bytes: %v", len(largest), err)
	}
	outer := mustJSON(t, Open{Open: largest, Window: wire.MaxWindow, Client: bytes.Repeat([]byte{'c'}, session.MaxCert),
		Proof: maximalPasskeyProof()})
	t.Logf("browser OPEN %d bytes, tunnel OPEN %d bytes", len(largest), len(outer))
	if len(outer) > wire.MaxOpenPayload {
		t.Fatalf("tunnel OPEN of %d bytes exceeds MaxOpenPayload", len(outer))
	}
	if _, _, err := ParseOpen(outer); err != nil {
		t.Fatalf("largest tunnel OPEN: %v", err)
	}
}

// clientCert returns a client certificate of the user admin
// (trusttest.ID(0x21)) of l, as its payload.
func clientCert(t testing.TB, l *trust.List) []byte {
	return mustJSON(t, session.ClientCert{Installation: l.Installation, User: trusttest.ID(0x21),
		Nonce: bytes.Repeat([]byte{0x31}, session.NonceSize), Sig: bytes.Repeat([]byte{0x32}, 32), DH: bytes.Repeat([]byte{0x33}, 32),
		Issued: now - 100, Expires: now + 3600})
}

func TestVerifyOpen(t *testing.T) {
	admin := trusttest.NewSigner(1)
	l := trusttest.Genesis(admin)
	client := clientCert(t, l)
	withUser := func(signer trusttest.Signer) []byte {
		p := signer.Prove(sign.PurposeClientCert, client)
		return mustJSON(t, Open{Open: browserOpen(), Window: wire.DefaultWindow, Client: client, Proof: &p})
	}

	t.Run("user comes from the certificate", func(t *testing.T) {
		o, err := VerifyOpen(withUser(admin), l, now)
		if err != nil || o.Session == nil {
			t.Fatalf("got %+v, %v", o, err)
		}
		if o.Session.Cert().User != trusttest.ID(0x21) || !bytes.Equal(o.Session.Payload(), client) {
			t.Errorf("user %s", o.Session.Cert().User)
		}
		if string(o.Open.Params) != `{ "cols": 80, "rows": 24 }` || o.Window != wire.DefaultWindow {
			t.Errorf("params %s, window %d", o.Open.Params, o.Window)
		}
	})
	t.Run("certificate signed by a key not in the list", func(t *testing.T) {
		if _, err := VerifyOpen(withUser(trusttest.NewSigner(9)), l, now); err != session.ErrUser {
			t.Errorf("got %v, want %v", err, session.ErrUser)
		}
	})
	t.Run("certificate expired", func(t *testing.T) {
		if _, err := VerifyOpen(withUser(admin), l, now+7200); err != session.ErrExpired {
			t.Errorf("got %v, want %v", err, session.ErrExpired)
		}
	})
	t.Run("no client certificate", func(t *testing.T) {
		o, err := VerifyOpen(mustJSON(t, stageOpen(t)), l, now)
		if err != nil || o.Session != nil || o.Open.Action != "stage" {
			t.Errorf("got %+v, %v", o, err)
		}
	})
	t.Run("malformed OPEN", func(t *testing.T) {
		if _, err := VerifyOpen([]byte(`{}`), l, now); err != wire.ErrOpen {
			t.Errorf("got %v, want %v", err, wire.ErrOpen)
		}
	})
}

// allocated returns how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// hostile returns prefix, then as many empty objects as fit, then suffix:
// exactly size bytes.
func hostile(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.Grow(size)
	b.WriteString(prefix + "{}")
	for b.Len()+len(",{}")+len(suffix) <= size {
		b.WriteString(",{}")
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// members returns prefix, then as many members "xN":"1" as fit, then
// suffix: exactly size bytes.
func members(prefix, suffix string, size int) []byte {
	var b bytes.Buffer
	b.WriteString(prefix + `"x0":"1"`)
	for i := 1; b.Len()+len(`,"x`+strconv.Itoa(i)+`":"1"`)+len(suffix) <= size; i++ {
		b.WriteString(`,"x` + strconv.Itoa(i) + `":"1"`)
	}
	for b.Len()+len(suffix) < size {
		b.WriteByte(' ')
	}
	b.WriteString(suffix)
	return b.Bytes()
}

// TestParsersBoundAllocation feeds each parser a document of its full size
// limit with more elements than its contract allows, or an array where it
// allows none: refusing it must allocate next to nothing.
func TestParsersBoundAllocation(t *testing.T) {
	// The largest JSON a code of MaxPairCode characters can carry.
	codeJSON := (MaxPairCode - len(PairPrefix)) * 3 / 4
	l := trusttest.Genesis(trusttest.NewSigner(1))
	cases := []struct {
		name  string
		in    []byte
		parse func([]byte) error
		want  error
	}{
		{"ParseHello", hostile(`{"roles":[`, `]}`, MaxHello),
			func(b []byte) error { _, err := ParseHello(b); return err }, ErrHello},
		{"ParseStatus, apps", hostile(`{"apps":[`, `]}`, wire.MaxPayload),
			func(b []byte) error { _, err := ParseStatus(b); return err }, ErrStatus},
		{"ParseStatus, components", members(`{"components":{`, `}}`, wire.MaxPayload),
			func(b []byte) error { _, err := ParseStatus(b); return err }, ErrStatus},
		{"ParseOpen", hostile(`{"proof":{"sig":[`, `]}}`, wire.MaxOpenPayload),
			func(b []byte) error { _, _, err := ParseOpen(b); return err }, wire.ErrOpen},
		{"VerifyOpen", hostile(`{"open":[`, `]}`, wire.MaxOpenPayload),
			func(b []byte) error { _, err := VerifyOpen(b, l, now); return err }, wire.ErrOpen},
		{"ParsePairRequest", hostile(`{"noise":[`, `]}`, MaxPairRequest),
			func(b []byte) error { _, err := ParsePairRequest(b); return err }, ErrPair},
		{"ParseJoinRequest", hostile(`{"sign":[`, `]}`, MaxPairRequest),
			func(b []byte) error { _, err := ParseJoinRequest(b); return err }, ErrJoin},
		{"ParsePairCode", []byte(PairPrefix + enc.B64.EncodeToString(hostile(`{"anchor":{"hash":[`, `]}}`, codeJSON))),
			func(b []byte) error { _, err := ParsePairCode(string(b)); return err }, ErrPairCode},
		{"ParseJoinCode", []byte(JoinPrefix + enc.B64.EncodeToString(hostile(`{"url":[`, `]}`, codeJSON))),
			func(b []byte) error { _, err := ParseJoinCode(string(b)); return err }, ErrJoinCode},
	}
	for _, c := range cases {
		var err error
		n := allocated(func() { err = c.parse(c.in) })
		t.Logf("%s: %d bytes allocated", c.name, n)
		if n > 64<<10 {
			t.Errorf("%s: %d bytes allocated", c.name, n)
		}
		if err != c.want {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

type pairCheckCase struct {
	Name  string `json:"name"`
	Code  string `json:"code"`
	Valid bool   `json:"valid"`
}

type pairCheckVector struct {
	Chain []trust.Signed  `json:"chain"`
	Cases []pairCheckCase `json:"cases"`
}

type joinVector struct {
	Code   string    `json:"code"`
	Secret enc.Bytes `json:"secret"`
	APIKey enc.Bytes `json:"apiKey"`
	Noise  enc.Bytes `json:"noise"`
	Sign   enc.Bytes `json:"sign"`
	MAC    enc.Bytes `json:"mac"`
}

type vector struct {
	Code          string          `json:"code"`
	Secret        enc.Bytes       `json:"secret"`
	APIKey        enc.Bytes       `json:"apiKey"`
	ControllerKey enc.Bytes       `json:"controllerKey"`
	MAC           enc.Bytes       `json:"mac"`
	Fingerprint   string          `json:"fingerprint"`
	PairCheck     pairCheckVector `json:"pairCheck"`
	Join          joinVector      `json:"join"`
}

func TestVectors(t *testing.T) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	v := vector{Code: c.String(), Secret: c.Secret, APIKey: c.Key, ControllerKey: ctl, MAC: PairMAC(c.Secret, ctl, c.Key),
		Fingerprint: Fingerprint(ctl)}
	chain := pairChain(t)
	v.PairCheck.Chain = chain
	for _, cc := range checkCases(t, chain) {
		v.PairCheck.Cases = append(v.PairCheck.Cases, pairCheckCase{Name: cc.name, Code: PairCode(cc.code).String(), Valid: cc.valid})
	}
	jc := testJoinCode(t)
	noise, sig := joinKeys()
	v.Join = joinVector{Code: jc.String(), Secret: jc.Secret, APIKey: jc.Key, Noise: noise, Sign: sig,
		MAC: JoinMAC(jc.Secret, noise, sig, jc.Key)}
	checkVector(t, v)
	golden.Check(t, *update, "tunnel.json", v)
}

// checkVector holds the vector against the Go functions, as an
// implementation in another language holds it against its own.
func checkVector(t *testing.T, v vector) {
	c, err := ParsePairCode(v.Code)
	if err != nil || !bytes.Equal(c.Secret, v.Secret) || !bytes.Equal(c.Key, v.APIKey) {
		t.Errorf("code: %+v, %v", c, err)
	}
	r := PairRequest{Name: "dienste", Noise: v.ControllerKey, MAC: v.MAC}
	if r.Verify(v.Secret, v.APIKey) != nil || Fingerprint(v.ControllerKey) != v.Fingerprint {
		t.Error("pairing MAC or fingerprint")
	}
	latest, err := trust.Open(trust.Anchor{Seq: 1, Hash: trust.Hash(v.PairCheck.Chain[0])}, v.PairCheck.Chain)
	if err != nil {
		t.Fatal(err)
	}
	for _, cc := range v.PairCheck.Cases {
		pc, err := ParsePairCode(cc.Code)
		if err == nil {
			err = pc.Check(latest)
		}
		if (err == nil) != cc.Valid {
			t.Errorf("pair check %s: got %v, want valid %v", cc.Name, err, cc.Valid)
		}
	}
	jc, err := ParseJoinCode(v.Join.Code)
	if err != nil || !bytes.Equal(jc.Secret, v.Join.Secret) || !bytes.Equal(jc.Key, v.Join.APIKey) || jc.Check(latest) != nil {
		t.Errorf("join code: %+v, %v", jc, err)
	}
	jr := JoinRequest{Node: "api-3", Noise: v.Join.Noise, Sign: v.Join.Sign, MAC: v.Join.MAC}
	if jr.Verify(v.Join.Secret, v.Join.APIKey) != nil {
		t.Error("join MAC")
	}
}

func FuzzParsePairCode(f *testing.F) {
	f.Add(testCode().String())
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParsePairCode(s)
	})
}

// FuzzParseJoinCode checks that every join code ParseJoinCode accepts
// encodes to one it accepts again, with the same content.
func FuzzParseJoinCode(f *testing.F) {
	f.Add(testJoinCode(f).String())
	f.Add(testCode().String())
	f.Fuzz(func(t *testing.T, s string) {
		c, err := ParseJoinCode(s)
		if err != nil {
			return
		}
		if again, err := ParseJoinCode(c.String()); err != nil || !reflect.DeepEqual(again, c) {
			t.Fatalf("round trip of %q: %+v, %v", s, again, err)
		}
	})
}

func FuzzParsePairRequest(f *testing.F) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	f.Add(mustJSON(f, PairRequest{Name: "dienste", Noise: ctl, MAC: PairMAC(c.Secret, ctl, c.Key)}))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParsePairRequest(b)
	})
}

func FuzzParseJoinRequest(f *testing.F) {
	c := testJoinCode(f)
	noise, sig := joinKeys()
	f.Add(mustJSON(f, JoinRequest{Node: "api-3", Noise: noise, Sign: sig, MAC: JoinMAC(c.Secret, noise, sig, c.Key)}))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseJoinRequest(b)
	})
}

func FuzzParseHello(f *testing.F) {
	f.Add(mustJSON(f, testHello()))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseHello(b)
	})
}

// FuzzParseStatus checks that every status ParseStatus accepts encodes to one
// it accepts again: no null, nothing over its budget.
func FuzzParseStatus(f *testing.F) {
	f.Add(mustJSON(f, testStatus()))
	f.Add([]byte(`{"diskFree":0}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := ParseStatus(b)
		if err != nil {
			return
		}
		out := mustJSON(t, s)
		if len(out) > wire.MaxPayload {
			return // escaping may make it longer, which is not under test here
		}
		if _, err := ParseStatus(out); err != nil {
			t.Fatalf("re-parse of %s: %v", out, err)
		}
	})
}

// FuzzParseOpen checks that the browser's OPEN inside an accepted tunnel OPEN
// parses again to the same params.
func FuzzParseOpen(f *testing.F) {
	f.Add(mustJSON(f, testOpen()))
	f.Add(mustJSON(f, stageOpen(f)))
	f.Fuzz(func(t *testing.T, b []byte) {
		o, inner, err := ParseOpen(b)
		if err != nil {
			return
		}
		if again, err := wire.ParseOpen(o.Open); err != nil || !bytes.Equal(again.Params, inner.Params) {
			t.Fatalf("inner OPEN parses differently: %v", err)
		}
	})
}
