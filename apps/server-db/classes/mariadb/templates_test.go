package mariadb

import (
	"os"
	"strings"
	"testing"
)

// TestGaleraTemplate holds the lines nobody may quietly drop: without them
// the cluster would replicate without TLS, take a connection in the clear or
// keep the data unencrypted on disk. The test is here and not only in the
// ulh script, because a shell script has no test of its own in this repo.
func TestGaleraTemplate(t *testing.T) {
	body, err := os.ReadFile("../../templates/galera.cnf")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, line := range []string{
		// nothing without TLS, and only TLS 1.3
		"require_secure_transport=ON",
		"tls_version=TLSv1.3",
		"ssl_ca=/etc/ngc/db-tls/ca.crt",
		"ssl_cert=/etc/ngc/db-tls/node.crt",
		"ssl_key=/etc/ngc/db-tls/node.key",
		// the replication between the nodes is checked against the same CA
		"socket.ssl_ca=/etc/ngc/db-tls/ca.crt",
		"socket.ssl_cert=/etc/ngc/db-tls/node.crt",
		"socket.ssl_key=/etc/ngc/db-tls/node.key",
		// MariaDB's own encryption, AES-256 through a 32-byte key
		"plugin_load_add=file_key_management",
		"file_key_management_encryption_algorithm=AES_CTR",
		"innodb_encrypt_tables=FORCE",
		"innodb_encrypt_log=ON",
		"innodb_encrypt_temporary_tables=ON",
		"encrypt_tmp_disk_tables=ON",
		"encrypt_binlog=ON",
		"aria_encrypt_tables=ON",
		// what Galera needs
		"wsrep_on=ON",
		"binlog_format=ROW",
		"default_storage_engine=InnoDB",
		"innodb_autoinc_lock_mode=2",
		// a blob may be as large as a canonical ui-tar
		"max_allowed_packet=128M",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the line %q is missing", line)
		}
	}
	for _, forbidden := range []string{
		"skip-ssl", "ssl=0", "require_secure_transport=OFF", "wsrep_on=OFF",
		"innodb_encrypt_tables=OFF", "skip-name-resolve=OFF", "bind-address=0.0.0.0",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("the line %q must not be there", forbidden)
		}
	}
	for _, placeholder := range []string{"@NODE_NAME@", "@NODE_ADDRESS@", "@CLUSTER_NAME@", "@CLUSTER_ADDRESS@"} {
		if !strings.Contains(text, placeholder) {
			t.Errorf("the placeholder %q is missing", placeholder)
		}
	}
}
