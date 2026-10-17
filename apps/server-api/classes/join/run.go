package join

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/flynn/noise"

	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/noisep"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// Where the joining side puts what it made and what it fetched. The ulh script
// reads SecretFile and writes both keys into api.json, then removes it.
const (
	SecretFile = "/run/ngc/join-secret"
	// JoinTimeout is how long Run waits for an admin to sign the new server
	// in. After that it stops with a German message, because the admin may
	// simply have gone away; nothing is lost and the step can be repeated.
	JoinTimeout = time.Hour
	// KeysRetry is the pause between two tries at PathKeys.
	KeysRetry = 5 * time.Second
)

// ErrJoinTimeout reports that nobody signed the new server in inside
// JoinTimeout. Its text is German because it is what the admin reads on the
// console of the new server.
var ErrJoinTimeout = errors.New("Beitritt abgebrochen: der Server wurde innerhalb einer Stunde nicht in die Vertrauensliste eingetragen")

// ErrJoinInput reports that what the ulh script handed in is not usable: the
// node ID, the tunnel address or the TLS pin of this server itself.
var ErrJoinInput = errors.New("join: node, URL or TLS pin of this server is not usable")

// ErrJoinRefused reports that the running API did not take the request: the
// code is spent, expired, or belongs to another installation. It carries no
// status any more: a status is an answer of that server, and this side says
// only that an answer came and that it was no.
var ErrJoinRefused = errors.New("join: the running API refused the join request")

// What is left of a failed join, and all of it carries NO value. Measured on
// 17.10.2026 against the code before this: what came back read
//
//	Post "https://127.0.0.1:14780/tunnel/join?secret=d5En6nQB...": dial tcp ...
//
// and ngc-api -join put that line into the journal word for word
// (main.go, joinRun). That is the address of the issuing API plus the
// base64url of the SHA-256 of the one-time secret -- the value the API
// spends. The second leak was quieter: a key that could not be written
// answered with the PATH of the private key file.
//
// They are categories and not one line, because the admin's next step
// differs: a request that never went out leaves the code good, an answer from
// the wrong server must not be repeated, and a refusal means the code is
// spent. Same division as the pairing of the controller
// (server-controller/classes/pairing).
var (
	// ErrJoinNoAnswer reports that no answer came at all: nothing listened,
	// the name did not resolve, the connection broke. Nothing joined, so the
	// code is still good.
	ErrJoinNoAnswer = errors.New("join: the issuing API did not answer")
	// ErrJoinTooSlow reports that the answer did not come in time. Whether
	// the API spent the code cannot be known from here.
	ErrJoinTooSlow = errors.New("join: the issuing API did not answer in time")
	// ErrJoinWrongAPI reports that the server which answered holds a TLS key
	// the pin of the code does not name. tunnel.PinnedTLS judges it -- this
	// is only its refusal under a name this package can read, because its
	// own sentinel is unexported in the frozen shared/tunnel (tunnel.go:387).
	ErrJoinWrongAPI = errors.New("join: the answer did not come from the API of the code")
	// ErrJoinSecret reports that one of the keys of this server could not be
	// written. It names no path: the path is where a private key of this
	// installation lies, and whoever reads this line is on the machine and
	// can be told which files they are in the documentation, not in a log.
	ErrJoinSecret = errors.New("join: a key of this server could not be written")
)

// JoinConfig is what Run needs. The ulh script of the new server fills it
// (Aufgabe 25): the code an admin pasted, the node ID and tunnel address the
// server asks to be listed with, and the pin of the TLS key the script has
// already made.
type JoinConfig struct {
	Code string // the join code as text (tunnel.JoinPrefix + base64url)
	Node string // node ID it asks for (wire.ValidName)
	URL  string // its own tunnel address (trust.ValidTunnelURL)
	TLS  []byte // trust.TLSPin of its own TLS key

	// NoiseFile and SignFile are where the two private keys this function
	// makes are written, 0600. The ulh script puts them into api.json.
	NoiseFile string
	SignFile  string
	// SecretFile is where the fetched shared keys land, 0600; empty means
	// SecretFile above.
	SecretFile string

	// Out shows the fingerprint and the anchor for the admin to compare;
	// nil means os.Stdout.
	Out io.Writer
	// Retry is the pause between two tries at PathKeys, Timeout the whole
	// run. Zero means KeysRetry and JoinTimeout; a test sets both small
	// instead of waiting.
	Retry   time.Duration
	Timeout time.Duration
	// Client is the HTTP client for both steps. Nil means one that verifies
	// the running API's TLS key against the pin in the code and nothing else
	// (tunnel.PinnedTLS). A test hands in its own.
	Client *http.Client
}

// Run is the side of the joining server, what ngc-api -join does. In order:
// read the code, make this server's own keys and write them to files, build
// the tunnel.JoinRequest and sign it with tunnel.JoinMAC, POST it to
// tunnel.PathJoin of the issuing API, show tunnel.JoinFingerprint and the
// anchor so the admin can compare them with what the shell shows him, then try
// PathKeys every Retry until an admin has signed the server in, and write the
// two shared keys to SecretFile (0600).
//
// Nothing here trusts the network: the POST verifies the API's TLS key against
// the pin in the code, and the key exchange runs Noise_KK against the API's
// Noise key from the code.
func Run(ctx context.Context, cfg JoinConfig) error {
	code, err := tunnel.ParseJoinCode(cfg.Code)
	if err != nil {
		return err
	}
	if !wire.ValidName(cfg.Node) || !trust.ValidTunnelURL(cfg.URL) || len(cfg.TLS) != sha256.Size {
		return ErrJoinInput
	}
	out := cfg.Out
	if out == nil {
		out = os.Stdout
	}
	client := cfg.Client
	if client == nil {
		tlsCfg := tunnel.PinnedTLS(code.TLS)
		// The pin stays tunnel's business: its own check decides, and this
		// only renames its refusal, whose sentinel is unexported there.
		// Anything else would be a second judge, and a second judge is one
		// that can be milder than the first.
		pinned := tlsCfg.VerifyConnection
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if err := pinned(cs); err != nil {
				return ErrJoinWrongAPI
			}
			return nil
		}
		client = &http.Client{Timeout: 60 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	}
	keys, signKey, err := makeKeys()
	if err != nil {
		return err
	}
	// The private keys go to disk before anything travels: a request the API
	// kept whose keys this server then lost would have to be signed off again.
	if err := writeSecret(cfg.NoiseFile, []byte(enc.B64.EncodeToString(keys.Private))); err != nil {
		return ErrJoinSecret
	}
	if err := writeSecret(cfg.SignFile, []byte(enc.B64.EncodeToString(signKey.Seed()))); err != nil {
		return ErrJoinSecret
	}
	req := tunnel.JoinRequest{Node: cfg.Node, Noise: enc.Bytes(keys.Public),
		Sign: enc.Bytes(signKey.Public().(ed25519.PublicKey)), TLS: enc.Bytes(cfg.TLS), URL: cfg.URL}
	req.MAC = enc.Bytes(tunnel.JoinMAC(code.Secret, req.Node, req.Noise, req.Sign, req.TLS, code.Key, req.URL))
	base, err := httpBase(code.URL)
	if err != nil {
		return err
	}
	if err := postRequest(ctx, client, base, code.Secret, req); err != nil {
		return err
	}
	// What the admin compares. The fingerprint covers exactly the values under
	// the MAC, so a request changed on the way shows another one.
	fmt.Fprintf(out, "Fingerabdruck: %s\n", tunnel.JoinFingerprint(req.Node, req.Noise, req.Sign, req.TLS, req.URL))
	fmt.Fprintf(out, "Vertrauensliste: Fassung %d, %s\n", code.Anchor.Seq, tunnel.Fingerprint(code.Anchor.Hash))
	shared, err := waitForKeys(ctx, cfg, client, base, keys, code)
	if err != nil {
		return err
	}
	body, err := json.Marshal(shared)
	if err != nil {
		return err
	}
	path := cfg.SecretFile
	if path == "" {
		path = SecretFile
	}
	if err := writeSecret(path, body); err != nil {
		return ErrJoinSecret
	}
	return nil
}

// makeKeys makes this server's Noise and signing key pairs.
func makeKeys() (noise.DHKey, ed25519.PrivateKey, error) {
	keys, err := noisep.Keypair(rand.Reader)
	if err != nil {
		return noise.DHKey{}, nil, err
	}
	_, signKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return noise.DHKey{}, nil, err
	}
	return keys, signKey, nil
}

// postRequest sends the join request. The API answers 204 or a code, and
// nothing of the secret comes back, so only the status is read.
func postRequest(ctx context.Context, client *http.Client, base string, secret []byte, req tunnel.JoinRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hash := secretHash(secret)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+tunnel.PathJoin+"?secret="+url.QueryEscape(hash), strings.NewReader(string(body)))
	if err != nil {
		// Only an address no URL can be gets here, and the request was never
		// built. Its error would quote that address with the query in it.
		return ErrJoinInput
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(r)
	if err != nil {
		return category(err)
	}
	defer resp.Body.Close()
	// The answer carries nothing this side needs; it is read away so the
	// connection can be reused, and bounded so a talkative server cannot fill
	// memory.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusNoContent {
		return ErrJoinRefused
	}
	return nil
}

// waitForKeys tries PathKeys until an admin has listed this server, the
// context ends, or Timeout runs out. Every failure is one pause and another
// try: before the signature a refusal is the normal state, and this side
// cannot tell it from a server that is briefly away.
func waitForKeys(ctx context.Context, cfg JoinConfig, client *http.Client, base string,
	keys noise.DHKey, code tunnel.JoinCode) (sharedKeys, error) {
	retry := cfg.Retry
	if retry <= 0 {
		retry = KeysRetry
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = JoinTimeout
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		shared, err := fetchKeys(wctx, client, base, keys, code.Key)
		if err == nil {
			return shared, nil
		}
		select {
		case <-wctx.Done():
			// The caller's own context ending is its business; our own
			// deadline is the message the admin reads.
			if ctx.Err() != nil {
				return sharedKeys{}, ctx.Err()
			}
			return sharedKeys{}, ErrJoinTimeout
		case <-time.After(retry):
		}
	}
}

// fetchKeys dials PathKeys once and reads the one frame with the two keys.
func fetchKeys(ctx context.Context, client *http.Client, base string, keys noise.DHKey,
	apiKey []byte) (sharedKeys, error) {
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+PathKeys+
		"?key="+url.QueryEscape(tunnel.Fingerprint(keys.Public)),
		&websocket.DialOptions{Subprotocols: []string{tunnel.Subprotocol}, HTTPClient: client})
	if err != nil {
		// waitForKeys swallows this and tries again, so it reaches nobody
		// today. It is turned into a category all the same: the error of the
		// dial carries the address and the query, and the next hand that
		// returns it instead of retrying must not have to notice that.
		return sharedKeys{}, category(err)
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(wire.MaxFrame + noisep.TagSize)
	hs, err := noisep.Handshake(true, []byte(PrologueKeys), keys, apiKey, rand.Reader)
	if err != nil {
		return sharedKeys{}, err
	}
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return sharedKeys{}, err
	}
	if err := ws.Write(ctx, websocket.MessageBinary, msg1); err != nil {
		return sharedKeys{}, err
	}
	kind, msg2, err := ws.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || len(msg2) != wire.HandshakeSize {
		return sharedKeys{}, errHandshake
	}
	_, cs1, cs2, err := hs.ReadMessage(nil, msg2)
	if err != nil || cs1 == nil || cs2 == nil {
		return sharedKeys{}, errHandshake
	}
	tr := noisep.NewTransport(true, cs1, cs2)
	kind, msg, err := ws.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		return sharedKeys{}, errHandshake
	}
	raw, err := tr.Open(msg)
	if err != nil {
		return sharedKeys{}, errHandshake
	}
	f, err := wire.Decode(raw)
	if err != nil || f.Type != wire.TypeData || f.Channel != wire.ControlChannel {
		return sharedKeys{}, errHandshake
	}
	var shared sharedKeys
	if json.Unmarshal(f.Payload, &shared) != nil || len(shared.SecretKey) != 32 || len(shared.ChallengeKey) != 32 {
		return sharedKeys{}, errHandshake
	}
	return shared, nil
}

// category turns what the transport came back with into one of the
// categories above and lets the error itself fall. It reads the SORT of the
// failure, not its text: the text carries the address and the query.
func category(err error) error {
	switch {
	case errors.Is(err, ErrJoinWrongAPI):
		return ErrJoinWrongAPI
	case errors.Is(err, context.DeadlineExceeded):
		return ErrJoinTooSlow
	}
	// http.Client.Timeout and a deadline on the socket both answer here, and
	// neither is context.DeadlineExceeded.
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrJoinTooSlow
	}
	return ErrJoinNoAnswer
}

// errHandshake is the one word for every way the key exchange can end badly:
// this side never says how far it got.
var errHandshake = errors.New("join: the key exchange did not finish")

// secretHash names which one-time value the API is to spend: the base64url of
// the SHA-256 of the secret. The secret itself stays here.
func secretHash(secret []byte) string {
	h := sha256.Sum256(secret)
	return enc.B64.EncodeToString(h[:])
}

// httpBase turns the tunnel address of the issuing API into the base of its
// HTTPS address: the same host and port, without the tunnel path.
func httpBase(tunnelURL string) (string, error) {
	u, err := url.Parse(tunnelURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("join: the code names no usable address")
	}
	return "https://" + u.Host, nil
}

// writeSecret writes b to path with 0600, through a neighbouring file and a
// rename, so a crash never leaves half a key behind. The file is created with
// the mode from the start: a chmod afterwards would leave a readable moment.
// The mode is what the system makes of it -- on Windows there is no such
// mode, and the case that measures it is skipped there.
func writeSecret(path string, b []byte) error {
	if path == "" {
		return fmt.Errorf("join: no file named for a secret")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
