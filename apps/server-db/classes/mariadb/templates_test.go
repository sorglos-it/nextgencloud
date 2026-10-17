package mariadb

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// wsrepOptions is the key whose value is a list of its own, and the one line
// of galera.cnf that no whole-line comparison can pin.
const wsrepOptions = "wsrep_provider_options="

// TestGaleraTemplate holds the lines nobody may quietly drop: without them
// the cluster would replicate without TLS, take a connection in the clear or
// keep the data unencrypted on disk. The test is here and not only in the
// ulh script, because a shell script has no test of its own in this repo.
//
// It pins EVERY line of the file, and the last loop is what makes that claim
// checkable instead of a promise: a line of the file that no list below names
// fails the test. Measured before that loop existed: four lines
// (wsrep_provider, wsrep_cluster_name, file_key_management_filename and
// file_key_management_filekey) could be deleted and this test stayed green,
// although its own comment said "every line is checked".
func TestGaleraTemplate(t *testing.T) {
	body, err := os.ReadFile("../../templates/galera.cnf")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)

	// wholeLines are pinned as a line of their own. A part of a line is not
	// enough: "ssl_ca=..." also stands inside "socket.ssl_ca=..." and in the
	// [mariabackup] section, so a server left without TLS would have passed a
	// check on the text alone.
	wholeLines := []string{
		// the sections
		"[mysqld]",
		"[mariabackup]",
		// nothing without TLS, and only TLS 1.3
		"require_secure_transport=ON",
		"tls_version=TLSv1.3",
		"ssl_ca=/etc/ngc/db-tls/ca.crt",
		"ssl_cert=/etc/ngc/db-tls/node.crt",
		"ssl_key=/etc/ngc/db-tls/node.key",
		// MariaDB's own encryption, AES-256 through a 32-byte key
		"plugin_load_add=file_key_management",
		"file_key_management_filename=/etc/ngc/db-keys/keys.enc",
		"file_key_management_filekey=FILE:/etc/ngc/db-keys/keyfile.key",
		"file_key_management_encryption_algorithm=AES_CTR",
		"innodb_encrypt_tables=FORCE",
		"innodb_encrypt_log=ON",
		"innodb_encrypt_temporary_tables=ON",
		"encrypt_tmp_disk_tables=ON",
		"encrypt_binlog=ON",
		"aria_encrypt_tables=ON",
		// what Galera needs
		"wsrep_on=ON",
		"wsrep_provider=/usr/lib/galera/libgalera_smm.so",
		"wsrep_cluster_name=ngc",
		"wsrep_cluster_address=gcomm://@CLUSTER_ADDRESS@",
		"wsrep_node_name=@NODE_NAME@",
		"wsrep_node_address=@NODE_ADDRESS@",
		"binlog_format=ROW",
		"default_storage_engine=InnoDB",
		"innodb_autoinc_lock_mode=2",
		// a new node copies the data with mariabackup, over TLS as well
		"wsrep_sst_method=mariabackup",
		"wsrep_sst_auth=sst:@SST_PASSWORD@",
		// the node listens on its own address only
		"bind-address=@NODE_ADDRESS@",
		// a blob may be as large as a canonical ui-tar
		"max_allowed_packet=128M",
		// strict from the server side too
		"sql_mode=STRICT_ALL_TABLES,NO_ENGINE_SUBSTITUTION",
	}
	// wantOptions is the whole quoted value of wsrep_provider_options, in
	// order and with nothing else in it: the replication between the nodes is
	// checked against the same CA.
	wantOptions := []string{
		"socket.ssl_ca=/etc/ngc/db-tls/ca.crt",
		"socket.ssl_cert=/etc/ngc/db-tls/node.crt",
		"socket.ssl_key=/etc/ngc/db-tls/node.key",
		"socket.ssl_cipher=TLS_AES_256_GCM_SHA384",
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	has := func(line string) bool {
		for _, l := range lines {
			if l == line {
				return true
			}
		}
		return false
	}
	for _, line := range wholeLines {
		if !has(line) {
			t.Errorf("the line %q is missing", line)
		}
	}
	// The three plain ssl_* lines stand twice, once per section. Counted per
	// section, so a copy in [mariabackup] cannot stand in for the server's own
	// TLS - the gap this test had while it only searched the whole text.
	server, backup, inBackup := 0, 0, false
	for _, l := range lines {
		if l == "[mariabackup]" {
			inBackup = true
		}
		switch l {
		case "ssl_ca=/etc/ngc/db-tls/ca.crt", "ssl_cert=/etc/ngc/db-tls/node.crt", "ssl_key=/etc/ngc/db-tls/node.key":
			if inBackup {
				backup++
			} else {
				server++
			}
		}
	}
	if server != 3 {
		t.Errorf("[mysqld] carries %d of the three ssl_* lines", server)
	}
	if backup != 3 {
		t.Errorf("[mariabackup] carries %d of the three ssl_* lines", backup)
	}
	// The options of wsrep_provider_options are read out of THAT line and
	// compared as a set, not searched for in the whole text.
	//
	// Why: searching the text said nothing about where an option stands, and
	// nothing at all about what else stands beside it. The same shape cost
	// three cases of the container hardening their eyes (point 51): they read
	// a mount spec as one string and asked HasSuffix(":rw"), and once
	// ",nosuid,nodev" was appended they found no writable mount any more and
	// stayed green. Here it would be worse than blind: the options are joined
	// with ";" and MariaDB takes the LAST of a repeated one, so appending
	// ";socket.ssl_ca=/tmp/anything.crt" swaps the CA of the replication while
	// every Contains above still finds the right line somewhere in the file.
	optLine := ""
	for _, l := range lines {
		if strings.HasPrefix(l, wsrepOptions) {
			if optLine != "" {
				t.Fatalf("galera.cnf carries %s twice; the last one would win", wsrepOptions)
			}
			optLine = l
		}
	}
	if optLine == "" {
		t.Fatalf("galera.cnf carries no %s line, so this case measures an earlier wall", wsrepOptions)
	}
	quoted := strings.TrimPrefix(optLine, wsrepOptions)
	if len(quoted) < 2 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' {
		t.Fatalf("the value of %s is not one quoted string: %q", wsrepOptions, quoted)
	}
	got := strings.Split(quoted[1:len(quoted)-1], ";")
	if !slices.Equal(got, wantOptions) {
		t.Errorf("%s carries %v, want exactly %v", wsrepOptions, got, wantOptions)
	}
	for _, forbidden := range []string{
		"skip-ssl", "ssl=0", "require_secure_transport=OFF", "wsrep_on=OFF",
		"innodb_encrypt_tables=OFF", "skip-name-resolve=OFF", "bind-address=0.0.0.0",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("the line %q must not be there", forbidden)
		}
	}
	// The four marks ngc-db.sh replaces. The cluster name is NOT one of them:
	// it is ngc for every installation, and no answer of ulh fills it, so a
	// mark there would have stayed in the written file.
	for _, placeholder := range []string{"@NODE_NAME@", "@NODE_ADDRESS@", "@CLUSTER_ADDRESS@", "@SST_PASSWORD@"} {
		if !strings.Contains(text, placeholder) {
			t.Errorf("the placeholder %q is missing", placeholder)
		}
	}
	// And the other way round: every line of the file has to be named above.
	// Without this a line could be added - or a pinned one renamed - and the
	// sentence "every line is checked" would quietly stop being true.
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if slices.Contains(wholeLines, l) {
			continue
		}
		// Pinned above as a set of options, not as a whole line.
		if strings.HasPrefix(l, wsrepOptions) {
			continue
		}
		t.Errorf("line %d of galera.cnf is not pinned by this test: %q", i+1, l)
	}
}
