package login

import (
	"testing"

	"github.com/pquerna/otp/totp"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
)

func TestTOTP(t *testing.T) {
	h, fx := newFixture(t)
	begin, secret, err := h.BeginTOTP(fx.PasswordName)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if begin.Secret == "" || begin.URI == "" {
		t.Fatalf("begin %+v", begin)
	}
	row := dbapi.User{ID: fx.PasswordUser, Name: fx.PasswordName, TOTP: secret}
	code, err := totp.GenerateCode(begin.Secret, timeOf(testNow))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if !session.ValidTOTP(code) {
		t.Fatalf("code %q is no TOTP", code)
	}
	if !h.checkTOTP(row, code, testNow) {
		t.Fatal("the right code was refused")
	}
	if h.checkTOTP(row, "000000", testNow) {
		t.Fatal("a wrong code was taken")
	}
	// A code of the period before last is too old: the skew is one period.
	if h.checkTOTP(row, code, testNow+3*session.TOTPPeriod) {
		t.Fatal("an old code was taken")
	}
	// The secret lies sealed in the row: a database dump alone is not enough.
	if string(secret) == begin.Secret {
		t.Fatal("the TOTP secret is stored in the clear")
	}
}

// TestTOTPWithoutASecret holds checkTOTP to the one thing its callers rest on:
// a row that carries no secret has no TOTP, and no code makes it true. The
// caller in Finish asks only where a secret is there, so nothing today reaches
// this -- and the day one does, it must not be the way in.
func TestTOTPWithoutASecret(t *testing.T) {
	h, fx := newFixture(t)
	begin, _, err := h.BeginTOTP(fx.PasswordName)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	code, err := totp.GenerateCode(begin.Secret, timeOf(testNow))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if h.checkTOTP(dbapi.User{ID: fx.PasswordUser, Name: fx.PasswordName}, code, testNow) {
		t.Fatal("a row without a secret took a code")
	}
	// Nor does a sealed value of another key open: the secret box of another
	// installation is not this one's.
	if h.checkTOTP(dbapi.User{ID: fx.PasswordUser, TOTP: []byte("not sealed at all")}, code, testNow) {
		t.Fatal("a damaged secret took a code")
	}
}

// TestBeginTOTPGivesAFreshSecret: two beginnings never hand out one secret,
// so a second user does not get the first one's.
func TestBeginTOTPGivesAFreshSecret(t *testing.T) {
	h, fx := newFixture(t)
	one, _, err := h.BeginTOTP(fx.PasswordName)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	two, _, err := h.BeginTOTP(fx.PasskeyName)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if one.Secret == two.Secret {
		t.Fatal("two users got the same TOTP secret")
	}
}
