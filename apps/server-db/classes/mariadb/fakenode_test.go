package mariadb

// A node of the cluster, spoken to over the real wire. This machine has no
// MariaDB and no Galera, and task 9 could therefore only hold the
// configuration against the numbers it was meant to carry - a test over a
// string, not over a behaviour. What is measurable without a server is
// everything the client does: that it insists on TLS, that it checks the
// certificate against the CA of the installation and against the name of the
// node, which node a new connection goes to, what it does when one node
// fails, which session variables it sends, which capabilities it asks for,
// and what the connection pool closes.
//
// What is NOT measurable here, and is marked as such in every place that
// touches it: whether a MariaDB server ACCEPTS wsrep_sync_wait, sql_mode,
// time_zone and max_allowed_packet (that needs a server), and whether
// ClientFoundRows really makes an UPDATE report matched instead of changed
// rows (that is the server's semantics, not the client's bit).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

// nodeMode is how a fake node misbehaves. nodeGood is a node that works.
type nodeMode int

const (
	nodeGood          nodeMode = iota
	nodeNoTLS                  // greets without CLIENT_SSL: a server that only speaks plain text
	nodeHandshakeDies          // accepts the connection and closes it without a greeting
	nodeSilent                 // accepts and never answers at all
	nodeNonPrimary             // greets and authenticates, then refuses with Galera's 1047
)

// The capability bits of the MySQL 10 handshake this test needs, by their
// position in the bitmask. They are written out instead of imported because
// the driver keeps its own copies private, and a test that read the driver's
// constants could not notice the driver changing them.
const (
	capMySQL         = 1 << 0
	capFoundRows     = 1 << 1
	capLongFlag      = 1 << 2
	capConnectWithDB = 1 << 3
	capLocalFiles    = 1 << 7
	capProtocol41    = 1 << 9
	capSSL           = 1 << 11
	capTransactions  = 1 << 13
	capSecureConn    = 1 << 15
	capMultiResults  = 1 << 17
	capPluginAuth    = 1 << 19
	capConnectAttrs  = 1 << 20
	capLenEncData    = 1 << 21
	capDeprecateEOF  = 1 << 24
)

// serverCaps is what a fake node advertises. Without CLIENT_SSL it is the
// greeting of a server that cannot do TLS.
func serverCaps(withTLS bool) uint32 {
	caps := uint32(capMySQL | capFoundRows | capLongFlag | capConnectWithDB | capLocalFiles |
		capProtocol41 | capTransactions | capSecureConn | capMultiResults |
		capPluginAuth | capConnectAttrs | capLenEncData | capDeprecateEOF)
	if withTLS {
		caps |= capSSL
	}
	return caps
}

// fakeNode is one node: a listener on the loopback with an ephemeral port, so
// no fixed port of another session is ever touched.
type fakeNode struct {
	addr   string // host:port exactly as config.DB.Nodes spells it
	mode   nodeMode
	tlsCfg *tls.Config

	mu       sync.Mutex
	accepted int      // TCP connections accepted
	authOK   int      // connections that got past authentication
	sni      []string // the server name of every TLS handshake
	caps     []uint32 // the capability bitmask of every handshake response
	queries  []string // the text of every COM_QUERY
	ln       net.Listener
	stop     chan struct{}
	stopOnce sync.Once
}

// newFakeNode starts a node of the given mode, signed by ca. host is
// "127.0.0.1" or "::1"; certHosts and certNames are what the certificate is
// good for, which one test makes wrong on purpose.
func newFakeNode(t *testing.T, ca *testAuthority, mode nodeMode, host string, certHosts, certNames []string) *fakeNode {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Skipf("kein Listener auf %s moeglich: %v", host, err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("address of the listener: %v", err)
	}
	n := &fakeNode{
		// JoinHostPort brackets an IPv6 address, which is the spelling
		// config.DB.Nodes takes.
		addr: net.JoinHostPort(host, port),
		mode: mode,
		stop: make(chan struct{}),
		ln:   ln,
	}
	n.tlsCfg = &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			n.mu.Lock()
			n.sni = append(n.sni, chi.ServerName)
			n.mu.Unlock()
			return ca.leaf(t, certHosts, certNames), nil
		},
	}
	go n.serve()
	t.Cleanup(n.close)
	return n
}

func (n *fakeNode) close() {
	n.stopOnce.Do(func() {
		close(n.stop)
		_ = n.ln.Close()
	})
}

func (n *fakeNode) serve() {
	for {
		c, err := n.ln.Accept()
		if err != nil {
			return
		}
		n.mu.Lock()
		n.accepted++
		n.mu.Unlock()
		go n.handle(c)
	}
}

// handle speaks as much of the MySQL 10 protocol as database/sql needs to hand
// out a usable connection.
func (n *fakeNode) handle(c net.Conn) {
	defer c.Close()
	switch n.mode {
	case nodeHandshakeDies:
		return // no greeting, the socket just goes away
	case nodeSilent:
		// Never answer, and never close either: this is the node that hangs.
		go func() { <-n.stop; _ = c.Close() }()
		_, _ = io.Copy(io.Discard, c)
		return
	}
	withTLS := n.mode != nodeNoTLS
	if err := writePacket(c, 0, greeting(withTLS)); err != nil {
		return
	}
	var rw net.Conn = c
	body, seq, err := readPacket(rw)
	if err != nil {
		// The client gave up without answering, which is what nodeNoTLS is
		// measured on: it must not send its password in plain text.
		return
	}
	if withTLS {
		// The first answer is the SSLRequest; everything after it is TLS.
		tc := tls.Server(c, n.tlsCfg)
		if err := tc.Handshake(); err != nil {
			return
		}
		rw = tc
		if body, seq, err = readPacket(rw); err != nil {
			return
		}
	}
	if len(body) < 4 {
		return
	}
	n.mu.Lock()
	n.caps = append(n.caps, binary.LittleEndian.Uint32(body[:4]))
	n.authOK++
	n.mu.Unlock()
	if err := writePacket(rw, seq+1, okPacket()); err != nil {
		return
	}
	for {
		body, seq, err = readPacket(rw)
		if err != nil || len(body) == 0 {
			return
		}
		switch body[0] {
		case 0x01: // COM_QUIT
			return
		case 0x03: // COM_QUERY
			n.mu.Lock()
			n.queries = append(n.queries, string(body[1:]))
			n.mu.Unlock()
			if n.mode == nodeNonPrimary {
				// What a Galera node answers while it is not part of the
				// primary component. The number and the text are ours, not a
				// cluster's: that a real Galera says exactly this is NOT
				// measured here.
				_ = writePacket(rw, seq+1, errPacket(1047, "WSREP has not yet prepared node for application use"))
				return
			}
		}
		if err := writePacket(rw, seq+1, okPacket()); err != nil {
			return
		}
	}
}

// counts reads the recorded numbers under the lock.
func (n *fakeNode) counts() (accepted, authOK int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.accepted, n.authOK
}

func (n *fakeNode) allQueries() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.queries...)
}

func (n *fakeNode) allCaps() []uint32 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]uint32(nil), n.caps...)
}

// greeting is the initial handshake packet, protocol version 10, in the layout
// the driver reads: version, connection id, the two halves of the salt, the
// capabilities in two pieces, and the name of the authentication plugin.
func greeting(withTLS bool) []byte {
	caps := serverCaps(withTLS)
	var salt [20]byte
	for i := range salt {
		salt[i] = byte('a' + i%26)
	}
	b := []byte{10}
	b = append(b, "11.4.0-MariaDB-ngc-fake"...)
	b = append(b, 0)
	b = append(b, 1, 0, 0, 0) // connection id
	b = append(b, salt[:8]...)
	b = append(b, 0) // filler
	b = append(b, byte(caps), byte(caps>>8))
	b = append(b, 45)   // character set
	b = append(b, 2, 0) // status flags: autocommit
	b = append(b, byte(caps>>16), byte(caps>>24))
	b = append(b, 21)               // length of the authentication data
	b = append(b, 0, 0, 0, 0, 0, 0) // reserved
	b = append(b, 0, 0, 0, 0)       // reserved, because CLIENT_MYSQL is set
	b = append(b, salt[8:20]...)    // second half, twelve bytes
	b = append(b, 0)                // terminates the salt
	b = append(b, "mysql_native_password"...)
	b = append(b, 0)
	return b
}

// okPacket is an OK with no affected rows: autocommit on, no warning.
func okPacket() []byte { return []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00} }

// errPacket is an ERR packet with the SQL state marker the driver expects.
func errPacket(code uint16, msg string) []byte {
	b := []byte{0xff, byte(code), byte(code >> 8), '#', '0', '8', 'S', '0', '1'}
	return append(b, msg...)
}

func readPacket(r io.Reader) (body []byte, seq byte, err error) {
	var h [4]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return nil, 0, err
	}
	n := int(h[0]) | int(h[1])<<8 | int(h[2])<<16
	body = make([]byte, n)
	if _, err = io.ReadFull(r, body); err != nil {
		return nil, 0, err
	}
	return body, h[3], nil
}

func writePacket(w io.Writer, seq byte, body []byte) error {
	n := len(body)
	p := append([]byte{byte(n), byte(n >> 8), byte(n >> 16), seq}, body...)
	_, err := w.Write(p)
	return err
}

// testAuthority is a CA with its key kept, so it can sign the certificate of a
// node. mariadb_test.go's testCA throws the key away, because the tests there
// only read a configuration; here a real TLS handshake has to succeed.
type testAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func newAuthority(t *testing.T, name string) *testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return &testAuthority{cert: cert, key: key,
		pem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

// leaf signs a server certificate for the given addresses and names.
func (a *testAuthority) leaf(t *testing.T, hosts, names []string) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "ngc-test-node"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     names,
	}
	for _, h := range hosts {
		ip := net.ParseIP(h)
		if ip == nil {
			t.Fatalf("%q is no address", h)
		}
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
