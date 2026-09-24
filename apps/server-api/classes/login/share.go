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

// shareCache keeps the last counted share with the time it was counted.
type shareCache struct {
	mu      sync.Mutex
	percent int
	counted int64
	ok      bool
}

// totpShare returns how many of the installation's users hold a TOTP secret,
// in percent (0..100), counted in the database and kept for shareTTL seconds.
// A database that cannot be reached gives the last counted value, and 0 if
// there is none: a made-up answer must still be built, and it must be built
// the same way for every name, so this never fails the request.
func (h *Handler) totpShare(ctx context.Context) int {
	now := h.S.Now()
	h.share.mu.Lock()
	defer h.share.mu.Unlock()
	if h.share.ok && now-h.share.counted < shareTTL {
		return h.share.percent
	}
	users, err := h.S.DB.UserList(ctx)
	if err != nil {
		return h.share.percent
	}
	with := 0
	for _, u := range users {
		if len(u.TOTP) > 0 {
			with++
		}
	}
	if len(users) > 0 {
		h.share.percent = with * 100 / len(users)
	} else {
		h.share.percent = 0
	}
	h.share.counted, h.share.ok = now, true
	return h.share.percent
}
