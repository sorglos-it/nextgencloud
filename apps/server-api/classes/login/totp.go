package login

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/session"
)

// totpOpts are the parameters of RFC 6238 that authenticator apps expect.
// Skew 1 takes the period before and the one after this one as well, so a
// phone whose clock is half a minute off still gets in.
var totpOpts = totp.ValidateOpts{Period: session.TOTPPeriod, Skew: 1,
	Digits: otp.Digits(session.TOTPDigits), Algorithm: otp.AlgorithmSHA1}

// timeOf turns Unix seconds into the time TOTP counts its periods from.
func timeOf(now int64) time.Time { return time.Unix(now, 0).UTC() }

// BeginTOTP makes a new TOTP secret for the user of that login name and hands
// it back twice: in core.TOTPBegin, the answer of ngc.desktop/totp-begin,
// where the user reads or scans it, and sealed with the key every API of the
// installation shares. The sealed one is the only form that belongs in
// dbapi.User.TOTP, and it is the only one checkTOTP reads -- so a dump of the
// database is no second factor. Nothing is written here: whoever calls this
// keeps the sealed secret until the user has confirmed it with a code.
//
// Kept where, and that is the warning: the sealed secret is bound to nothing.
// secrets.Box.Seal takes no associated data, so a sealed secret out of one
// user's row opens in another's and works there. It has to stay on the server
// between this answer and the code that confirms it -- handed to the client
// and taken back, it would let whoever ever saw one row's secret set it as
// anybody's second factor.
func (h *Handler) BeginTOTP(name string) (core.TOTPBegin, []byte, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: "NGC", AccountName: name, Period: session.TOTPPeriod,
		Digits: otp.Digits(session.TOTPDigits), Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return core.TOTPBegin{}, nil, err
	}
	sealed, err := h.S.Box.Seal([]byte(key.Secret()))
	if err != nil {
		return core.TOTPBegin{}, nil, err
	}
	return core.TOTPBegin{Secret: key.Secret(), URI: key.URL()}, sealed, nil
}

// checkTOTP reports whether code is a code of the user's sealed secret for the
// time now -- this period or one either side (totpOpts). Everything else is
// false and nothing else: a row that carries no secret, a code that is not six
// digits (session.ValidTOTP), a secret this API's key does not open. So a
// caller that asks where there is no second factor never lets anybody in, and a
// damaged secret shuts the door instead of opening it.
func (h *Handler) checkTOTP(row dbapi.User, code string, now int64) bool {
	if len(row.TOTP) == 0 || !session.ValidTOTP(code) {
		return false
	}
	secret, err := h.S.Box.Open(row.TOTP)
	if err != nil {
		return false
	}
	ok, err := totp.ValidateCustom(code, string(secret), timeOf(now), totpOpts)
	return err == nil && ok
}
