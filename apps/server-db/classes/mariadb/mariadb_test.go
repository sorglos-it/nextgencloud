package mariadb

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// TestOpenRefusesWithoutTLS: without a CA nothing opens. A connection without
// a checked certificate must be impossible to configure, not just unusual.
func TestOpenRefusesWithoutTLS(t *testing.T) {
	base := config.DB{Driver: config.DriverMariaDB, Nodes: []string{"db-1.ngc.test:3306"},
		Name: "ngc", User: "ngc", Password: config.NewSecretText("x")}
	if db, err := OpenDB(base); err == nil {
		db.Close()
		t.Error("a cluster without a CA was opened")
	}
	bad := base
	bad.CA = "-----BEGIN CERTIFICATE-----\nnonsense\n-----END CERTIFICATE-----\n"
	if db, err := OpenDB(bad); err == nil {
		db.Close()
		t.Error("a broken CA was accepted")
	}
	wrong := base
	wrong.Driver = config.DriverSQLite
	if _, err := OpenDB(wrong); err == nil || !strings.Contains(err.Error(), "driver") {
		t.Errorf("wrong driver: %v", err)
	}
}

// TestOpenRefusesWithoutANode: an empty node list is refused before anything
// is built, so no connection ever goes to a node nobody named.
func TestOpenRefusesWithoutANode(t *testing.T) {
	cfg := config.DB{Driver: config.DriverMariaDB, Name: "ngc", User: "ngc",
		Password: config.NewSecretText("x"), CA: testCA(t)}
	if db, err := OpenDB(cfg); err == nil {
		db.Close()
		t.Fatal("a cluster without a node was opened")
	} else if !strings.Contains(err.Error(), "no node") {
		t.Fatalf("without a node: %v", err)
	}
}

// TestConnectorRefusesAnAddressThatIsNoHostPort: every node address passes the
// grammar of package trust before it reaches the driver's DSN, so nothing that
// could carry a parameter of its own gets there.
func TestConnectorRefusesAnAddressThatIsNoHostPort(t *testing.T) {
	cfg := config.DB{Driver: config.DriverMariaDB, Name: "ngc", User: "ngc",
		Password: config.NewSecretText("x"), CA: testCA(t)}
	for _, node := range []string{
		"db-1.ngc.test",                         // no port
		"db-1.ngc.test:3306?tls=false",          // a parameter behind the port
		"db-1.ngc.test:3306/other",              // another database behind it
		"db-1.ngc.test:0",                       // no port number
		"db-1.ngc.test:3306,db-2.ngc.test:3306", // two of them in one entry
		"",
	} {
		cfg.Nodes = []string{node}
		if db, err := OpenDB(cfg); err == nil {
			db.Close()
			t.Errorf("%q was accepted as a node", node)
		}
	}
}

// TestConnectorPinsTheSecureSettings holds the settings that a connection must
// never lose: TLS against the CA with this node's name, no fallback to plain
// text, no interpolation, no multiple statements, no local files, and
// ClientFoundRows, which sqlstore.put and LockTake rest on.
func TestConnectorPinsTheSecureSettings(t *testing.T) {
	cfg := config.DB{Driver: config.DriverMariaDB, Nodes: []string{"db-1.ngc.test:3306"},
		Name: "ngc", User: "ngc", Password: config.NewSecretText("x"), CA: testCA(t)}
	mc, err := nodeConfig(cfg, cfg.Nodes[0])
	if err != nil {
		t.Fatalf("nodeConfig: %v", err)
	}
	if mc.TLS == nil {
		t.Error("no TLS configuration")
	} else {
		if mc.TLS.ServerName != "db-1.ngc.test" {
			t.Errorf("server name %q", mc.TLS.ServerName)
		}
		if mc.TLS.MinVersion < 0x0304 { // TLS 1.3
			t.Errorf("minimum TLS version %#x", mc.TLS.MinVersion)
		}
		if mc.TLS.InsecureSkipVerify {
			t.Error("the certificate is not checked")
		}
	}
	if mc.AllowFallbackToPlaintext {
		t.Error("a connection may fall back to plain text")
	}
	if !mc.ClientFoundRows {
		t.Error("ClientFoundRows is off: writing the same row again looks like no such row")
	}
	if mc.InterpolateParams {
		t.Error("the values are put into the statement")
	}
	if mc.MultiStatements {
		t.Error("several statements in one query are allowed")
	}
	if mc.AllowAllFiles || mc.AllowCleartextPasswords || mc.AllowOldPasswords {
		t.Error("a weakening option is on")
	}
	if !mc.CheckConnLiveness {
		t.Error("a dead connection is handed out")
	}
	if mc.ParseTime {
		t.Error("timestamps are parsed although they are numbers")
	}
	if mc.Loc != time.UTC {
		t.Errorf("location %v", mc.Loc)
	}
	if mc.MaxAllowedPacket != maxPacket {
		t.Errorf("max_allowed_packet %d, want %d", mc.MaxAllowedPacket, maxPacket)
	}
	if mc.Addr != "db-1.ngc.test:3306" || mc.Net != "tcp" || mc.DBName != "ngc" || mc.User != "ngc" {
		t.Errorf("address %q, net %q, database %q, user %q", mc.Addr, mc.Net, mc.DBName, mc.User)
	}
	if mc.Params["wsrep_sync_wait"] != "1" {
		t.Errorf("wsrep_sync_wait %q", mc.Params["wsrep_sync_wait"])
	}
	if mc.Params["sql_mode"] != "'STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'" {
		t.Errorf("sql_mode %q", mc.Params["sql_mode"])
	}
	if mc.Params["time_zone"] != "'+00:00'" {
		t.Errorf("time_zone %q", mc.Params["time_zone"])
	}
	// The DSN is what the driver would send; neither the password nor a
	// weakening option may show up in it.
	if dsn := mc.FormatDSN(); strings.Contains(dsn, "allowAllFiles=true") ||
		strings.Contains(dsn, "interpolateParams=true") {
		t.Errorf("DSN: %s", dsn)
	}
}

// testCA makes a self-signed CA certificate in PEM, the shape config.DB.CA
// takes. The private key is thrown away: nothing here connects, only the
// configuration is read.
func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ngc-test-ca"},
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
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
