package mariadb_test

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/mariadb"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/migrate"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/dbtest"
)

var ctx = context.Background()

// clusterConfig reads the access from the environment. Without it every test
// here skips itself, so tools/check.sh runs on a machine without a cluster.
func clusterConfig(t *testing.T, migrateAccount bool) config.DB {
	t.Helper()
	nodes := os.Getenv("NGC_DB_NODES")
	caFile := os.Getenv("NGC_DB_CA")
	if nodes == "" || caFile == "" {
		t.Skip("kein Verbund: NGC_DB_NODES und NGC_DB_CA fehlen (apps/server-db/config/dbtest.env.example)")
	}
	ca, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatalf("das Zertifikat der CA ist nicht lesbar: %v", err)
	}
	user, password := os.Getenv("NGC_DB_USER"), os.Getenv("NGC_DB_PASSWORD")
	if migrateAccount {
		user, password = os.Getenv("NGC_DB_MIGRATE_USER"), os.Getenv("NGC_DB_MIGRATE_PASSWORD")
	}
	return config.DB{
		Driver: config.DriverMariaDB, Nodes: strings.Split(nodes, ","),
		Name: os.Getenv("NGC_DB_NAME"), User: user,
		Password: config.NewSecretText(password), CA: string(ca),
	}
}

// fresh gives a subtest an empty schema. The migrate account applies the
// migrations once and then empties every table; no test creates a table.
func fresh(t *testing.T) dbapi.Store {
	t.Helper()
	admin, err := mariadb.OpenDB(clusterConfig(t, true))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := migrate.Apply(ctx, admin, sqlstore.MariaDB, "test", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	for _, table := range sqlstore.Tables {
		if table == "ngc_schema" {
			continue // the record of the migrations stays
		}
		if _, err := admin.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("%s leeren: %v", table, err)
		}
	}
	s, err := mariadb.Open(clusterConfig(t, false))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestConformance runs the contract's own tests against the cluster: the very
// same tests SQLite passes.
func TestConformance(t *testing.T) {
	// Asked once on the parent, so that without a cluster this case reports
	// SKIP instead of PASS over eleven skipped subtests. A PASS that holds no
	// contract at all is indistinguishable from a measurement without -v.
	clusterConfig(t, true)
	dbtest.Run(t, func(t *testing.T) dbapi.Store { return fresh(t) })
}

// TestAccountRights: the account of the API may read and write, nothing else.
// If it could create a table, an attack that got as far as a statement of its
// own could change the schema.
func TestAccountRights(t *testing.T) {
	db, err := mariadb.OpenDB(clusterConfig(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		"CREATE TABLE ngc_probe (id BIGINT NOT NULL, PRIMARY KEY (id))",
		"DROP TABLE ngc_blob",
		"GRANT SELECT ON ngc.* TO ngc@'%'",
	} {
		if _, err := db.ExecContext(ctx, stmt); err == nil {
			t.Errorf("the account of the API was allowed to: %s", stmt)
		}
	}
}

// TestFailover switches off the node this test runs on and checks that the
// store keeps working through the others. It needs root and only runs on
// DB 1, where the session sits.
func TestFailover(t *testing.T) {
	if os.Getenv("NGC_DB_FAILOVER") != "local" {
		t.Skip("kein Ausfalltest: NGC_DB_FAILOVER=local fehlt")
	}
	s := fresh(t)
	defer s.Close()
	write := func(n int) error {
		return s.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: strconv.Itoa(n), Seq: uint64(n)})
	}
	if err := write(1); err != nil {
		t.Fatal(err)
	}
	// systemctl is started with an argument list, never through a shell.
	run := func(action string) {
		t.Helper()
		out, err := exec.CommandContext(ctx, "systemctl", action, "mariadb").CombinedOutput()
		if err != nil {
			t.Fatalf("systemctl %s mariadb: %v\n%s", action, err, out)
		}
	}
	run("stop")
	defer run("start")
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for n := 2; time.Now().Before(deadline); n++ {
		if last = write(n); last == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if last != nil {
		t.Fatalf("nach dem Ausfall eines Knotens ging nichts mehr: %v", last)
	}
	// Reading works too, and the row written before the failure is there.
	if _, err := s.BlobGet(ctx, dbapi.BlobTrust, "1"); err != nil {
		t.Errorf("lesen nach dem Ausfall: %v", err)
	}
	run("start")
	// The node comes back and the store uses it again within one pause.
	time.Sleep(35 * time.Second)
	if err := write(999); err != nil {
		t.Errorf("nachdem der Knoten zurück ist: %v", err)
	}
}
