package tunnel

import (
	"bytes"
	"encoding/json"
	"flag"
	"reflect"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/enc"
	"github.com/sorglos-it/nextgencloud/contracts/internal/golden"
	"github.com/sorglos-it/nextgencloud/contracts/proof"
	"github.com/sorglos-it/nextgencloud/contracts/trust"
	"github.com/sorglos-it/nextgencloud/contracts/trust/trusttest"
	"github.com/sorglos-it/nextgencloud/contracts/wire"
)

var update = flag.Bool("update", false, "rewrite contracts/testdata")

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
	short := c
	short.Secret = short.Secret[:8]
	long := PairPrefix + strings.Repeat("A", MaxPairCode)
	for _, s := range []string{"", PairPrefix, PairPrefix + "!!", strings.TrimPrefix(c.String(), PairPrefix), short.String(), long} {
		if _, err := ParsePairCode(s); err != ErrPairCode {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestPairRequest(t *testing.T) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	r := PairRequest{Name: "dienste", Noise: ctl, MAC: PairMAC(c.Secret, ctl, c.Key)}
	b, _ := json.Marshal(r)
	got, err := ParsePairRequest(b)
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
	badName := PairRequest{Name: "a b", Noise: ctl, MAC: bytes.Repeat([]byte{1}, 32)}
	b, _ = json.Marshal(badName)
	if _, err := ParsePairRequest(b); err != ErrPair {
		t.Errorf("bad name: %v", err)
	}
	badNoise := PairRequest{Name: "dienste", Noise: nil, MAC: bytes.Repeat([]byte{1}, 32)}
	b, _ = json.Marshal(badNoise)
	if _, err := ParsePairRequest(b); err != ErrPair {
		t.Errorf("bad noise length: %v", err)
	}
	badMAC := PairRequest{Name: "dienste", Noise: ctl, MAC: nil}
	b, _ = json.Marshal(badMAC)
	if _, err := ParsePairRequest(b); err != ErrPair {
		t.Errorf("bad mac length: %v", err)
	}
}

func TestHello(t *testing.T) {
	h := Hello{Version: "1.0.0", Contract: Contract, Node: "ctl-1", TrustSeq: 3, Arch: "arm64", OS: "debian-13", Parts: []string{PartServices}}
	b, _ := json.Marshal(h)
	if _, err := ParseHello(b); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Hello){
		func(h *Hello) { h.Arch = "riscv64" },
		func(h *Hello) { h.Parts = []string{"mail"} },
		func(h *Hello) { h.Contract = 0 },
		func(h *Hello) { h.Version = "" },
		func(h *Hello) { h.Node = "Ctl 1" },
	} {
		bad := h
		mutate(&bad)
		b, _ := json.Marshal(bad)
		if _, err := ParseHello(b); err != ErrHello {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func testOpen() Open {
	return Open{
		Open: wire.Open{App: "org.sorglos.terminal", Service: "terminal", Action: "shell", Window: wire.DefaultWindow,
			E2E: bytes.Repeat([]byte{7}, wire.HandshakeSize)},
		User: trusttest.ID(0x21), Client: []byte(`{"inst":"x"}`),
		Proof: &proof.Proof{Kind: proof.KindPassword, KeyID: "k", Sig: []byte{1}},
	}
}

func TestParseOpen(t *testing.T) {
	b, _ := json.Marshal(testOpen())
	if _, err := ParseOpen(b); err != nil {
		t.Fatalf("valid: %v", err)
	}
	auto := Open{Open: wire.Open{App: "ngc.store", Service: "controller", Action: "update", Window: wire.DefaultWindow}}
	b, _ = json.Marshal(auto)
	if _, err := ParseOpen(b); err != nil {
		t.Errorf("automatic update without user: %v", err)
	}
	noProof := testOpen()
	noProof.Proof = nil
	b, _ = json.Marshal(noProof)
	if _, err := ParseOpen(b); err == nil {
		t.Error("client certificate without proof accepted")
	}
	userOnly := testOpen()
	userOnly.Client, userOnly.Proof = nil, nil
	b, _ = json.Marshal(userOnly)
	if _, err := ParseOpen(b); err == nil {
		t.Error("user without client certificate and proof accepted")
	}
	proofOnly := testOpen()
	proofOnly.User, proofOnly.Client = "", nil
	b, _ = json.Marshal(proofOnly)
	if _, err := ParseOpen(b); err == nil {
		t.Error("proof without user and client certificate accepted")
	}
	if _, err := ParseOpen([]byte(`{"app":"org.sorglos.terminal","service":"terminal","action":"shell","window":262144,"extra":1}`)); err == nil {
		t.Error("unknown field accepted")
	}
}

type vector struct {
	Code          string    `json:"code"`
	Secret        enc.Bytes `json:"secret"`
	APIKey        enc.Bytes `json:"apiKey"`
	ControllerKey enc.Bytes `json:"controllerKey"`
	MAC           enc.Bytes `json:"mac"`
	Fingerprint   string    `json:"fingerprint"`
}

func TestVectors(t *testing.T) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	golden.Check(t, *update, "tunnel.json", vector{Code: c.String(), Secret: c.Secret, APIKey: c.Key,
		ControllerKey: ctl, MAC: PairMAC(c.Secret, ctl, c.Key), Fingerprint: Fingerprint(ctl)})
}

func FuzzParsePairCode(f *testing.F) {
	f.Add(testCode().String())
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParsePairCode(s)
	})
}

func FuzzParsePairRequest(f *testing.F) {
	c := testCode()
	ctl := bytes.Repeat([]byte{0xC1}, 32)
	b, _ := json.Marshal(PairRequest{Name: "dienste", Noise: ctl, MAC: PairMAC(c.Secret, ctl, c.Key)})
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParsePairRequest(b)
	})
}

func FuzzParseHello(f *testing.F) {
	b, _ := json.Marshal(Hello{Version: "1.0.0", Contract: Contract, Node: "ctl-1", TrustSeq: 3, Arch: "arm64", OS: "debian-13", Parts: []string{PartServices}})
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseHello(b)
	})
}

func FuzzParseOpen(f *testing.F) {
	b, _ := json.Marshal(testOpen())
	f.Add(b)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = ParseOpen(b)
	})
}
