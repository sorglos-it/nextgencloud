// Package trusttest builds signed trust lists for tests in other packages.
// Its keys come from fixed seeds and must never be used outside tests.
package trusttest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"testing"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/noisep"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/sign"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
)

// ID returns a valid ID made of 16 bytes of fill.
func ID(fill byte) string { return enc.B64.EncodeToString(bytes.Repeat([]byte{fill}, 16)) }

// Signer holds a password key or a passkey. Keys come from a fixed seed
// instead of Argon2id or a real authenticator so that tests stay fast; the
// KDF fields on a password key are only metadata.
type Signer struct {
	Priv ed25519.PrivateKey
	Key  proof.UserKey
}

// NewSigner returns the password key with seed fill.
func NewSigner(fill byte) Signer {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
	pub := priv.Public().(ed25519.PublicKey)
	return Signer{Priv: priv, Key: proof.UserKey{
		ID: sign.KeyID(pub), Kind: proof.KindPassword, Alg: proof.AlgEdDSA, Public: enc.Bytes(pub),
		KDF: &proof.KDF{Alg: proof.KDFArgon2id, Salt: bytes.Repeat([]byte{fill}, proof.SaltSize),
			MemKiB: proof.DefaultMemKiB, Time: proof.DefaultTime, Par: proof.DefaultPar},
	}}
}

// Fixed relying party for NewPasskey's WebAuthn assertions. Genesis uses the
// same RP for its passkey admin, so the two agree.
const (
	rpID     = "ngc.example.org"
	rpOrigin = "https://ngc.example.org"
)

// NewPasskey returns an Ed25519 passkey (as a software WebAuthn authenticator
// would hold) with seed fill. Its Prove asserts against the fixed RP
// ngc.example.org / https://ngc.example.org, with the user always present and
// verified, so it stays fully deterministic like NewSigner's password key.
func NewPasskey(fill byte) Signer {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
	pub := priv.Public().(ed25519.PublicKey)
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		panic(err)
	}
	return Signer{Priv: priv, Key: proof.UserKey{
		ID: sign.KeyID(spki), Kind: proof.KindPasskey, Alg: proof.AlgEdDSA, Public: enc.Bytes(spki),
		CredID: bytes.Repeat([]byte{fill}, 8),
	}}
}

// Prove signs payload for purpose: a password key signs sign.Message
// directly, as the browser does; a passkey builds a WebAuthn assertion (user
// present and verified) against the fixed RP NewPasskey uses.
func (s Signer) Prove(purpose string, payload []byte) proof.Proof {
	if s.Key.Kind == proof.KindPasskey {
		challenge := enc.B64.EncodeToString(proof.Challenge(purpose, payload))
		cd := []byte(`{"type":"webauthn.get","challenge":"` + challenge + `","origin":"` + rpOrigin + `","crossOrigin":false}`)
		rpHash := sha256.Sum256([]byte(rpID))
		ad := append(append([]byte{}, rpHash[:]...), 0x05, 0, 0, 0, 0) // flags UP|UV, zero sign count
		cdHash := sha256.Sum256(cd)
		signed := append(append([]byte{}, ad...), cdHash[:]...)
		return proof.Proof{Kind: proof.KindPasskey, KeyID: s.Key.ID, Sig: ed25519.Sign(s.Priv, signed), AuthData: ad, ClientData: cd}
	}
	return proof.Proof{Kind: proof.KindPassword, KeyID: s.Key.ID, Sig: ed25519.Sign(s.Priv, sign.Message(purpose, payload))}
}

// Sign signs l as a trust list version.
func (s Signer) Sign(t testing.TB, l *trust.List) trust.Signed {
	t.Helper()
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return trust.Signed{Payload: b, Proof: s.Prove(sign.PurposeTrustList, b)}
}

// NodeKeys returns the Noise key and the signing key of a test node.
func NodeKeys(fill byte) (noise.DHKey, ed25519.PrivateKey) {
	dh, err := noisep.Keypair(bytes.NewReader(bytes.Repeat([]byte{fill}, 32)))
	if err != nil {
		panic(err)
	}
	return dh, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{fill}, ed25519.SeedSize))
}

// GenesisPasskeyFill is the seed Genesis uses for its second admin, who signs
// with a passkey instead of a password key. NewPasskey is deterministic, so
// a test that needs to act as that signer calls NewPasskey(GenesisPasskeyFill)
// and gets back the exact same key Genesis embedded in the list.
const GenesisPasskeyFill = 7

// Genesis returns version 1: user "admin" with admin's key, a second admin
// "admin2" who signs with a passkey (NewPasskey(GenesisPasskeyFill)) against
// the RP ngc.example.org, API "api-1" (NodeKeys(0xA1)) and controller "ctl-1"
// (NodeKeys(0xC1)), on which admins may run root tasks. Also carries the
// global store (ID trust.StoreGlobal, as the setup puts it into version 1)
// and two standing approvals (system, and controller updates) so a
// realistic list exercises those fields too.
func Genesis(admin Signer) *trust.List {
	apiDH, apiSign := NodeKeys(0xA1)
	ctlDH, _ := NodeKeys(0xC1)
	admin2 := NewPasskey(GenesisPasskeyFill)
	_, storeSign := NodeKeys(0xB1)
	_, autoSign := NodeKeys(0xD1)
	return &trust.List{
		Seq: 1, Issued: 1790000000, Installation: ID(0x11),
		RP: proof.RP{ID: rpID, Origins: []string{rpOrigin}},
		Users: []trust.User{
			{ID: ID(0x20), Name: "admin2", Roles: []string{trust.RoleAdmin}, Keys: []proof.UserKey{admin2.Key}},
			{ID: ID(0x21), Name: "admin", Roles: []string{trust.RoleAdmin}, Keys: []proof.UserKey{admin.Key}},
		},
		Roles: []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
		Nodes: []trust.Node{
			{ID: "api-1", Kind: trust.NodeAPI, Name: "API 1", Noise: apiDH.Public,
				Sign: enc.Bytes(apiSign.Public().(ed25519.PublicKey)), URL: "wss://api-1.test:7443/tunnel"},
			{ID: "ctl-1", Kind: trust.NodeController, Name: "Dienste", Noise: ctlDH.Public, RootRoles: []string{trust.RoleAdmin}},
		},
		Stores: []trust.Store{
			{ID: trust.StoreGlobal, Name: "NGC Store", URL: "https://store.ngc.example", Key: enc.Bytes(storeSign.Public().(ed25519.PublicKey))},
		},
		Auto: []trust.Auto{
			{Target: trust.AutoController, Key: enc.Bytes(autoSign.Public().(ed25519.PublicKey))},
			{Target: trust.AutoSystem},
		},
	}
}

// Next returns the list signed in prev with Seq, Prev and Issued advanced.
func Next(t testing.TB, prev trust.Signed) *trust.List {
	t.Helper()
	l, err := trust.Parse(prev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	l.Seq++
	l.Prev = trust.Hash(prev)
	l.Issued++
	return l
}
