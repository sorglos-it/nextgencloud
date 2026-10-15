package controllers

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// FuzzPair feeds the pairing with everything; no body may ever make a pending
// controller, because this API never handed a secret out.
//
// The query is escaped before it goes into the request, and that is not a
// weaker target: net/http hands the handler the unescaped value, so what the
// handler reads is exactly the fuzzed string. Unescaped, the fuzzer would spend
// its time on httptest.NewRequest, which panics on a URL it cannot parse -- and
// that would be a case about a test helper.
func FuzzPair(f *testing.F) {
	f.Add(`{"name":"a","noise":"AAAA","mac":"AAAA"}`, "")
	f.Add(`{}`, "AAAA")
	f.Add(`{"name":"services-1","noise":"`+zeros32+`","mac":"`+zeros32+`"}`, zeros32)
	f.Fuzz(func(t *testing.T, body, secret string) {
		srv, _ := newServer(t)
		w := httptest.NewRecorder()
		srv.Pair(w, post(tunnel.PathPair+"?secret="+url.QueryEscape(secret), body))
		switch w.Code {
		case 204, 400, 403, 413, 500, 503:
		default:
			t.Fatalf("status %d", w.Code)
		}
		if w.Code == 204 {
			t.Fatalf("a fuzzed request paired: %q %q", body, secret)
		}
		if list, err := srv.S.DB.PendingList(ctx); err != nil || len(list) != 0 {
			t.Fatalf("pending %v: %v", list, err)
		}
	})
}

// FuzzControl feeds channel 0 of a tunnel with everything.
func FuzzControl(f *testing.F) {
	f.Add(`{"t":"hello","b":{}}`)
	f.Add(`{"t":"status"}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, payload string) {
		_, fx := newServer(t)
		c := dialTunnel(t, fx, fx.ControllerKeys)
		c.writeRaw(t, []byte(payload))
		// Either the tunnel takes the message or it ends; both are fine, a
		// panic is not.
		c.drain()
	})
}

// zeros32 is base64url of 32 zero bytes, a seed that gets past the lengths
// tunnel.ParsePairRequest asks for so the fuzzer starts behind the parser.
const zeros32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
