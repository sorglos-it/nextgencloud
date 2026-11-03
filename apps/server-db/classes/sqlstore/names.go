// Package sqlstore implements dbapi.Store on database/sql in SQL that
// MariaDB, SQLite, MySQL, MSSQL and Oracle all understand. Only the column
// types and the suffix of CREATE TABLE differ; they come from a Dialect.
package sqlstore

import (
	"fmt"
	"strings"
)

// Tables are every table NGC uses, in the order 0001 creates them. Nothing
// outside this list is ever named in a statement.
var Tables = []string{
	"ngc_schema", "ngc_lock", "ngc_blob", "ngc_app", "ngc_once",
	"ngc_user", "ngc_session", "ngc_pending", "ngc_log",
}

// Columns are the columns of each table, in the order of the CREATE TABLE.
var Columns = map[string][]string{
	"ngc_schema":  {"version", "step", "done", "applied"},
	"ngc_lock":    {"name", "owner", "until"},
	"ngc_blob":    {"kind", "id", "seq", "data", "updated"},
	"ngc_app":     {"id", "version", "store", "node", "active", "manifest", "sha256", "grants", "kept_data", "updated"},
	"ngc_once":    {"kind", "token", "data", "expires", "used"},
	"ngc_user":    {"id", "name", "roles", "totp", "locked", "failures", "last_login"},
	"ngc_session": {"token", "user_id", "cert", "proof", "api_key", "api_cert", "created", "expires", "seen"},
	"ngc_pending": {"pubkey", "name", "since"},
	"ngc_log":     {"id", "ts", "user_id", "kind", "app", "action", "node", "result_code", "detail"},
}

// Placeholders are the names a migration may use for a column type. A
// migration writes {{key}}; Fill puts the dialect's type in its place.
var Placeholders = []string{"key", "text", "doc", "data", "int", "opts"}

// ColumnType is the placeholder each column was created with, by table and
// column name. It is the one piece this package was missing, and the reason it
// stands here as data rather than being read out of a file: until 30.10.2026
// the only place that said which type a column carries was the TEXT of
// sql/0001-core.sql, so nothing a program runs could ask the question - and
// what is not in the repo as data does not hold. Columns knows the names,
// this knows what each one is.
//
// What it is FOR is a check against the real database: the type a column
// really has, read back out of the database, against the one it was made with.
// Measured on 30.10.2026 why that is worth having: ngc_blob.seq turned from
// the number type into the byte type lets both SQLite and MariaDB start
// without a word, and BlobFrom then hands its rows back in the wrong order -
// on SQLite it hands back rows that do not belong in the answer at all. No
// error anywhere. The check that looked like it covered this asked only
// whether a type is ONE OF the dialect's types, and "BLOB" is one of them.
//
// "opts" is not in here: it is the suffix of CREATE TABLE, not a column.
// migrate's TestColumnTypeIsWhatTheMigrationsWrite holds every entry against
// the statements the migrations really carry, so neither half can drift.
var ColumnType = map[string]map[string]string{
	"ngc_schema": {"version": "int", "step": "int", "done": "int", "applied": "int"},
	"ngc_lock":   {"name": "key", "owner": "text", "until": "int"},
	"ngc_blob":   {"kind": "key", "id": "key", "seq": "int", "data": "data", "updated": "int"},
	"ngc_app": {"id": "key", "version": "key", "store": "key", "node": "key", "active": "int",
		"manifest": "doc", "sha256": "key", "grants": "doc", "kept_data": "int", "updated": "int"},
	"ngc_once": {"kind": "key", "token": "key", "data": "key", "expires": "int", "used": "int"},
	"ngc_user": {"id": "key", "name": "key", "roles": "doc", "totp": "doc", "locked": "int",
		"failures": "int", "last_login": "int"},
	"ngc_session": {"token": "key", "user_id": "key", "cert": "doc", "proof": "doc",
		"api_key": "doc", "api_cert": "doc", "created": "int", "expires": "int", "seen": "int"},
	"ngc_pending": {"pubkey": "key", "name": "key", "since": "int"},
	"ngc_log": {"id": "key", "ts": "int", "user_id": "key", "kind": "key", "app": "key",
		"action": "key", "node": "key", "result_code": "key", "detail": "text"},
}

// reserved holds the words the five databases reserve, upper case. The list
// is why columns are called user_id, ts and result_code: "user", "time" and
// "result" are reserved somewhere. It is not the full standard, only what a
// name of ours could realistically hit. "action" is deliberately not in it:
// none of the five reserves it, and the column of a log entry is called that
// in the contract too (core.LogEntry.Action).
var reserved = []string{
	"ADD", "ALL", "ALTER", "AND", "AS", "ASC", "AUDIT", "BETWEEN", "BY", "CASE", "CHECK",
	"COLUMN", "COMMENT", "COMMIT", "CREATE", "CURRENT", "CURRENT_DATE", "CURRENT_TIME",
	"CURRENT_TIMESTAMP", "CURRENT_USER", "DATE", "DEFAULT", "DELETE", "DESC", "DISTINCT", "DROP",
	"ELSE", "END", "EXISTS", "FILE", "FOR", "FROM", "GRANT", "GROUP", "HAVING", "IN", "INDEX",
	"INSERT", "INTO", "IS", "KEY", "LEVEL", "LIKE", "LOCK", "LONG", "MODE", "NOT", "NULL", "NUMBER",
	"OF", "OFFSET", "ON", "OPTION", "OR", "ORDER", "PASSWORD", "PRIMARY", "PUBLIC", "RAW", "RENAME",
	"RESOURCE", "REVOKE", "ROW", "ROWID", "ROWNUM", "ROWS", "SELECT", "SESSION", "SET", "SHARE",
	"SIZE", "START", "SYSDATE", "TABLE", "THEN", "TIME", "TIMESTAMP", "TO", "TRIGGER", "UID",
	"UNION", "UNIQUE", "UPDATE", "USER", "VALIDATE", "VALUES", "VARCHAR", "VIEW", "WHEN", "WHERE",
	"WITH",
}

// Fill puts the dialect's column types into a migration statement. An
// unknown or malformed placeholder is an error, so a typo in a migration
// never reaches the database.
func Fill(d Dialect, stmt string) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(stmt, "{{")
		if i < 0 {
			b.WriteString(stmt)
			return b.String(), nil
		}
		b.WriteString(stmt[:i])
		rest := stmt[i+2:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			return "", fmt.Errorf("sqlstore: placeholder without end in %q", stmt)
		}
		name := rest[:j]
		typ := d.Type(name)
		if typ == "" && name != "opts" {
			return "", fmt.Errorf("sqlstore: unknown placeholder %q", name)
		}
		b.WriteString(typ)
		stmt = rest[j+2:]
	}
}
