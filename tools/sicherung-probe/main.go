// sicherung-probe answers one question about a backup, and it is the question
// the backup itself cannot answer: does this file still open, and with WHAT.
//
// It takes the copy a "ngc-controller backup" wrote and a file holding the
// dbKey of controller.json (base64url, the one line, with or without quotes),
// and opens the copy three times: with that key, with one bit of that key
// changed, and with no key at all. The second and third answers are the point
// - a backup kept without its key is bytes, and that sentence is worth more
// measured than reasoned.
//
// Aufruf:
//
//	go run ./tools/sicherung-probe <sicherung.db> <schluessel.txt>
//
// It opens the file it is given, and state.Open MIGRATES what it opens, so it
// is handed a copy of the copy and never the only one there is.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sorglos-it/nextgencloud/apps/server-controller/classes/state"
	"github.com/sorglos-it/nextgencloud/shared/config"
)

func try(name, path string, key []byte) {
	st, err := state.Open(config.DB{Driver: config.DriverSQLite, Path: path, Key: config.NewSecret(key)}, "messprobe", time.Now().Unix())
	if err != nil {
		fmt.Printf("%-22s OEFFNEN FEHLGESCHLAGEN: %v\n", name, err)
		return
	}
	apps, err := st.Apps(context.Background())
	if err != nil {
		fmt.Printf("%-22s offen, aber Lesen fehlgeschlagen: %v\n", name, err)
		return
	}
	ids := []string{}
	for _, a := range apps {
		ids = append(ids, fmt.Sprintf("%s@%s(aktiv=%v)", a.ID, a.Version, a.Active))
	}
	fmt.Printf("%-22s OFFEN, %d App-Zeilen: %v\n", name, len(apps), ids)
}

func main() {
	db, keyfile := os.Args[1], os.Args[2]
	raw, err := os.ReadFile(keyfile)
	if err != nil {
		panic(err)
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		s = string(raw)
	}
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '"') {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && s[0] == '"' {
		s = s[1:]
	}
	key, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(s)
	}
	if err != nil {
		panic(err)
	}
	fmt.Printf("Schluessel: %d Byte\n\n", len(key))
	try("richtiger Schluessel", db, key)
	wrong := append([]byte(nil), key...)
	wrong[0] ^= 1
	try("ein Bit falsch", db, wrong)
	try("ohne Schluessel", db, nil)
}
