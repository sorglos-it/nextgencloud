package browser

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/dbwait"
	"github.com/sorglos-it/nextgencloud/apps/server-api/classes/httpapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/session"
)

// busySession is a store whose session read cannot get through because another
// program holds the database -- a state of the INSTALLATION (point 48),
// carrying dbwait.ErrBusy the way package sqlstore hands the named cause of
// the deadline back (point 49).
type busySession struct{ dbapi.Store }

func (busySession) SessionGet(context.Context, []byte) (dbapi.Session, error) {
	return dbapi.Session{}, fmt.Errorf("sqlstore: the read did not get through: %w", dbwait.ErrBusy)
}

// cookieValue is a cookie value of the right shape. Which bytes they are does not
// matter to this case: the read fails before any row is looked at, which is
// the whole reason an answer about it is no oracle.
func cookieValue(b byte) string {
	raw := make([]byte, session.TokenSize)
	for i := range raw {
		raw[i] = b
	}
	return enc.B64.EncodeToString(raw)
}

// TestTheSocketSaysComeBackWhenTheDatabaseIsBusy is the assurance at the
// second caller of login.Live (browser.go:121 before this change). The upgrade
// is refused before a link exists, so what goes out is an ordinary HTTP
// answer -- and until Live carried its error up it was a bare 503, the same
// one a database that is away gets. The code stays E_UNREACHABLE: a word of
// its own lies in frozen shared/wire/wire.go:450-462 (BLOCKED, P9).
func TestTheSocketSaysComeBackWhenTheDatabaseIsBusy(t *testing.T) {
	h := newServer(t)
	h.Srv.S.DB = busySession{Store: h.Srv.S.DB}
	w, r := socketRequest(cookieValue(1))
	h.Srv.Socket(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Retry-After"); got != strconv.Itoa(httpapi.RetryBusy) {
		t.Fatalf("Retry-After %q, want %d", got, httpapi.RetryBusy)
	}
}

// TestTheBusySocketAnswersTheSameForEveryCookie holds what the header above
// must not buy: an answer that follows which token was sent would say whether
// a token is one this API handed out. Numbers and not an argument -- status,
// every header with every value, body.
func TestTheBusySocketAnswersTheSameForEveryCookie(t *testing.T) {
	h := newServer(t)
	h.Srv.S.DB = busySession{Store: h.Srv.S.DB}
	one, r1 := socketRequest(cookieValue(1))
	h.Srv.Socket(one, r1)
	two, r2 := socketRequest(cookieValue(2))
	h.Srv.Socket(two, r2)
	if one.Code != two.Code {
		t.Errorf("status %d vs %d", one.Code, two.Code)
	}
	if !maps.EqualFunc(one.Header(), two.Header(), slices.Equal[[]string]) {
		t.Errorf("headers %v vs %v", one.Header(), two.Header())
	}
	if one.Body.String() != two.Body.String() {
		t.Errorf("bodies %q vs %q", one.Body, two.Body)
	}
}
