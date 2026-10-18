package session

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// seeded builds a user key from a number, so a test list can hold more keys
// than trusttest's single seed byte allows. Passkeys get a credential ID of
// credLen bytes, as an authenticator would hand out.
func seeded(n int, passkey bool, credLen int) proof.UserKey {
	seed := make([]byte, ed25519.SeedSize)
	seed[0], seed[1], seed[2] = byte(n), byte(n>>8), byte(n>>16)
	if passkey {
		seed[3] = 1
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	if !passkey {
		return proof.UserKey{ID: sign.KeyID(pub), Kind: proof.KindPassword, Alg: proof.AlgEdDSA,
			Public: enc.Bytes(pub), KDF: &proof.KDF{Alg: proof.KDFArgon2id,
				Salt:   bytes.Repeat([]byte{byte(n)}, proof.SaltSize),
				MemKiB: proof.DefaultMemKiB, Time: proof.DefaultTime, Par: proof.DefaultPar}}
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		panic(err)
	}
	cred := make([]byte, credLen)
	for i := range cred {
		cred[i] = byte(n + i)
	}
	return proof.UserKey{ID: sign.KeyID(spki), Kind: proof.KindPasskey, Alg: proof.AlgEdDSA,
		Public: enc.Bytes(spki), CredID: cred}
}

// homogeneous returns a valid list whose users all have the very same form:
// passkeys of the same number, with a password key and a role that asks for
// the password too when both is set. A made-up answer has nowhere to hide in
// such an installation — anything but that one form is recognisable at once.
func homogeneous(t testing.TB, users, passkeys int, both bool) *trust.List {
	t.Helper()
	l := trusttest.Genesis(trusttest.NewSigner(1))
	roles := []string{trust.RoleAdmin}
	if both {
		l.Roles = append(l.Roles, trust.Role{Name: "careful", Level: trust.LevelCritical, PasswordToo: true})
		roles = append(roles, "careful")
	}
	l.Users = nil
	for i := 0; i < users; i++ {
		u := trust.User{ID: trusttest.ID(byte(0x20 + i)), Name: fmt.Sprintf("u%d", i), Roles: roles}
		for k := 0; k < passkeys; k++ {
			u.Keys = append(u.Keys, seeded(i*100+k, true, 32))
		}
		if both {
			u.Keys = append(u.Keys, seeded(i*100+90, false, 0))
		}
		l.Users = append(l.Users, u)
	}
	// A list keeps its users sorted by ID, and base64url does not sort the
	// way the bytes behind it do.
	slices.SortFunc(l.Users, func(a, b trust.User) int { return strings.Compare(a.ID, b.ID) })
	if err := l.Validate(); err != nil {
		t.Fatalf("homogeneous list of %d users: %v", users, err)
	}
	return l
}

// TestMadeUpAnswerLooksLikeTheInstallation nails the finding that fixed odds
// give a made-up answer away. In a list where every user holds two passkeys, a
// password key and a role with passwordToo, and where every user has TOTP,
// every made-up answer must have exactly that form: anything else says at once
// that the name does not exist. The same for a list of passkeys alone.
func TestMadeUpAnswerLooksLikeTheInstallation(t *testing.T) {
	names := make([]string, 200)
	for i := range names {
		names[i] = fmt.Sprintf("ghost%d", i)
	}
	cases := []struct {
		name      string
		list      *trust.List
		share     int
		passkeys  int
		wantKDF   bool
		wantToo   bool
		wantTOTP  bool
		wantCount int
	}{
		{"every user with both kinds and TOTP", homogeneous(t, 8, 2, true), 100, 2, true, true, true, 2},
		{"every user with passkeys alone, nobody with TOTP", homogeneous(t, 8, 3, false), 0, 3, false, false, false, 3},
	}
	for _, c := range cases {
		for _, n := range names {
			got, err := Challenge(c.list, ChallengeInput{Name: n, TOTPShare: c.share, Key: loginKey,
				Nonce: bytes.Repeat([]byte{0x58}, NonceSize), Now: now, MaxLifetime: loginMaxLifetime})
			if err != nil {
				t.Fatalf("%s: %s: %v", c.name, n, err)
			}
			if (got.KDF != nil) != c.wantKDF || got.PasswordToo != c.wantToo || got.TOTP != c.wantTOTP ||
				len(got.Passkeys) != c.wantCount {
				t.Fatalf("%s: %s made up kdf=%v passkeys=%d totp=%v passwordToo=%v, "+
					"but every real user has kdf=%v passkeys=%d totp=%v passwordToo=%v",
					c.name, n, got.KDF != nil, len(got.Passkeys), got.TOTP, got.PasswordToo,
					c.wantKDF, c.wantCount, c.wantTOTP, c.wantToo)
			}
			// A credential ID longer or shorter than the real ones would be
			// as telling as their number.
			for _, p := range got.Passkeys {
				if len(p.CredID) != 32 {
					t.Fatalf("%s: %s made up a credential ID of %d bytes, the real ones have 32",
						c.name, n, len(p.CredID))
				}
			}
		}
	}
}

// TestMadeUpAnswerFollowsTheSpread checks that a made-up answer does not pick
// one form of a mixed installation: over many names every form the list holds
// must come up, so no name is sorted by its form alone.
func TestMadeUpAnswerFollowsTheSpread(t *testing.T) {
	f := newFixture(t)
	forms := map[string]int{}
	for i := 0; i < 400; i++ {
		got, err := Challenge(f.list, loginInput(fmt.Sprintf("ghost%d", i), false,
			bytes.Repeat([]byte{0x58}, NonceSize), now))
		if err != nil {
			t.Fatal(err)
		}
		forms[fmt.Sprintf("kdf=%v passkeys=%v", got.KDF != nil, len(got.Passkeys) > 0)]++
	}
	// The fixture's list holds users with passkeys alone, with a password
	// alone and with both, so all three must appear.
	for _, want := range []string{"kdf=false passkeys=true", "kdf=true passkeys=false", "kdf=true passkeys=true"} {
		if forms[want] == 0 {
			t.Errorf("no made-up answer of the form %q, although the list has such users: %v", want, forms)
		}
	}
}

// TestChallengeCostsTheSameForEveryName nails the finding that time and memory
// told a known name from an unknown one although the answers did not. Both
// paths walk the whole list and build the made-up answer, so the cost follows
// the list and not the name. Only the allocations decide, because a time
// measurement flickers from one machine to the next; the times are printed so
// that a look at the test output still shows them.
func TestChallengeCostsTheSameForEveryName(t *testing.T) {
	l := homogeneous(t, 200, 4, true) // 800 passkeys and 200 password keys
	nonce := bytes.Repeat([]byte{0x58}, NonceSize)
	call := func(name string) func() {
		in := ChallengeInput{Name: name, TOTPShare: 50, Key: loginKey, Nonce: nonce,
			Now: now, MaxLifetime: loginMaxLifetime}
		if _, err := Challenge(l, in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return func() { _, _ = Challenge(l, in) }
	}
	// u0 is the first user of the list, u199 the last, and ghost is in it
	// nowhere: an early return or a lookup that gives up sooner for one of
	// them would show here.
	runs := []struct {
		name string
		run  func()
	}{{"u0", call("u0")}, {"u199", call("u199")}, {"ghost", call("ghost")}}
	var allocs []float64
	for _, r := range runs {
		n := testing.AllocsPerRun(20, r.run)
		start := time.Now()
		for i := 0; i < 200; i++ {
			r.run()
		}
		each := time.Since(start) / 200
		t.Logf("%-6s %6.0f allocations, %v each", r.name, n, each)
		allocs = append(allocs, n)
	}
	low, high := allocs[0], allocs[0]
	for _, n := range allocs {
		low, high = min(low, n), max(high, n)
	}
	// The known path copies the user's own keys on top, a handful of small
	// allocations; anything beyond a quarter more is a difference that says
	// whether the name exists.
	if high > low*1.25+8 {
		t.Errorf("allocations run from %.0f to %.0f: the cost tells a known name from an unknown one", low, high)
	}
}

// TestChallengeRefusesWhatItCannotBuildAnAnswerFrom drives the entry check of
// Challenge. It is one if with six conditions (session.go:375), and before
// this case NOT ONE of them was held: measured on 18.10.2026, a mutant that
// dropped any single condition -- and one that disarmed the whole if -- left
// all 96 cases of this package green. ErrChallenge did not appear in one test
// file of shared/session.
//
// The answer is asserted by identity and not with errors.Is, because that is
// the promise of this error: ErrChallenge "never depends on the login name, so
// it tells no one whether a user exists" (session.go:262). A wrapped error
// carrying a reason would pass errors.Is and break exactly that promise.
//
// WHAT EACH CASE HERE DOES AND DOES NOT HOLD, measured mutant by mutant on
// 18.10.2026 against these cases alone:
//
//   - the key, the share, the name and the whole if: the mutant dies HERE, at
//     the assertion below.
//   - the list: dropping "l == nil" does not reach an assertion at all, it
//     PANICS on the nil list. The kill is a crash, and it is named as one.
//   - THE NONCE IS HELD BY A SECOND WALL, not by this if. Dropping
//     "len(in.Nonce) != NonceSize" leaves these two cases GREEN, because
//     finishChallenge turns the answer away anyway (ParseLoginChallenge wants
//     exactly NonceSize) and answers the same ErrChallenge. Both mutants
//     together were run to settle it: with the entry condition AND the
//     self-check gone, both nonce cases give nil. So these two cases hold the
//     RULE about the nonce, and the condition in this if is a double of a
//     wall further down -- which no case can tell apart, because both walls
//     answer the same error on purpose.
func TestChallengeRefusesWhatItCannotBuildAnAnswerFrom(t *testing.T) {
	f := newFixture(t)
	nonce := bytes.Repeat([]byte{0x11}, NonceSize)
	good := loginInput("admin", false, nonce, 1790000000)
	// The intermediate check: the good input has to get through, or every
	// refusal below would be the answer to something else.
	if _, err := Challenge(f.list, good); err != nil {
		t.Fatalf("the good input was refused: %v", err)
	}
	for name, c := range map[string]struct {
		list   *trust.List
		mutate func(in *ChallengeInput)
	}{
		"no list":                     {nil, func(*ChallengeInput) {}},
		"no challenge key":            {f.list, func(in *ChallengeInput) { in.Key = nil }},
		"a nonce one byte short":      {f.list, func(in *ChallengeInput) { in.Nonce = nonce[:NonceSize-1] }},
		"a nonce one byte long":       {f.list, func(in *ChallengeInput) { in.Nonce = bytes.Repeat([]byte{0x11}, NonceSize+1) }},
		"a TOTP share below zero":     {f.list, func(in *ChallengeInput) { in.TOTPShare = -1 }},
		"a TOTP share over a hundred": {f.list, func(in *ChallengeInput) { in.TOTPShare = 101 }},
		"a name that is no login name": {f.list, func(in *ChallengeInput) {
			in.Name = "Admin" // ValidLoginName takes lower case only
		}},
		"no name at all": {f.list, func(in *ChallengeInput) { in.Name = "" }},
	} {
		in := good
		c.mutate(&in)
		if _, err := Challenge(c.list, in); err != ErrChallenge {
			t.Errorf("%s: got %v, want %v", name, err, ErrChallenge)
		}
	}
	// The other side of the one bound that has two ends, so a rule that
	// refused every share could not pass the cases above.
	for _, share := range []int{0, 100} {
		in := good
		in.TOTPShare = share
		if _, err := Challenge(f.list, in); err != nil {
			t.Errorf("a TOTP share of %d was refused: %v", share, err)
		}
	}
}

// TestChallengeRefusesAnAnswerTheBrowserWouldRefuse drives the two refusals
// behind the entry check, both of them unheld before this case.
//
// WHY THEY ARE NOT THE SAME CHECK TWICE. finishChallenge (session.go:414)
// holds the finished answer against ParseLoginChallenge, the rules the browser
// reads it with, "because an answer a browser refuses would tell it that the
// name is unknown". Now and MaxLifetime go into the answer untouched and the
// entry check does NOT look at them, so a zero in either passes the first wall
// and is caught by the second. That is the seam this case measures.
//
// AND ONE REFUSAL NO CASE CAN REACH, named instead of faked: session.go:417
// answers ErrChallenge when json.Marshal of the answer fails. Nothing in
// LoginChallenge can make it fail -- strings, numbers, bools, enc.Bytes, whose
// MarshalJSON cannot return an error (enc.go:64), and *proof.KDF of the same
// shape. A mutant that drops that branch therefore LIVES, and it lives on
// purpose: the branch is defence against a type that may be added later, not
// a path of today. Measured on 18.10.2026 with the branch removed: 96 cases
// green.
func TestChallengeRefusesAnAnswerTheBrowserWouldRefuse(t *testing.T) {
	f := newFixture(t)
	nonce := bytes.Repeat([]byte{0x11}, NonceSize)
	good := loginInput("admin", false, nonce, 1790000000)

	// The self-check, reached with values the entry check lets through.
	for name, mutate := range map[string]func(in *ChallengeInput){
		"a time of zero":            func(in *ChallengeInput) { in.Now = 0 },
		"a time before the epoch":   func(in *ChallengeInput) { in.Now = -1 },
		"no session length at all":  func(in *ChallengeInput) { in.MaxLifetime = 0 },
		"a session length too long": func(in *ChallengeInput) { in.MaxLifetime = MaxLifetime + 1 },
	} {
		in := good
		mutate(&in)
		if _, err := Challenge(f.list, in); err != ErrChallenge {
			t.Errorf("%s: got %v, want %v", name, err, ErrChallenge)
		}
	}

	// A password key without a KDF, on a user the list KNOWS. Challenge does
	// not validate the list it is handed -- trust.List.Validate would refuse
	// such a key (proof.UserKey.Validate wants a KDF for a password key) --
	// so this branch is what stands between an unvalidated list and an answer
	// the browser could not use. The list is built fresh here and the key of
	// the fixture is left alone.
	l := trusttest.Genesis(f.admin)
	if l.Users[1].Name != "admin" || l.Users[1].Keys[0].Kind != proof.KindPassword ||
		l.Users[1].Keys[0].KDF == nil {
		t.Fatalf("Genesis no longer puts the password admin at Users[1]: %q", l.Users[1].Name)
	}
	l.Users[1].Keys[0].KDF = nil
	if _, err := Challenge(l, good); err != ErrChallenge {
		t.Errorf("a password key without a KDF: got %v, want %v", err, ErrChallenge)
	}
	// And the same list with the KDF in place gets through, so the case above
	// is about the KDF and not about this hand-built list.
	if _, err := Challenge(trusttest.Genesis(f.admin), good); err != nil {
		t.Errorf("the untouched list was refused: %v", err)
	}
}
