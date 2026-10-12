package mariadb

import (
	"crypto/tls"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// TestCAWorksWithTLSConfig makes the certificates the way ngc-db.sh does and
// proves that config.DB.TLSConfig accepts exactly them: the same CA, the
// node's own host name, TLS 1.3. If openssl builds something the Go side
// refuses, this fails here and not on the servers.
func TestCAWorksWithTLSConfig(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl fehlt")
	}
	dir := t.TempDir()
	cnf := filepath.Join(dir, "db-ca.cnf")
	body, err := os.ReadFile("../../templates/db-ca.cnf")
	if err != nil {
		t.Fatal(err)
	}
	const host = "db-1.ngc.test"
	if err := os.WriteFile(cnf, []byte(strings.ReplaceAll(string(body), "@HOST@", host)), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("openssl", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("openssl %v: %v\n%s", args, err, out)
		}
	}
	// The CA: Ed25519, as everywhere else in NGC.
	run("genpkey", "-algorithm", "ed25519", "-out", "ca.key")
	run("req", "-new", "-x509", "-key", "ca.key", "-out", "ca.crt", "-days", "7300",
		"-config", cnf, "-section", "ca_dn", "-extensions", "ca_ext")
	// The node makes its own key and only hands over the request.
	run("genpkey", "-algorithm", "ed25519", "-out", "node.key")
	run("req", "-new", "-key", "node.key", "-out", "node.csr", "-config", cnf, "-section", "node_dn")
	run("x509", "-req", "-in", "node.csr", "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial",
		"-out", "node.crt", "-days", "3650", "-extfile", cnf, "-extensions", "node_ext")

	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	db := config.DB{Driver: config.DriverMariaDB, Nodes: []string{host + ":3306"}, Name: "ngc",
		User: "ngc", Password: config.NewSecretText("x"), CA: string(caPEM)}
	if err := db.Validate(); err != nil {
		t.Fatalf("config refuses the CA of the template: %v", err)
	}
	clientCfg, err := db.TLSConfig(host)
	if err != nil {
		t.Fatal(err)
	}
	// A real handshake against a server that shows the node certificate.
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "node.crt"), filepath.Join(dir, "node.key"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// Accept in a loop until Close ends it. With a single Accept the second
	// dial below would hang: nothing would answer its ClientHello, and the
	// client checks the name only after it has seen the certificate.
	// Measured: with one Accept the case does not fall, it never returns.
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			c.(*tls.Conn).Handshake()
			c.Close()
		}
	}()
	dialer := &net.Dialer{}
	conn, err := tls.DialWithDialer(dialer, "tcp", listener.Addr().String(), clientCfg)
	if err != nil {
		t.Fatalf("the certificate of the node is refused: %v", err)
	}
	conn.Close()

	// The same certificate under another name must not be accepted.
	wrong, err := db.TLSConfig("db-2.ngc.test")
	if err != nil {
		t.Fatal(err)
	}
	conn, err = tls.DialWithDialer(dialer, "tcp", listener.Addr().String(), wrong)
	if err == nil {
		conn.Close()
		t.Error("a certificate for another host was accepted")
	} else if ve := new(tls.CertificateVerificationError); !errors.As(err, &ve) {
		// Not just "it failed": it has to fail on the certificate. A refusal
		// for any other reason would let this case pass while the name check
		// was gone.
		t.Errorf("refused, but not for the certificate: %v", err)
	}
}
