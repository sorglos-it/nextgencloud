// Package pairing joins this server to an API with the pairing code the shell
// shows: it sends the controller's own key under the one-time secret of the
// code and returns what controller.json keeps of that API.
//
// The code is a secret, and this runs as root: it is never logged, never put
// into an error and never shown. Only tunnel.ParsePairCode reads it, and
// nothing here formats a tunnel.PairCode, whose String is the code itself.
package pairing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Timeout bounds one pairing. The address comes from the code, so a server
// that answers slowly or not at all must not hold the command forever.
const Timeout = 30 * time.Second

// maxRoles is the most roles one server carries (tunnel.RoleWeb, RoleAPI,
// RoleDB, RoleServices), as config.Controller holds them.
const maxRoles = 4

// ErrInput reports a pairing this server could not use even if it worked. It
// names no value: the input holds the code.
var ErrInput = errors.New("pairing: this server cannot pair with these values")

// Input is what Do needs. Code is the pairing code as the admin typed it.
type Input struct {
	Code  string      // the code; a secret, checked by tunnel.ParsePairCode alone
	Name  string      // this controller's name (wire.ValidName), shown to the admin in the API
	Roles []string    // the roles of this server (tunnel.ValidRole), none twice
	Noise noise.DHKey // this server's own key pair; only Public leaves it
	// Client replaces the client Do builds out of the code. Only a test sets
	// it: the client Do builds is the one that checks the API's TLS key
	// against the pin of the code (tunnel.PinnedTLS), and any other client
	// gives that up.
	Client *http.Client
}

// Result is what pairing yields for controller.json: the API to dial and the
// trust list version to pin.
type Result struct {
	APIs   []config.APIRef
	Anchor trust.Anchor
}

// Do pairs this server with the API the code names: it posts a
// tunnel.PairRequest to tunnel.PathPair of that API, over TLS checked against
// the pin of the code, and answers with what controller.json keeps. Only 204
// pairs; every other answer is tunnel.ErrPair.
//
// The secret of the code is good for one pairing, so what Do is handed is
// checked before the request goes out: the code, the name, the roles and the
// key. That is not all of config.Controller.Validate; the database, its key,
// the release key, podman and the rest of the file never reach this package.
// For node.pair they are whole because config.LoadController checked them.
// Another caller answers for them itself, or a pairing that worked can still
// end in a configuration that will not save.
func Do(ctx context.Context, in Input) (Result, error) {
	// First and before anything else: nothing but ParsePairCode looks at the
	// text, and its error quotes none of it.
	code, err := tunnel.ParsePairCode(in.Code)
	if err != nil {
		return Result{}, err
	}
	if !wire.ValidName(in.Name) || !validRoles(in.Roles) || len(in.Noise.Public) != 32 {
		return Result{}, ErrInput
	}
	host, port, ok := address(code.URL)
	if !ok {
		return Result{}, tunnel.ErrPairCode
	}
	client := in.Client
	if client == nil {
		// The pin stands in for the chain and the name, so the name is only
		// what SNI carries to a server with several certificates.
		tlsCfg := tunnel.PinnedTLS(code.TLS)
		tlsCfg.ServerName = host
		own := &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
			Timeout:   Timeout,
			// The request carries the name and key of this server under the
			// MAC of the one-time secret, and it goes to the address of the
			// code alone. A redirect would send it on, to another server or
			// in clear text; it is taken as the answer it is, and only 204
			// pairs.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		// Pairing happens once; the link to the API is the tunnel's, not this
		// one's.
		defer own.CloseIdleConnections()
		client = own
	}
	body, err := json.Marshal(tunnel.PairRequest{
		Name:  in.Name,
		Noise: in.Noise.Public,
		MAC:   tunnel.PairMAC(code.Secret, in.Noise.Public, code.Key),
	})
	if err != nil {
		return Result{}, ErrInput
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+net.JoinHostPort(host, port)+tunnel.PathPair, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	// The answer of a pairing carries nothing this side reads. It is taken to
	// the bound of a pairing message, so the connection can be closed cleanly
	// and a talkative server fills no memory, and it goes nowhere: only that
	// server knows what it wrote in there.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, tunnel.MaxPairRequest))
	if resp.StatusCode != http.StatusNoContent {
		return Result{}, tunnel.ErrPair
	}
	return Result{
		APIs:   []config.APIRef{{URL: code.URL, Key: code.Key, TLS: code.TLS}},
		Anchor: code.Anchor,
	}, nil
}

// Show returns the two lines the admin compares with the display of the shell
// (shared/ulh.md): the fingerprint of this controller's key, and the anchor by
// version and fingerprint. It takes the anchor, not the code it came in
// (Result.Anchor is that anchor), so no caller of Show has to hold a
// tunnel.PairCode, whose String is the pairing code itself.
func Show(anchor trust.Anchor, noisePub []byte) string {
	return fmt.Sprintf("Fingerabdruck dieses Controllers: %s\nAnker: Fassung %d, %s\n",
		tunnel.Fingerprint(noisePub), anchor.Seq, tunnel.Fingerprint(anchor.Hash))
}

// address splits the tunnel address of a code into host and port. The host
// names the server in the handshake, and both together are the whole address
// the request goes to: no path, user, query or fragment of an address travels
// with it. ParsePairCode has taken the URL through trust.ValidTunnelURL
// already; it is asked again here so that this stays true of every caller.
func address(u string) (host, port string, ok bool) {
	if !trust.ValidTunnelURL(u) {
		return "", "", false
	}
	host, port, err := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(u, "wss://"), "/tunnel"))
	if err != nil {
		return "", "", false
	}
	return host, port, true
}

// validRoles checks the roles of this server: one to maxRoles roles of the
// contract, none of them twice, as config.Controller.Validate asks for them.
func validRoles(roles []string) bool {
	if len(roles) == 0 || len(roles) > maxRoles {
		return false
	}
	for i, r := range roles {
		if !tunnel.ValidRole(r) || slices.Contains(roles[:i], r) {
			return false
		}
	}
	return true
}
