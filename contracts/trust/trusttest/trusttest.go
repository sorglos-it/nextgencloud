// Package trusttest builds signed trust lists for tests in other packages.
// Its keys come from fixed seeds and must never be used outside tests.
package trusttest

import (
	"bytes"
	"crypto/ed25519"
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

// Signer holds a password key. The key comes from a fixed seed instead of
// Argon2id so that tests stay fast; the KDF fields are only metadata.
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

// Prove signs payload for purpose, as the browser does with a password key.
func (s Signer) Prove(purpose string, payload []byte) proof.Proof {
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

// Genesis returns version 1: user "admin" with admin's key, API "api-1"
// (NodeKeys(0xA1)) and controller "ctl-1" (NodeKeys(0xC1)), on which admins may
// run root tasks.
func Genesis(admin Signer) *trust.List {
	apiDH, apiSign := NodeKeys(0xA1)
	ctlDH, _ := NodeKeys(0xC1)
	return &trust.List{
		Seq: 1, Issued: 1790000000, Installation: ID(0x11),
		Users: []trust.User{{ID: ID(0x21), Name: "admin", Roles: []string{trust.RoleAdmin}, Keys: []proof.UserKey{admin.Key}}},
		Roles: []trust.Role{{Name: trust.RoleAdmin, Level: trust.LevelCritical}},
		Nodes: []trust.Node{
			{ID: "api-1", Kind: trust.NodeAPI, Name: "API 1", Noise: apiDH.Public,
				Sign: enc.Bytes(apiSign.Public().(ed25519.PublicKey)), URL: "wss://api-1.test:7443/tunnel"},
			{ID: "ctl-1", Kind: trust.NodeController, Name: "Dienste", Noise: ctlDH.Public, RootRoles: []string{trust.RoleAdmin}},
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
