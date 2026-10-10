package coreapp

import (
	"encoding/json"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// FuzzOpenParams sends an OPEN with params of the fuzzer's own making for
// every core action of service api the shell may open, and holds what the
// contract says about the answer: either the channel closes with a code of the
// contract, or it answers a result -- never a crash, never a result over
// wire.MaxCallResult, and for a call-style action never something that is not
// JSON.
//
// Each iteration gets a fixture of its own. It is the slower way, and it is
// the one that measures the params: a fixture shared over the run would carry
// the channel numbers, the pending TOTP secret and the log of every iteration
// before it, and a failure could then come from the one before rather than
// from these params.
func FuzzOpenParams(f *testing.F) {
	f.Add("", 0)
	f.Add(`{}`, 0)
	f.Add(`{"app":"org.sorglos.y"}`, 2)
	f.Add(`{"code":"123456"}`, 5)
	f.Add(`{"app":""}`, 2)
	f.Add(`{"App":"org.sorglos.y"}`, 2)
	f.Add(`{"app":"org.sorglos.y","app":"x"}`, 2)
	f.Add(`[1,2,3]`, 0)
	f.Add(`{"role":"admin","rights":["org.sorglos.y/use"]}`, 9)

	shell := shellActions()
	f.Fuzz(func(t *testing.T, params string, which int) {
		a := shell[((which%len(shell))+len(shell))%len(shell)]
		fx := newFixture(t)
		fx.PutApp(t, appY, true)
		fx.Grant(t, appY+"/"+core.RightUse)
		c := fx.openRaw(t, a, params)
		if a.Flow == core.FlowIn || a.Flow == core.FlowTransfer {
			// The opener of such a channel sends its EOF; without it the case
			// would wait for an upload nobody makes.
			c.eof(t)
		}
		body, cl := fx.drain(t, c)
		if cl.Status == wire.StatusOK {
			if len(body) > wire.MaxCallResult {
				t.Fatalf("%d bytes of result", len(body))
			}
			if a.Flow == core.FlowCall && len(body) > 0 && !json.Valid(body) {
				t.Fatalf("the result is no JSON: %q", firstBytes(body))
			}
			return
		}
		if cl.Code.Message() == "" {
			t.Fatalf("close with a code outside the contract: %+v", cl)
		}
	})
}

// openRaw sends an OPEN whose params are the fuzzer's bytes exactly as they
// came, without going through json.Marshal: params that are no JSON at all
// have to reach wire.ParseOpen, and a marshaller in between would refuse them
// here instead -- which would measure the fixture and not the API.
func (h *harness) openRaw(t *testing.T, a core.Action, params string) *chann {
	t.Helper()
	h.num++
	c := &chann{h: h, num: h.num, credit: wire.MinWindow}
	body := `{"app":` + quote(a.App) + `,"service":` + quote(a.Service) +
		`,"action":` + quote(a.Name) + `,"window":4096`
	if params != "" {
		body += `,"params":` + params
	}
	body += `}`
	h.peer.write(t, wire.Frame{Type: wire.TypeOpen, Channel: c.num, Payload: []byte(body)})
	return c
}

// quote is a JSON string of s; the values it is given are names of the
// contract, so it cannot fail.
func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// shellActions are the core actions of service api, which the shell may open
// without a signed task. The root tasks of service controller are left out:
// no params make one, so they would only ever answer E_CONFIRM.
func shellActions() []core.Action {
	var out []core.Action
	for _, a := range core.All() {
		if a.Service == core.ServiceAPI && core.ShellOpens(a) {
			out = append(out, a)
		}
	}
	return out
}

// firstBytes cuts a value short for a message, so no failure prints megabytes.
func firstBytes(b []byte) []byte { return b[:min(len(b), 64)] }

// drain reads a channel to its CLOSE and returns what came with it.
func (h *harness) drain(t *testing.T, c *chann) ([]byte, wire.Close) {
	t.Helper()
	var body []byte
	for {
		f := c.pull(t)
		switch f.Type {
		case wire.TypeData:
			body = append(body, f.Payload...)
		case wire.TypeClose:
			cl, err := wire.ParseClose(f.Payload)
			if err != nil {
				t.Fatalf("close payload: %v", err)
			}
			return body, cl
		}
	}
}
