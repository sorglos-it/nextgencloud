package link

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/coder/websocket"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// The three properties a reason has to keep, and they are the whole point of
// the closed list: it reaches the journal, it does NOT reach the peer, and it
// carries nothing a peer wrote.

// A printed ErrLink says exactly what it said before the reasons existed. This
// is the one that protects the peer: the head of link.go promises that nothing
// on the tunnel says which step failed, and an error whose text grew a place
// would break that promise everywhere the text is used at once.
func TestAPrintedErrLinkStillSaysNothingAboutThePlace(t *testing.T) {
	for _, r := range reasons {
		err := broke(r)
		if err.Error() != ErrLink.Error() {
			t.Fatalf("broke(%q).Error() = %q, want exactly %q", r, err.Error(), ErrLink.Error())
		}
		if strings.Contains(err.Error(), string(r)) {
			t.Fatalf("broke(%q) prints its own place", r)
		}
	}
}

// Every reason is still an ErrLink. Callers test for that and nothing else -
// link.go's read loop among them - so a reason that stopped matching would
// turn a broken contract into an orderly end.
func TestEveryReasonIsStillErrLink(t *testing.T) {
	for _, r := range reasons {
		if !errors.Is(broke(r), ErrLink) {
			t.Fatalf("broke(%q) is not ErrLink", r)
		}
	}
}

// ReasonOf answers for a broken contract and keeps quiet for everything else.
// An API that cannot be reached has no clause, so its reason is empty and the
// journal writes nothing rather than a guess.
func TestReasonOfKeepsQuietWhereThereIsNoClause(t *testing.T) {
	for what, err := range map[string]error{
		"nothing at all":         nil,
		"the API is away":        ErrAPI,
		"values of our own":      ErrConfig,
		"a bare ErrLink":         ErrLink,
		"something of elsewhere": errors.New("x"),
	} {
		if got := ReasonOf(err); got != "" {
			t.Fatalf("%s: ReasonOf = %q, want the empty reason", what, got)
		}
	}
	if got := ReasonOf(broke(ReasonDecrypt)); got != ReasonDecrypt {
		t.Fatalf("ReasonOf = %q, want %q", got, ReasonDecrypt)
	}
}

// What a reason may look like. It is a fixed word of reason.go, so it is short,
// lower-case ASCII with dashes, and above all it holds no digits: a number in
// one of these could only have come from the wire, and a number from the wire
// is a number a stranger chose.
func TestEveryReasonIsFromTheClosedList(t *testing.T) {
	seen := map[Reason]bool{}
	for _, r := range reasons {
		if seen[r] {
			t.Fatalf("%q is in the list twice", r)
		}
		seen[r] = true
		if r == "" || len(r) > 32 {
			t.Fatalf("%q is not a word of this file", r)
		}
		for _, c := range string(r) {
			if c > unicode.MaxASCII || (!unicode.IsLower(c) && c != '-') {
				t.Fatalf("%q holds %q, which is no part of a fixed word", r, c)
			}
		}
	}
}

// And now the places themselves: each one names ITS OWN clause. This is the
// test that makes the measurement of 24.10.2026 impossible to repeat - a
// journal line that could have come from thirteen places.
//
// Each case is a dying mutant of its own: give any of these the reason of
// another place and exactly one case goes red, which is what tells the thirteen
// apart. The living control is the line above every want: the same run with the
// peer behaving still has to reach the end it reached before.

func TestTheDialNamesWhichClauseWasBroken(t *testing.T) {
	for _, c := range []struct {
		what string
		want Reason
		// serve speaks for the API. accept says whether it offers the
		// subprotocol of the tunnel at all.
		accept bool
		answer func(*testing.T, *websocket.Conn, *http.Request)
	}{{
		what:   "a server that does not speak the subprotocol",
		want:   ReasonSubprotocol,
		accept: false,
		answer: func(t *testing.T, c *websocket.Conn, r *http.Request) {
			for {
				if _, _, err := c.Read(r.Context()); err != nil {
					return
				}
			}
		},
	}, {
		what:   "message 2 of the wrong size",
		want:   ReasonHandshake,
		accept: true,
		answer: func(t *testing.T, c *websocket.Conn, r *http.Request) {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
			c.Write(r.Context(), websocket.MessageBinary, make([]byte, wire.HandshakeSize+1))
			for {
				if _, _, err := c.Read(r.Context()); err != nil {
					return
				}
			}
		},
	}, {
		what:   "message 2 not binary",
		want:   ReasonHandshake,
		accept: true,
		answer: func(t *testing.T, c *websocket.Conn, r *http.Request) {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
			c.Write(r.Context(), websocket.MessageText, []byte("hello"))
			for {
				if _, _, err := c.Read(r.Context()); err != nil {
					return
				}
			}
		},
	}, {
		what:   "message 2 of the right size that is no Noise message",
		want:   ReasonNoise,
		accept: true,
		answer: func(t *testing.T, c *websocket.Conn, r *http.Request) {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
			c.Write(r.Context(), websocket.MessageBinary, bytes.Repeat([]byte{7}, wire.HandshakeSize))
			for {
				if _, _, err := c.Read(r.Context()); err != nil {
					return
				}
			}
		},
	}} {
		t.Run(c.what, func(t *testing.T) {
			opts := &websocket.AcceptOptions{}
			if c.accept {
				opts.Subprotocols = []string{tunnel.Subprotocol}
			}
			cfg := serve(t, func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, opts)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				c.answer(t, conn, r)
			})
			_, err := Dial(context.Background(), cfg)
			// The living half: it is still a broken contract and not an
			// unreachable API, exactly as before the reasons.
			if !errors.Is(err, ErrLink) {
				t.Fatalf("Dial = %v, want the link ended", err)
			}
			if got := ReasonOf(err); got != c.want {
				t.Fatalf("reason = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTheRunNamesWhichClauseWasBroken(t *testing.T) {
	for _, c := range []struct {
		what string
		want Reason
		send func(*apiSide)
	}{{
		what: "a message that is not binary",
		want: ReasonNotBinary,
		send: func(a *apiSide) {
			a.c.Write(context.Background(), websocket.MessageText, []byte("{}"))
		},
	}, {
		// The one that would be load-dependent if anything in this package is,
		// and the reason the whole change was asked for.
		what: "a message that does not decrypt",
		want: ReasonDecrypt,
		send: func(a *apiSide) {
			a.c.Write(context.Background(), websocket.MessageBinary, bytes.Repeat([]byte{7}, 64))
		},
	}, {
		what: "a frame of an unknown type",
		want: ReasonFrame,
		send: func(a *apiSide) {
			a.c.Write(context.Background(), websocket.MessageBinary, mustSeal(a, []byte{9, 0, 0, 0, 1}))
		},
	}, {
		what: "a control message only the controller may send",
		want: ReasonControlBody,
		send: func(a *apiSide) { a.control(wire.CtlStatus, tunnel.Status{DiskFree: 1}) },
	}, {
		what: "a second hello, which is a type the API may not repeat here",
		want: ReasonBye,
		send: func(a *apiSide) {
			a.control(wire.CtlHello, tunnel.Hello{Version: "1.0.0", Contract: tunnel.Contract})
		},
	}} {
		t.Run(c.what, func(t *testing.T) {
			_, ended := running(t, nil, func(a *apiSide) {
				c.send(a)
				a.quiet()
			})
			err := waitFor(t, ended)
			if !errors.Is(err, ErrLink) {
				t.Fatalf("Run = %v, want the link ended", err)
			}
			got := ReasonOf(err)
			if got != c.want {
				t.Fatalf("reason = %q, want %q", got, c.want)
			}
			// And whatever it is, it came out of the closed list.
			if !slices.Contains(reasons, got) {
				t.Fatalf("reason %q is not in the closed list", got)
			}
		})
	}
}
