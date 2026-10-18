// Command fscryptprobe is a measuring fixture, not part of NGC: it asks the
// real runtime package for the encrypted data directory of one app and prints
// what the kernel answered, so the errno behind ErrNoFscrypt is on the record.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"

	ctrlrt "github.com/sorglos-it/nextgencloud/apps/server-controller/classes/runtime"
)

func main() {
	app := "org.sorglos.probe"
	if len(os.Args) > 1 {
		app = os.Args[1]
	}
	key := make([]byte, 64)
	if _, err := rand.Read(key); err != nil {
		fmt.Println("PROBE_FAIL rand", err)
		os.Exit(1)
	}
	dir, err := ctrlrt.EnsureData(context.Background(), app, ctrlrt.User{UID: 60000, GID: 60000}, key)
	fmt.Printf("DIR=%q\n", dir)
	fmt.Printf("ERR=%v\n", err)
	fmt.Printf("IS_NO_FSCRYPT=%v\n", errors.Is(err, ctrlrt.ErrNoFscrypt))
	fmt.Printf("TEXT=%s\n", ctrlrt.NoFscryptText)
	if err != nil {
		os.Exit(2)
	}
}
