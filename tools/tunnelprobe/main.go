// Command tunnelprobe is a measuring fixture, not part of NGC: it writes the
// two configuration files of one API and one controller, makes version 1 of
// the trust list with both nodes in it and puts that version into the API own
// store with state.AddVersion. Everything it short-circuits is named in the
// report; the tunnel it prepares is the real one.
package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/apitls"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbopen"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/secrets"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/state"
	ctlstate "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	_ "github.com/sorglos-it/nextgencloud/apps/server-db/classes/register"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

const (
	etc      = "/etc/ngc"
	apiJSON  = etc + "/api.json"
	ctlJSON  = etc + "/controller.json"
	certFile = etc + "/tls/api.crt"
	keyFile  = etc + "/tls/api.key"
	dbFile   = "/var/lib/ngc/api.db"
	ctlDB    = "/var/lib/ngc/controller/state.db"
	anchorF  = etc + "/anchor.json"
	adminF   = etc + "/probe-admin.key"
	nodeID   = "api-1"
	ctlName  = "dienste"
	ctlNode  = "ctl-1"
	tunURL   = "wss://localhost:7443/tunnel"
	listen   = "127.0.0.1:7443"
)

func die(step string, err error) {
	fmt.Printf("PROBE_FAIL step=%s err=%v\n", step, err)
	os.Exit(1)
}

func b64key(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		die("rand", err)
	}
	return enc.B64.EncodeToString(b)
}

func main() {
	if len(os.Args) < 2 {
		die("args", fmt.Errorf("need a step"))
	}
	switch os.Args[1] {
	case "config":
		writeConfig()
	case "trust":
		writeTrust()
	case "ctlstate":
		readCtlState()
	case "trust2":
		writeTrust2()
	case "paircode":
		printPairCode()
	case "provisional":
		writeProvisional(os.Args[2])
	default:
		die("args", fmt.Errorf("unknown step"))
	}
}

// writeConfig makes both configuration files plus the TLS certificate, so the
// pin in controller.json is the pin of the key the API will serve.
func writeConfig() {
	_, pin, err := apitls.Ensure(certFile, keyFile, "localhost", time.Now())
	if err != nil {
		die("apitls.Ensure", err)
	}
	apiNoise := make([]byte, 32)
	if _, err := rand.Read(apiNoise); err != nil {
		die("rand", err)
	}
	apiNoisePriv, err := ecdh.X25519().NewPrivateKey(apiNoise)
	if err != nil {
		die("x25519 api", err)
	}
	ctlNoise := make([]byte, 32)
	if _, err := rand.Read(ctlNoise); err != nil {
		die("rand", err)
	}
	ctlNoisePriv, err := ecdh.X25519().NewPrivateKey(ctlNoise)
	if err != nil {
		die("x25519 ctl", err)
	}
	_, apiSignPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		die("ed25519 api", err)
	}
	relPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		die("ed25519 release", err)
	}
	storePub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		die("ed25519 store", err)
	}
	api := map[string]any{
		"node": nodeID, "url": tunURL, "listen": listen,
		"cert": certFile, "certKey": keyFile,
		"db":           map[string]any{"driver": "sqlite", "path": dbFile, "key": b64key(64)},
		"noiseKey":     enc.B64.EncodeToString(apiNoise),
		"signKey":      enc.B64.EncodeToString(apiSignPriv.Seed()),
		"secretKey":    b64key(32),
		"challengeKey": b64key(32),
		"releaseKey":   enc.B64.EncodeToString(relPub),
		"storeKey":     enc.B64.EncodeToString(storePub),
		"storeUrl":     "https://store.invalid/ngc",
	}
	writeJSON(apiJSON, api)
	ctl := map[string]any{
		"name": ctlName, "node": ctlNode,
		"noiseKey": enc.B64.EncodeToString(ctlNoise),
		"apis": []any{map[string]any{
			"url": tunURL,
			"key": enc.B64.EncodeToString(apiNoisePriv.PublicKey().Bytes()),
			"tls": enc.B64.EncodeToString(pin),
		}},
		"anchor":     map[string]any{"seq": 1, "hash": enc.B64.EncodeToString(make([]byte, 32))},
		"db":         ctlDB,
		"dbKey":      b64key(64),
		"releaseKey": enc.B64.EncodeToString(relPub),
		"roles":      []string{"services"},
	}
	writeJSON(ctlJSON, ctl)
	fmt.Printf("PROBE_CONFIG ok apiNoisePub=%s ctlNoisePub=%s pin=%s\n",
		enc.B64.EncodeToString(apiNoisePriv.PublicKey().Bytes()),
		enc.B64.EncodeToString(ctlNoisePriv.PublicKey().Bytes()),
		enc.B64.EncodeToString(pin))
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		die("marshal", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		die("write", err)
	}
}

// ctlFile is what this fixture reads back out of controller.json: only the
// public half of the key is needed, and config.LoadController would refuse the
// provisional file.
type ctlFile struct {
	NoiseKey string `json:"noiseKey"`
}

// writeTrust builds version 1 with the API node, the controller node and one
// admin with a password key, and hands it to state.AddVersion.
func writeTrust() {
	ctx := context.Background()
	cfg, err := config.LoadAPI(apiJSON)
	if err != nil {
		die("config.LoadAPI", err)
	}
	raw, err := os.ReadFile(ctlJSON)
	if err != nil {
		die("read controller.json", err)
	}
	var cf ctlFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		die("parse controller.json", err)
	}
	ctlSeed, err := enc.B64.DecodeString(cf.NoiseKey)
	if err != nil || len(ctlSeed) != 32 {
		die("controller noise key", fmt.Errorf("not 32 bytes"))
	}
	ctlPriv, err := ecdh.X25519().NewPrivateKey(ctlSeed)
	if err != nil {
		die("x25519 ctl", err)
	}
	db, err := dbopen.Open(ctx, cfg.DB, "tunnelprobe")
	if err != nil {
		die("dbopen.Open", err)
	}
	box, err := secrets.NewBox(cfg.SecretKey)
	if err != nil {
		die("secrets.NewBox", err)
	}
	_, pin, err := apitls.Ensure(cfg.Cert, cfg.CertKey, "localhost", time.Now())
	if err != nil {
		die("apitls.Ensure", err)
	}
	st := state.New(cfg, db, box, pin)
	inst, err := st.EnsureInstallation(ctx)
	if err != nil {
		die("EnsureInstallation", err)
	}
	apiNoisePriv, err := ecdh.X25519().NewPrivateKey(cfg.NoiseKey.Bytes())
	if err != nil {
		die("x25519 api", err)
	}
	apiSign := ed25519.NewKeyFromSeed(cfg.SignKey.Bytes())
	adminPub, adminPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		die("ed25519 admin", err)
	}
	salt := make([]byte, proof.SaltSize)
	if _, err := rand.Read(salt); err != nil {
		die("rand", err)
	}
	key := proof.UserKey{ID: sign.KeyID(adminPub), Kind: proof.KindPassword, Alg: proof.AlgEdDSA,
		Public: enc.Bytes(adminPub),
		KDF: &proof.KDF{Alg: proof.KDFArgon2id, Salt: enc.Bytes(salt),
			MemKiB: proof.MinMemKiB, Time: 3, Par: 1}}
	if err := key.Validate(); err != nil {
		die("userkey.Validate", err)
	}
	uid := make([]byte, 16)
	if _, err := rand.Read(uid); err != nil {
		die("rand", err)
	}
	list := trust.List{
		Seq: 1, Issued: time.Now().Unix(), Installation: inst,
		Users: []trust.User{{ID: enc.B64.EncodeToString(uid), Name: "admin",
			Roles: []string{trust.RoleAdmin}, Keys: []proof.UserKey{key}}},
		Roles: []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelOff}},
		Nodes: nodesSorted(
			trust.Node{ID: nodeID, Kind: trust.NodeAPI, Name: nodeID,
				Noise: enc.Bytes(apiNoisePriv.PublicKey().Bytes()),
				Sign:  enc.Bytes(apiSign.Public().(ed25519.PublicKey)),
				URL:   cfg.URL, TLS: enc.Bytes(pin)},
			trust.Node{ID: ctlNode, Kind: trust.NodeController, Name: ctlName,
				Noise: enc.Bytes(ctlPriv.PublicKey().Bytes())}),
	}
	if err := list.Validate(); err != nil {
		die("list.Validate", err)
	}
	payload, err := json.Marshal(list)
	if err != nil {
		die("marshal list", err)
	}
	sg := trust.Signed{Payload: enc.Bytes(payload), Proof: proof.Proof{
		Kind: proof.KindPassword, KeyID: key.ID,
		Sig: enc.Bytes(ed25519.Sign(adminPriv, sign.Message(sign.PurposeTrustList, payload))),
	}}
	v, err := st.AddVersion(ctx, sg)
	if err != nil {
		die("AddVersion", err)
	}
	if err := os.WriteFile(adminF, []byte(enc.B64.EncodeToString(adminPriv.Seed())), 0o600); err != nil {
		die("write admin key", err)
	}
	h := sha256.Sum256(payload)
	if err := patchAnchor(ctlJSON, v.List.Seq, h[:]); err != nil {
		die("patch anchor", err)
	}
	if err := os.WriteFile(anchorF, mustJSON(map[string]any{
		"seq": v.List.Seq, "hash": enc.B64.EncodeToString(h[:])}), 0o600); err != nil {
		die("write anchor", err)
	}
	fmt.Printf("PROBE_TRUST ok seq=%d hash=%s installation=%s nodes=%d\n",
		v.List.Seq, enc.B64.EncodeToString(h[:]), inst, len(v.List.Nodes))
}

// readCtlState opens the controller own encrypted SQLite and says which
// versions of the trust list it holds. Read only, and it is the evidence that
// a channel ran: nothing else writes these rows.
func readCtlState() {
	ctx := context.Background()
	cfg, err := config.LoadController(ctlJSON)
	if err != nil {
		die("config.LoadController", err)
	}
	st, err := ctlstate.Open(config.DB{Driver: config.DriverSQLite, Path: cfg.DB, Key: cfg.DBKey},
		"tunnelprobe", time.Now().Unix())
	if err != nil {
		die("ctlstate.Open", err)
	}
	defer st.Close()
	vs, err := st.Versions(ctx, 1)
	if err != nil {
		die("Versions", err)
	}
	out := ""
	for _, v := range vs {
		l, err := trust.Parse(v.Payload)
		if err != nil {
			die("trust.Parse", err)
		}
		h := sha256.Sum256(v.Payload)
		out += fmt.Sprintf(" seq=%d hash=%s nodes=%d", l.Seq, enc.B64.EncodeToString(h[:]), len(l.Nodes))
	}
	fmt.Printf("PROBE_CTLSTATE versions=%d%s\n", len(vs), out)
}

// writeTrust2 advances the trust list by one version, signed by the same
// admin. It is what makes the API push a chain to a controller that already
// holds version 1, so a second channel can be measured.
func writeTrust2() {
	ctx := context.Background()
	cfg, err := config.LoadAPI(apiJSON)
	if err != nil {
		die("config.LoadAPI", err)
	}
	seed, err := os.ReadFile(adminF)
	if err != nil {
		die("read admin key", err)
	}
	sd, err := enc.B64.DecodeString(string(bytes.TrimSpace(seed)))
	if err != nil || len(sd) != ed25519.SeedSize {
		die("admin key", fmt.Errorf("not a seed"))
	}
	adminPriv := ed25519.NewKeyFromSeed(sd)
	db, err := dbopen.Open(ctx, cfg.DB, "tunnelprobe")
	if err != nil {
		die("dbopen.Open", err)
	}
	box, err := secrets.NewBox(cfg.SecretKey)
	if err != nil {
		die("secrets.NewBox", err)
	}
	_, pin, err := apitls.Ensure(cfg.Cert, cfg.CertKey, "localhost", time.Now())
	if err != nil {
		die("apitls.Ensure", err)
	}
	st := state.New(cfg, db, box, pin)
	cur, err := st.Trust(ctx)
	if err != nil {
		die("Trust", err)
	}
	next := *cur.List
	next.Seq = cur.List.Seq + 1
	next.Prev = enc.Bytes(cur.Hash)
	next.Issued = time.Now().Unix()
	// The one change: a second role, which no node and no user uses. It is
	// enough to make a new version and touches nothing that is measured.
	// Roles stand sorted by name in a list (trust.List.Validate), and
	// "watcher" sorts after "admin".
	next.Roles = append(append([]trust.Role{}, next.Roles...),
		trust.Role{Name: "watcher", Level: trust.LevelOff})
	if err := next.Validate(); err != nil {
		die("list.Validate", err)
	}
	payload, err := json.Marshal(next)
	if err != nil {
		die("marshal", err)
	}
	sg := trust.Signed{Payload: enc.Bytes(payload), Proof: proof.Proof{
		Kind: proof.KindPassword, KeyID: sign.KeyID(adminPriv.Public().(ed25519.PublicKey)),
		Sig: enc.Bytes(ed25519.Sign(adminPriv, sign.Message(sign.PurposeTrustList, payload))),
	}}
	v, err := st.AddVersion(ctx, sg)
	if err != nil {
		die("AddVersion", err)
	}
	h := sha256.Sum256(payload)
	fmt.Println("PROBE_TRUST2 ok seq=" + fmt.Sprint(v.List.Seq) + " hash=" + enc.B64.EncodeToString(h[:]))
}

// printPairCode builds a pairing code with the real address, the real Noise
// key and the real TLS pin of this API, and a secret that was never
// registered. What it measures is the way, not the pairing: the API has to
// refuse it at the one-time value.
func printPairCode() {
	cfg, err := config.LoadAPI(apiJSON)
	if err != nil {
		die("config.LoadAPI", err)
	}
	_, pin, err := apitls.Ensure(cfg.Cert, cfg.CertKey, "localhost", time.Now())
	if err != nil {
		die("apitls.Ensure", err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(cfg.NoiseKey.Bytes())
	if err != nil {
		die("x25519", err)
	}
	secret := make([]byte, tunnel.SecretSize)
	if _, err := rand.Read(secret); err != nil {
		die("rand", err)
	}
	h := make([]byte, 32)
	if _, err := rand.Read(h); err != nil {
		die("rand", err)
	}
	c := tunnel.PairCode{URL: cfg.URL, Key: enc.Bytes(priv.PublicKey().Bytes()),
		TLS: enc.Bytes(pin), Secret: enc.Bytes(secret),
		Anchor: trust.Anchor{Seq: 1, Hash: enc.Bytes(h)}}
	fmt.Println(c.String())
}

// writeProvisional writes the configuration ngc-controller pair reads before
// it asks for the code, the same way the ulh script does it: the address in
// the reserved domain .invalid and 32 zero bytes where the pairing will put
// the API and the anchor.
func writeProvisional(path string) {
	zero := enc.B64.EncodeToString(make([]byte, 32))
	ctl := map[string]any{
		"name":     ctlName,
		"noiseKey": b64key(32),
		"apis": []any{map[string]any{
			"url": "wss://unpaired.invalid:7443/tunnel", "key": zero, "tls": zero,
		}},
		"anchor":     map[string]any{"seq": 1, "hash": zero},
		"db":         "/var/lib/ngc/controller/prov.db",
		"dbKey":      b64key(64),
		"releaseKey": b64key(32),
		"roles":      []string{"services"},
	}
	writeJSON(path, ctl)
	if _, err := config.LoadController(path); err != nil {
		die("provisional LoadController", err)
	}
	fmt.Println("PROBE_PROVISIONAL ok validate=passed")
}

func nodesSorted(ns ...trust.Node) []trust.Node {
	for i := 1; i < len(ns); i++ {
		for j := i; j > 0 && ns[j-1].ID > ns[j].ID; j-- {
			ns[j-1], ns[j] = ns[j], ns[j-1]
		}
	}
	return ns
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		die("marshal", err)
	}
	return append(b, '\n')
}

func patchAnchor(path string, seq uint64, hash []byte) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	m["anchor"] = map[string]any{"seq": seq, "hash": enc.B64.EncodeToString(hash)}
	writeJSON(path, m)
	return nil
}
