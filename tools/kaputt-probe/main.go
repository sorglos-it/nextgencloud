// Command probe is a throwaway measuring instrument for the damage runs of
// 18.10.2026. It opens the encrypted database through the project's own code
// path and reports what SQLite says about the file. It prints no secrets.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlite"
	"github.com/sorglos-it/nextgencloud/apps/server-db/classes/sqlstore"
	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
)

func main() {
	cfgPath := flag.String("config", "/etc/ngc/api.json", "configuration file")
	dbPath := flag.String("db", "", "override the database path from the configuration")
	writes := flag.Int("write", 0, "add this many log rows (0 = none)")
	forever := flag.Bool("forever", false, "keep writing log rows until killed")
	blobs := flag.Bool("blobs", false, "list kind, id and seq of ngc_blob (no values)")
	digest := flag.Bool("digest", false, "print a digest over the rows of ngc_log")
	check := flag.Bool("check", false, "run PRAGMA integrity_check and count rows")
	flag.Parse()

	cfg, err := readDB(*cfgPath)
	if err != nil {
		fmt.Println("CONFIG-ERROR:", err)
		os.Exit(2)
	}
	if *dbPath != "" {
		cfg.Path = *dbPath
	}
	// Self-check of the instrument: the file it is about to judge must exist.
	if st, err := os.Stat(cfg.Path); err != nil {
		fmt.Println("PROBE-PRECONDITION-FAILED: no file at the database path:", err)
		os.Exit(2)
	} else {
		fmt.Printf("probe: file=%d bytes\n", st.Size())
	}

	db, err := sqlite.OpenDB(cfg)
	if err != nil {
		fmt.Println("OPEN-ERROR:", err)
		os.Exit(1)
	}
	defer db.Close()

	if *check {
		runCheck(db)
	}
	if *blobs {
		runBlobs(db)
	}
	if *digest {
		runDigest(db)
	}
	if *writes > 0 || *forever {
		runWrites(db, *writes, *forever)
	}
}

// readDB reads only the db section of the configuration. The key never leaves
// this function as text.
func readDB(path string) (config.DB, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return config.DB{}, err
	}
	var outer struct {
		DB    json.RawMessage `json:"db"`
		DBKey json.RawMessage `json:"dbKey"`
	}
	if err := json.Unmarshal(b, &outer); err != nil {
		return config.DB{}, err
	}
	var cfg config.DB
	if err := json.Unmarshal(outer.DB, &cfg); err == nil {
		return cfg, nil
	}
	// The controller writes db as a plain path and dbKey beside it.
	var dbfile string
	if err := json.Unmarshal(outer.DB, &dbfile); err != nil {
		return config.DB{}, err
	}
	cfg = config.DB{Driver: config.DriverSQLite, Path: dbfile}
	if len(outer.DBKey) > 0 {
		if err := json.Unmarshal(outer.DBKey, &cfg.Key); err != nil {
			return config.DB{}, err
		}
	}
	return cfg, nil
}

func runCheck(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		fmt.Println("INTEGRITY-ERROR:", err)
	} else {
		n := 0
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				fmt.Println("INTEGRITY-SCAN-ERROR:", err)
				break
			}
			n++
			if n <= 10 {
				fmt.Println("integrity_check:", s)
			}
		}
		if err := rows.Err(); err != nil {
			fmt.Println("INTEGRITY-ROWS-ERROR:", err)
		}
		fmt.Printf("integrity_check: %d line(s)\n", n)
		rows.Close()
	}

	for _, t := range []string{"ngc_user", "ngc_log", "ngc_blob", "ngc_app", "ngc_session"} {
		var c int64
		err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+t).Scan(&c)
		if err != nil {
			fmt.Printf("count %s: ERROR %v\n", t, err)
			continue
		}
		fmt.Printf("count %s: %d\n", t, c)
	}
}

// runBlobs names the rows of ngc_blob. It prints kind, id, seq and the SIZE
// of the value, never the value: a trust list and a catalogue are in there.
func runBlobs(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT kind, id, seq, length(data) FROM ngc_blob ORDER BY kind, seq, id")
	if err != nil {
		fmt.Println("BLOBS-ERROR:", err)
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var kind, id string
		var seq, size int64
		if err := rows.Scan(&kind, &id, &seq, &size); err != nil {
			fmt.Println("BLOBS-SCAN-ERROR:", err)
			return
		}
		n++
		fmt.Printf("blob: kind=%s id=%s seq=%d bytes=%d\n", kind, id, seq, size)
	}
	if err := rows.Err(); err != nil {
		fmt.Println("BLOBS-ROWS-ERROR:", err)
	}
	fmt.Printf("blob: %d row(s)\n", n)
}

// runDigest reads every row of ngc_log in a fixed order and prints one hash
// over it. Two runs on two files with the same rows print the same hash, so a
// changed hash with an unchanged row COUNT is content that changed silently.
// It prints the hash and the number of rows, never a row.
func runDigest(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT id, ts, kind, result_code, detail FROM ngc_log ORDER BY id")
	if err != nil {
		fmt.Println("DIGEST-ERROR:", err)
		return
	}
	defer rows.Close()
	h := sha256.New()
	n := 0
	for rows.Next() {
		var id, kind, result, detail string
		var t int64
		if err := rows.Scan(&id, &t, &kind, &result, &detail); err != nil {
			fmt.Println("DIGEST-SCAN-ERROR:", err)
			fmt.Printf("digest: BROKEN after %d rows\n", n)
			return
		}
		n++
		fmt.Fprintf(h, "%s|%d|%s|%s|%s\n", id, t, kind, result, detail)
	}
	if err := rows.Err(); err != nil {
		fmt.Println("DIGEST-ROWS-ERROR:", err)
		fmt.Printf("digest: BROKEN after %d rows\n", n)
		return
	}
	fmt.Printf("digest: rows=%d sha256=%s\n", n, hex.EncodeToString(h.Sum(nil))[:32])
}

func runWrites(db *sql.DB, n int, forever bool) {
	st := sqlstore.New(db, sqlstore.SQLite)
	ok, fail := 0, 0
	var lastErr error
	start := time.Now()
	for i := 0; forever || i < n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := st.LogAdd(ctx, core.LogEntry{
			ID:     newID(),
			Time:   time.Now().Unix(),
			Kind:   "probe",
			Result: "ok",
			Detail: fmt.Sprintf("damage run filler %d %s", i, filler),
		})
		cancel()
		if err != nil {
			fail++
			if lastErr == nil || err.Error() != lastErr.Error() {
				fmt.Printf("WRITE-ERROR after %d ok: %v\n", ok, err)
				if errorsIsInvalid(err) {
					fmt.Println("WRITE-ERROR is ErrInvalid - the instrument built a bad row, not a finding")
					os.Exit(2)
				}
				lastErr = err
			}
			if fail > 20 {
				break
			}
			continue
		}
		ok++
		if ok%200 == 0 {
			fmt.Printf("wrote %d rows in %.1fs\n", ok, time.Since(start).Seconds())
		}
	}
	fmt.Printf("writes: ok=%d fail=%d in %.1fs\n", ok, fail, time.Since(start).Seconds())
}

func errorsIsInvalid(err error) bool { return err == dbapi.ErrInvalid }

// filler makes each row big enough that a few thousand of them fill a small
// filesystem in a measurable time.
var filler = func() string {
	b := make([]byte, 900)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}()

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
