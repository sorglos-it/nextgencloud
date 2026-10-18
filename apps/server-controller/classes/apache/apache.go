// Package apache keeps the two files Apache reads about the API servers in
// step with the trust list: the balancer it proxies /api/ to, and the
// certificates it accepts there.
//
// Everything that reaches those files has passed the grammar of package trust
// first (trust.ValidWebURL). Apache reads its configuration as lines of
// directives, so an address carrying a quote, a space or a line break would
// be a directive of its own; the grammar lets none of those through, and
// Render refuses the whole file rather than leaving one entry out.
package apache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/templates"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

var (
	// ErrForm says nothing was written because an address did not pass the
	// grammar of package trust, or because no API was left to balance over.
	// An empty balancer is not a smaller configuration but one Apache
	// refuses, so the previous file stays.
	ErrForm = errors.New("apache: no address for the balancer")
	// ErrPin says a certificate does not belong to the key the trust list
	// pins. It did not reach the trust file and Apache was not reloaded.
	ErrPin = errors.New("apache: certificate does not match its pin")
	// ErrApache says Apache refused the new configuration or could not be
	// reloaded; the previous files stand again.
	ErrApache = errors.New("apache: configtest or reload failed")
	// ErrFile says a file could not be read or written.
	ErrFile = errors.New("apache: could not write Apache's files")
)

// apachectl is the program that checks and reloads Apache. It is started
// without a shell and with its arguments as a list.
const apachectl = "/usr/sbin/apachectl"

// maxAPIs bounds the balancer, as config.Web.Validate bounds the list in
// web.json. A longer list is a mistake and not a bigger installation, and the
// previous file is the safer answer to it.
const maxAPIs = 16

// Options tells a Writer where the files are and how to reach the API servers
// and Apache. The three functions are there to be replaced in tests; left
// empty, each does the real thing.
type Options struct {
	// WebConfig is web.json (config.SaveWeb). Only its list of APIs belongs
	// to this package; the passkey settings beside it are kept as they are.
	WebConfig string
	// TrustFile holds the certificates Apache accepts (config.APITrustFile).
	TrustFile string
	// BalancerFile is Apache's balancer (config.BalancerFile).
	BalancerFile string
	// Fetch returns the certificate node serves, in DER. The default dials
	// the node over TLS pinned to its key (tunnel.PinnedTLS).
	Fetch func(ctx context.Context, node trust.Node) ([]byte, error)
	// ConfigTest returns an error if Apache would not start with the files as
	// they are now (apachectl configtest).
	ConfigTest func(ctx context.Context) error
	// Reload hands Apache its new configuration without dropping a request
	// (apachectl graceful).
	Reload func(ctx context.Context) error
}

// Writer writes the files Apache reads. One Writer belongs to one
// installation; it holds no state of its own, so the files on disk are the
// only truth about what Apache was last told.
type Writer struct{ o Options }

// New returns a Writer for these options.
func New(o Options) *Writer {
	if o.Fetch == nil {
		o.Fetch = fetchCert
	}
	if o.ConfigTest == nil {
		o.ConfigTest = func(ctx context.Context) error { return run(ctx, "configtest") }
	}
	if o.Reload == nil {
		o.Reload = func(ctx context.Context) error { return run(ctx, "graceful") }
	}
	return &Writer{o: o}
}

// balancer is what the template is filled with. The two paths come from
// package config rather than from the template, so the file Apache reads and
// the file this program writes cannot name two different places.
type balancer struct {
	BalancerFile string
	TrustFile    string
	APIs         []config.WebAPI
}

var balancerTmpl = template.Must(template.New("apache-apis.conf").Parse(templates.ApacheAPIs))

// Render builds the balancer file for apis. It returns nil, and so writes
// nothing at all, if the list is empty or if one entry is not exactly an
// address of the grammar with a 32-byte pin: a single unchecked byte in this
// file is a directive of Apache's, and half a balancer is worse than the one
// that stands.
func Render(apis []config.WebAPI) []byte {
	if len(apis) == 0 || len(apis) > maxAPIs {
		return nil
	}
	for _, a := range apis {
		if !trust.ValidWebURL(a.URL) || len(a.TLS) != sha256.Size {
			return nil
		}
	}
	var out bytes.Buffer
	if err := balancerTmpl.Execute(&out, balancer{
		BalancerFile: config.BalancerFile,
		TrustFile:    config.APITrustFile,
		APIs:         apis,
	}); err != nil {
		return nil
	}
	return out.Bytes()
}

// Sync brings both files in line with the API servers of the trust list: it
// runs whenever those change. Every certificate is checked against the pin of
// its node before it is written, Apache is reloaded only after a configtest,
// and a failing configtest puts the previous files back. changed says whether
// anything about the files was different from what already stood there.
//
// A SERVER THAT IS AWAY DOES NOT HOLD THE OTHERS UP, and that is a measured
// decision, not a convenience. This used to answer the first Fetch that failed
// and write nothing at all, so one API that was down froze what Apache knew
// for the whole installation — exactly at the moment the list had changed
// because a server had moved or a new one had joined. Measured on 18.10.2026
// at a two-machine installation: `WARN Apache could not be brought in line
// with the trust list err="dial tcp …:7443: connect: no route to host"`, twice,
// and the balancer kept naming a server that was not there any more.
//
// What it costs to leave such a node OUT is small and measured at the same
// installation: Apache answers over the members that are left (30 of 30 right
// answers with one dead member in the file), marks a member it cannot reach as
// failed for retry=5 seconds (AH00959) and pays about 3 s once per window. What
// it costs to leave a WRONG one IN is not small: a member whose certificate
// does not match its address makes Apache answer the client 500
// (AH00898/AH01097, measured 1 in 24 and 1 in 40 requests) and that worker is
// not marked failed, so every turn of lbmethod=byrequests hits it again.
//
// A node left out comes back by itself: node.watchWeb runs Sync again on every
// change of the list and once a day, so a server that is reachable with the
// right pin is written back then. The errors of the nodes that were left out
// come back joined, so the log names every one of them, and the answer is
// changed AND an error: the files were written, and something about the list
// could not be.
func (w *Writer) Sync(ctx context.Context, nodes []trust.Node) (bool, error) {
	var (
		apis  []config.WebAPI
		certs [][]byte
		errs  []error
	)
	for _, n := range nodes {
		if n.Kind != trust.NodeAPI {
			continue
		}
		// A node of the list that is not a well-formed API node is not
		// skipped quietly: the list is signed, so this is a mistake
		// somewhere else, and Apache keeps what it has until it is fixed.
		if !trust.ValidAPINode(n) {
			return false, fmt.Errorf("%w: node %q", ErrForm, n.ID)
		}
		url, ok := trust.WebURL(n.URL)
		if !ok || !trust.ValidWebURL(url) {
			return false, fmt.Errorf("%w: node %q", ErrForm, n.ID)
		}
		der, err := w.o.Fetch(ctx, n)
		if err != nil {
			errs = append(errs, fmt.Errorf("node %q: %w", n.ID, err))
			continue
		}
		if err := checkPin(der, n.TLS); err != nil {
			// The sharper of the two: something answers at that address and it
			// is not the server the list names. It is left out for the same
			// reason it is named loudly — a pin that does not match is the one
			// thing a balancer must not carry.
			errs = append(errs, fmt.Errorf("node %q: %w", n.ID, err))
			continue
		}
		apis = append(apis, config.WebAPI{URL: url, TLS: n.TLS})
		certs = append(certs, der)
	}
	conf := Render(apis)
	if conf == nil {
		// Not one API could be written. A balancer without a member makes
		// Apache answer every /api/ with 503, so what it has keeps working and
		// the reasons go back to the caller.
		return false, errors.Join(append(errs, ErrForm)...)
	}
	changed, err := w.apply(ctx, bundle(certs), conf, apis)
	if err != nil {
		return changed, err
	}
	return changed, errors.Join(errs...)
}

// Compare checks the certificate one API serves against the one in the trust
// file: it runs whenever a tunnel to that API is built and once a day. A
// renewed certificate carries the same key - that is what the pin proves - so
// only the file needs rewriting, and Apache locks nobody out over an expiry
// date. A node whose key is not in the file at all is left to Sync, which
// owns the list.
func (w *Writer) Compare(ctx context.Context, node trust.Node) (bool, error) {
	if node.Kind != trust.NodeAPI || !trust.ValidAPINode(node) {
		return false, fmt.Errorf("%w: node %q", ErrForm, node.ID)
	}
	der, err := w.o.Fetch(ctx, node)
	if err != nil {
		return false, err
	}
	if err := checkPin(der, node.TLS); err != nil {
		return false, fmt.Errorf("%w: node %q", err, node.ID)
	}
	old, err := os.ReadFile(w.o.TrustFile)
	if err != nil {
		return false, errors.Join(ErrFile, err)
	}
	certs, found, same := replaceCert(old, der, node.TLS)
	if !found || same {
		return false, nil
	}
	conf, err := os.ReadFile(w.o.BalancerFile)
	if err != nil {
		return false, errors.Join(ErrFile, err)
	}
	// The addresses do not change here, so web.json keeps its list: nil says
	// apply must not touch it.
	return w.apply(ctx, bundle(certs), conf, nil)
}

// apply puts the new files in place and tells Apache, or leaves everything as
// it was - as long as the fault is a write or ConfigTest. A failed Reload is
// the one exception, and it takes nothing back: the new files stay where they
// are while Apache goes on serving the old ones. Writer.Sync says it
// precisely. The trust file goes first: Apache has not been told yet, so a
// certificate too many there changes nothing, while a balancer naming a
// server whose certificate is still missing would.
func (w *Writer) apply(ctx context.Context, trustPEM, conf []byte, apis []config.WebAPI) (bool, error) {
	oldTrust, trustThere := read(w.o.TrustFile)
	oldConf, confThere := read(w.o.BalancerFile)
	if trustThere && confThere && bytes.Equal(oldTrust, trustPEM) && bytes.Equal(oldConf, conf) {
		return false, nil
	}
	if err := writeAtomic(w.o.TrustFile, trustPEM); err != nil {
		return false, err
	}
	if err := writeAtomic(w.o.BalancerFile, conf); err != nil {
		w.restore(oldTrust, trustThere, oldConf, confThere)
		return false, err
	}
	if err := w.o.ConfigTest(ctx); err != nil {
		w.restore(oldTrust, trustThere, oldConf, confThere)
		return false, errors.Join(ErrApache, err)
	}
	// web.json only after Apache has agreed to the files it reads itself, so
	// the web interface never names a server Apache would refuse to reach.
	if apis != nil {
		if err := w.saveWeb(apis); err != nil {
			w.restore(oldTrust, trustThere, oldConf, confThere)
			return false, err
		}
	}
	if err := w.o.Reload(ctx); err != nil {
		return false, errors.Join(ErrApache, err)
	}
	return true, nil
}

// saveWeb replaces the list of APIs in web.json and keeps everything else in
// it, because the passkey settings there belong to the web interface.
func (w *Writer) saveWeb(apis []config.WebAPI) error {
	web, err := config.LoadWeb(w.o.WebConfig)
	if err != nil {
		// No file yet, or one this version cannot read: the API list is all
		// this program knows, and writing it is better than stopping here.
		web = &config.Web{}
	}
	web.APIs = apis
	if err := config.SaveWeb(w.o.WebConfig, web); err != nil {
		return errors.Join(ErrFile, err)
	}
	return nil
}

// restore puts both files back as they were. A file that was not there before
// is removed again.
func (w *Writer) restore(trustPEM []byte, trustThere bool, conf []byte, confThere bool) {
	for _, f := range []struct {
		path  string
		body  []byte
		there bool
	}{
		{w.o.TrustFile, trustPEM, trustThere},
		{w.o.BalancerFile, conf, confThere},
	} {
		if !f.there {
			_ = os.Remove(f.path)
			continue
		}
		_ = writeAtomic(f.path, f.body)
	}
}

// checkPin reports whether der is a certificate for the key this pin names.
// Trust hangs on the key, never on the certificate: trust.TLSPin of its
// public key info is the whole comparison.
// A pin of the wrong length needs no check of its own: bytes.Equal below is
// false for it, as it is for any other key.
func checkPin(der []byte, pin enc.Bytes) error {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return errors.Join(ErrPin, err)
	}
	if !bytes.Equal(trust.TLSPin(cert.RawSubjectPublicKeyInfo), pin) {
		return ErrPin
	}
	return nil
}

// bundle writes the certificates as the PEM file Apache reads.
func bundle(certs [][]byte) []byte {
	var out bytes.Buffer
	for _, der := range certs {
		// pem.Encode fails only on a writer that errors, which a buffer does
		// not.
		_ = pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	return out.Bytes()
}

// replaceCert puts der in the place of the certificate of the same key in the
// PEM file old. found says whether a certificate for that key was in there,
// same whether it was already this one. Anything in the file that is not a
// certificate is dropped, so a hand-edited file comes back as this program
// writes it.
func replaceCert(old, der []byte, pin enc.Bytes) (certs [][]byte, found, same bool) {
	rest := old
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return certs, found, same
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if checkPin(block.Bytes, pin) == nil {
			found = true
			if bytes.Equal(block.Bytes, der) {
				same = true
			}
			certs = append(certs, der)
			continue
		}
		certs = append(certs, block.Bytes)
	}
}

// read gives the contents of a file and whether it was there.
func read(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	return b, err == nil
}

// writeAtomic replaces a file completely or leaves it as it was: Apache reads
// these files while this program writes them, and half a line of either would
// stop it from starting. Both are world readable by design - Apache reads
// them as its own user and neither holds a secret.
func writeAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".ngc-")
	if err != nil {
		return errors.Join(ErrFile, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return errors.Join(ErrFile, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return errors.Join(ErrFile, err)
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(ErrFile, err)
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(ErrFile, err)
	}
	return nil
}

// fetchCert reads the certificate an API serves. The connection is pinned to
// the key of the trust list (tunnel.PinnedTLS), so this never fetches the
// certificate of whoever answers at that address.
func fetchCert(ctx context.Context, node trust.Node) ([]byte, error) {
	url, ok := trust.WebURL(node.URL)
	if !ok {
		return nil, fmt.Errorf("%w: node %q", ErrForm, node.ID)
	}
	addr, ok := strings.CutPrefix(url, "https://")
	if !ok {
		return nil, fmt.Errorf("%w: node %q", ErrForm, node.ID)
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: node %q", ErrForm, node.ID)
	}
	cfg := tunnel.PinnedTLS(node.TLS)
	// The name is sent so the server can pick its certificate; the pin, not
	// the name, decides whether this connection is trusted.
	cfg.ServerName = host
	d := tls.Dialer{Config: cfg}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, ErrPin
	}
	return state.PeerCertificates[0].Raw, nil
}

// run starts apachectl with one argument and no shell.
func run(ctx context.Context, arg string) error {
	out, err := exec.CommandContext(ctx, apachectl, arg).CombinedOutput()
	if err != nil {
		// Apache names the file and the line of a syntax error, which is the
		// one thing that helps here, and it is no secret.
		return fmt.Errorf("%s %s: %w: %s", apachectl, arg, err, bytes.TrimSpace(out))
	}
	return nil
}
