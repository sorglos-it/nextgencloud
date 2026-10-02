package mariadb

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/sorglos-it/nextgencloud/shared/store"

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
// never lose: TLS against the CA with this node's own name, no fallback to
// plain text, no interpolation, no multiple statements, no local files, the
// password, and ClientFoundRows, which sqlstore.put and LockTake rest on.
//
// It runs over three nodes of three different names - one of them an IPv6
// address, whose brackets config.DB.TLSConfig does not want - and over a
// database and a user that are not called "ngc", so that a hard-wired
// constant in nodeConfig cannot pass as the value that was configured.
func TestConnectorPinsTheSecureSettings(t *testing.T) {
	const password = "not-the-real-one"
	cfg := config.DB{Driver: config.DriverMariaDB,
		Nodes: []string{"db-1.ngc.test:3306", "db-zwei.example:3307", "[2001:db8::5]:3306"},
		Name:  "ngc_other", User: "ngc_reader", Password: config.NewSecretText(password),
		CA: testCA(t)}
	// The server name of each node, as config.DB.TLSConfig asks for it: an
	// IPv6 address without its brackets.
	want := map[string]string{
		"db-1.ngc.test:3306":   "db-1.ngc.test",
		"db-zwei.example:3307": "db-zwei.example",
		"[2001:db8::5]:3306":   "2001:db8::5",
	}
	for _, node := range cfg.Nodes {
		mc, err := nodeConfig(cfg, node)
		if err != nil {
			t.Fatalf("nodeConfig(%q): %v", node, err)
		}
		if mc.TLS == nil {
			t.Errorf("%s: no TLS configuration", node)
		} else {
			if mc.TLS.ServerName != want[node] {
				t.Errorf("%s: server name %q, want %q", node, mc.TLS.ServerName, want[node])
			}
			if mc.TLS.MinVersion != tls.VersionTLS13 {
				t.Errorf("%s: minimum TLS version %#x, want TLS 1.3", node, mc.TLS.MinVersion)
			}
			if mc.TLS.InsecureSkipVerify {
				t.Errorf("%s: the certificate is not checked", node)
			}
			if mc.TLS.RootCAs == nil {
				t.Errorf("%s: no CA to check against", node)
			}
		}
		if mc.Addr != node {
			t.Errorf("address %q, want %q", mc.Addr, node)
		}
		// The password has to arrive, or the installation cannot log in on
		// its first start. The value itself is never printed.
		if mc.Passwd != password {
			t.Errorf("%s: the password of the configuration did not arrive", node)
		}
		if mc.User != "ngc_reader" {
			t.Errorf("%s: user %q", node, mc.User)
		}
		if mc.DBName != "ngc_other" {
			t.Errorf("%s: database %q", node, mc.DBName)
		}
		if mc.Net != "tcp" {
			t.Errorf("%s: net %q", node, mc.Net)
		}
		if mc.AllowFallbackToPlaintext {
			t.Errorf("%s: a connection may fall back to plain text", node)
		}
		if !mc.ClientFoundRows {
			t.Errorf("%s: ClientFoundRows is off: writing the same row again looks like no such row", node)
		}
		if mc.InterpolateParams {
			t.Errorf("%s: the values are put into the statement", node)
		}
		if mc.MultiStatements {
			t.Errorf("%s: several statements in one query are allowed", node)
		}
		if mc.AllowAllFiles || mc.AllowCleartextPasswords || mc.AllowOldPasswords {
			t.Errorf("%s: a weakening option is on", node)
		}
		if !mc.CheckConnLiveness {
			t.Errorf("%s: a dead connection is handed out", node)
		}
		if mc.ParseTime {
			t.Errorf("%s: timestamps are parsed although they are numbers", node)
		}
		if mc.Loc != time.UTC {
			t.Errorf("%s: location %v", node, mc.Loc)
		}
		// The numbers are written out here: comparing against the constant
		// the line is meant to hold would pass however that constant moves.
		if mc.MaxAllowedPacket != 128<<20 {
			t.Errorf("%s: max_allowed_packet %d, want %d", node, mc.MaxAllowedPacket, 128<<20)
		}
		if mc.Timeout != 5*time.Second {
			t.Errorf("%s: dial timeout %v, want 5s", node, mc.Timeout)
		}
		if mc.ReadTimeout != 60*time.Second || mc.WriteTimeout != 60*time.Second {
			t.Errorf("%s: read %v, write %v, want 60s each", node, mc.ReadTimeout, mc.WriteTimeout)
		}
		// The driver writes to stderr through its default logger, and a
		// driver message may stand next to a password.
		if _, ok := mc.Logger.(*mysql.NopLogger); !ok {
			t.Errorf("%s: the driver logs through %T", node, mc.Logger)
		}
		if mc.Params["wsrep_sync_wait"] != "1" {
			t.Errorf("%s: wsrep_sync_wait %q", node, mc.Params["wsrep_sync_wait"])
		}
		if mc.Params["sql_mode"] != "'STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION'" {
			t.Errorf("%s: sql_mode %q", node, mc.Params["sql_mode"])
		}
		if mc.Params["time_zone"] != "'+00:00'" {
			t.Errorf("%s: time_zone %q", node, mc.Params["time_zone"])
		}
		// The DSN is what the driver would send; no weakening option may show
		// up in it. The text itself is never printed, because it carries the
		// password.
		if dsn := mc.FormatDSN(); strings.Contains(dsn, "allowAllFiles=true") ||
			strings.Contains(dsn, "interpolateParams=true") ||
			strings.Contains(dsn, "tls=false") {
			t.Errorf("%s: the DSN carries a weakening option", node)
		}
	}
}

// TestMaxPacketMatchesTheServerTemplate ties the Go constant to the line of
// galera.cnf the comment beside it names. Both numbers are written out, and
// the template is read, so neither side can move alone. The blob bound of the
// contract is the floor: a canonical ui-tar has to fit through.
func TestMaxPacketMatchesTheServerTemplate(t *testing.T) {
	if maxPacket != 128<<20 {
		t.Errorf("maxPacket is %d, want %d", maxPacket, 128<<20)
	}
	if maxPacket <= store.MaxUITotal {
		t.Errorf("maxPacket %d is not above store.MaxUITotal %d", maxPacket, store.MaxUITotal)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "templates", "galera.cnf"))
	if err != nil {
		t.Fatalf("galera.cnf: %v", err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "max_allowed_packet="); ok {
			line = v
			break
		}
	}
	if line == "" {
		t.Fatal("galera.cnf names no max_allowed_packet")
	}
	// The template writes it in mebibytes, the Go side in bytes.
	mib, ok := strings.CutSuffix(line, "M")
	if !ok {
		t.Fatalf("max_allowed_packet=%s is no value in MiB", line)
	}
	n, err := strconv.Atoi(mib)
	if err != nil {
		t.Fatalf("max_allowed_packet=%s: %v", line, err)
	}
	if int64(n)<<20 != int64(maxPacket) {
		t.Errorf("galera.cnf says %d MiB, the driver %d bytes", n, maxPacket)
	}
}

// TestPoolLimits holds the four numbers of the connection pool as numbers,
// and proves for the one that database/sql hands back that it really reached
// the pool. The other three are write-only: that they arrive is NOT
// measurable from here.
func TestPoolLimits(t *testing.T) {
	if maxOpenConns != 16 || maxIdleConns != 4 ||
		connLifetime != 5*time.Minute || connIdleTime != time.Minute {
		t.Errorf("pool: %d open, %d idle, %v lifetime, %v idle time",
			maxOpenConns, maxIdleConns, connLifetime, connIdleTime)
	}
	cfg := config.DB{Driver: config.DriverMariaDB, Nodes: []string{"db-1.ngc.test:3306"},
		Name: "ngc", User: "ngc", Password: config.NewSecretText("x"), CA: testCA(t)}
	db, err := OpenDB(cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()
	if got := db.Stats().MaxOpenConnections; got != 16 {
		t.Errorf("the open pool allows %d connections, want 16", got)
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
