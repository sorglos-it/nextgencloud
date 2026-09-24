package login

import (
	"context"
	"sync"
)

// shareTTL is how long a counted TOTP share is used again. The share moves
// only when users are added or change their TOTP, so a minute-old number is
// as good as a fresh one -- and counting on every login/start would put a full
// table scan in front of every login.
const shareTTL = 300

// shareCache keeps the last counted share and the time the count was last
// tried. Tried, not counted: a try that failed sets it too, so a database that
// cannot be reached is asked once per shareTTL like one that answers. Counting
// on every login was the shape of it before, and on a real database that is no
// error that comes back at once but a connection that has to time out first.
type shareCache struct {
	mu      sync.Mutex
	percent int
	tried   int64
	asked   bool // whether it was ever tried, so that a small clock is no gate
}

// totpShare returns how many of the installation's users hold a TOTP secret,
// in percent (0..100), counted in the database and kept for shareTTL seconds.
// A database that cannot be reached gives the last counted value, and 0 if
// there is none: a made-up answer must still be built, and it must be built
// the same way for every name, so this never fails the request.
//
// The lock is held to take the turn and given back before the database is
// asked, so the one login that counts waits on it and no other. Two logins at
// once cannot both count: the turn is taken under the lock.
func (h *Handler) totpShare(ctx context.Context) int {
	now := h.S.Now()
	h.share.mu.Lock()
	kept := h.share.percent
	if h.share.asked && now-h.share.tried < shareTTL {
		h.share.mu.Unlock()
		return kept
	}
	h.share.tried, h.share.asked = now, true
	h.share.mu.Unlock()

	users, err := h.S.DB.UserList(ctx)
	if err != nil {
		return kept
	}
	with := 0
	for _, u := range users {
		if len(u.TOTP) > 0 {
			with++
		}
	}
	percent := 0
	if len(users) > 0 {
		percent = with * 100 / len(users)
	}
	h.share.mu.Lock()
	h.share.percent = percent
	h.share.mu.Unlock()
	return percent
}
