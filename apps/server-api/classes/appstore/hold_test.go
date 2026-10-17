package appstore

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// TestAnUploadedPackageIsKeptForItsActivation is Punkt 30 from the other side:
// an upload keeps its package, and spoolPackage finds it by the checksum of the
// signed task. Until 17.10.2026 nobody held the file, so spoolPackage answered
// ErrStore for every uploaded app and an activation of one ended E_UNREACHABLE
// -- measured on a running installation that day, after the upload itself was
// fixed.
//
// It goes through spoolPackage and not through the hold alone: what has to hold
// is that the ACTIVATION finds the file, and the activation asks that function.
func TestAnUploadedPackageIsKeptForItsActivation(t *testing.T) {
	st, _ := newStores(t)
	st.Hold = t.TempDir()
	pkg := buildPackage(t, goodEntries(t))
	res, err := st.Upload(ctx, bytes.NewReader(pkg))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	sum := sha256.Sum256(pkg)
	if !bytes.Equal(res.SHA256, sum[:]) {
		t.Fatal("the upload answered another checksum than the file has")
	}
	// The name is the checksum and nothing else, so a reader that knows the
	// sum needs no row and no clock to find the file.
	name, ok := holdName(sum[:])
	if !ok {
		t.Fatal("a SHA-256 has no name in the hold")
	}
	if _, err := os.Stat(filepath.Join(st.Hold, name)); err != nil {
		t.Fatalf("the package was not kept under its checksum: %v", err)
	}
	// And this is the line Punkt 30 was about: the activation asks for the
	// file with the sum of the signed task and gets the bytes it uploaded.
	var got bytes.Buffer
	n, err := st.spoolPackage(ctx, ActivateRequest{
		Params: &core.ActivateParams{App: "org.example.notes", Version: "0.1.0",
			Store: core.LocalStore, SHA256: enc.Bytes(sum[:])},
	}, &got)
	if err != nil {
		t.Fatalf("spoolPackage: %v", err)
	}
	if n != int64(len(pkg)) || !bytes.Equal(got.Bytes(), pkg) {
		t.Fatalf("spooled %d bytes of %d, and they are %v the package", n, len(pkg),
			map[bool]string{true: "", false: "not"}[bytes.Equal(got.Bytes(), pkg)])
	}
}

// TestAnActivationFindsNoPackageItNeverGotHolds the other half, and it is the
// half that keeps the hold honest: a checksum nobody uploaded finds nothing,
// and so does a server that keeps nothing at all. Both read as ErrStore, which
// is the E_UNREACHABLE an activation answered before the hold existed -- an
// installation of two API servers behind one database still lands there, and
// that is the named limit of hold.go and not a fault here.
func TestAnActivationFindsNoPackageItNeverGot(t *testing.T) {
	pkg := buildPackage(t, goodEntries(t))
	sum := sha256.Sum256(pkg)
	other := sha256.Sum256([]byte("another file entirely"))

	// A hold that was never written to: the checksum is good, the file is not
	// there.
	st, _ := newStores(t)
	st.Hold = t.TempDir()
	var sink bytes.Buffer
	if _, err := st.spoolPackage(ctx, ActivateRequest{Params: &core.ActivateParams{
		Store: core.LocalStore, SHA256: enc.Bytes(sum[:])}}, &sink); err != ErrStore {
		t.Fatalf("an empty hold answered %v, want ErrStore", err)
	}
	// Uploaded, but the task names another file. The upload is in the hold, so
	// this is not "the hold is empty": it is "not that one".
	if _, err := st.Upload(ctx, bytes.NewReader(pkg)); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	sink.Reset()
	if _, err := st.spoolPackage(ctx, ActivateRequest{Params: &core.ActivateParams{
		Store: core.LocalStore, SHA256: enc.Bytes(other[:])}}, &sink); err != ErrStore {
		t.Fatalf("a foreign checksum answered %v, want ErrStore", err)
	}
	if sink.Len() != 0 {
		t.Fatalf("%d bytes were spooled for a package that was never uploaded", sink.Len())
	}
	// A server that keeps nothing: the same answer as before the hold existed,
	// and the upload still works.
	bare, _ := newStores(t)
	if _, err := bare.Upload(ctx, bytes.NewReader(pkg)); err != nil {
		t.Fatalf("Upload without a hold: %v", err)
	}
	sink.Reset()
	if _, err := bare.spoolPackage(ctx, ActivateRequest{Params: &core.ActivateParams{
		Store: core.LocalStore, SHA256: enc.Bytes(sum[:])}}, &sink); err != ErrStore {
		t.Fatalf("a server without a hold answered %v, want ErrStore", err)
	}
}

// TestOnlyASHA256HasAName holds the one guard of the name, and a mutant that
// dropped it lived through every other case here on 17.10.2026: a value of
// another length than a SHA-256 gets NO name at all.
//
// It matters because the name is a path. A sum of three bytes would make a file
// name of four characters, and then "the file is called after its checksum" is
// no longer true of what lies in the directory -- two packages could share a
// name, and an activation would be handed bytes nobody signed for. The guard is
// in one place (holdName) and everything that reads or writes the hold goes
// through it, which is why the two doors are checked here as well.
func TestOnlyASHA256HasAName(t *testing.T) {
	good := sha256.Sum256([]byte("a package"))
	if _, ok := holdName(good[:]); !ok {
		t.Fatal("a SHA-256 has no name")
	}
	for _, c := range []struct {
		name string
		sum  []byte
	}{
		{"nothing at all", nil},
		{"an empty slice", []byte{}},
		{"one byte short", good[:len(good)-1]},
		{"one byte long", append(good[:], 0)},
		{"three bytes", []byte{1, 2, 3}},
	} {
		if n, ok := holdName(c.sum); ok {
			t.Errorf("%s got the name %q", c.name, n)
		}
	}
	st, _ := newStores(t)
	st.Hold = t.TempDir()
	if _, _, err := st.heldPackage([]byte{1, 2, 3}); err == nil {
		t.Error("heldPackage answered for a sum that is no SHA-256")
	}
	f, err := st.newHeld()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.keepHeld(f, []byte{1, 2, 3}); err != ErrStore {
		t.Errorf("keepHeld answered %v for a sum that is no SHA-256, want ErrStore", err)
	}
	if es, err := os.ReadDir(st.Hold); err != nil || len(es) != 0 {
		t.Errorf("the hold holds %d entries after a refused name (%v)", len(es), err)
	}
}

// TestAHeldPackageIsSweptByAge holds the answer to the second question of Punkt
// 30, what stays behind: everything in the hold goes by AGE, at every upload,
// so a package nobody activated and the temporary file of an upload that died
// halfway are cleaned the same way. The ages are moved, not the clock -- see
// holdNow.

// firstName is the hold name of a package, so a case can age it.
func firstName(t *testing.T, pkg []byte) string {
	t.Helper()
	sum := sha256.Sum256(pkg)
	name, ok := holdName(sum[:])
	if !ok {
		t.Fatal("a SHA-256 has no name in the hold")
	}
	return name
}

func TestAHeldPackageIsSweptByAge(t *testing.T) {
	st, _ := newStores(t)
	st.Hold = t.TempDir()
	st.HoldFor = 10 * time.Minute
	pkg := buildPackage(t, goodEntries(t))
	if _, err := st.Upload(ctx, bytes.NewReader(pkg)); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	// A leftover of an upload that never finished, still young enough to stay:
	// it has to be here for the FIRST sweep to prove it is age that decides
	// and not the name -- a sweep that went by the prefix would take it now.
	dead := filepath.Join(st.Hold, holdPrefix+"1234567890")
	if err := os.WriteFile(dead, []byte("half an upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ages are set with the clock the sweep reads -- the file system's -- and
	// not with the state's: the two may disagree, and a cut in the wrong time
	// base sweeps everything or nothing (holdNow).
	young := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(dead, young, young); err != nil {
		t.Fatal(err)
	}
	// Nothing is swept while both are young enough.
	st.sweepHold(st.holdNow())
	if es, err := os.ReadDir(st.Hold); err != nil || len(es) != 2 {
		t.Fatalf("before the clock moved the hold has %d entries (%v), want 2", len(es), err)
	}
	// Both are pushed past HoldFor and the next upload sweeps them. The ages
	// move, not the clock: see holdNow.
	for _, name := range []string{dead, filepath.Join(st.Hold, firstName(t, pkg))} {
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(name, old, old); err != nil {
			t.Fatal(err)
		}
	}
	other := buildPackage(t, append(goodEntries(t), packEntry{name: "ui/extra.txt", body: []byte("x"), kind: tar.TypeReg}))
	if _, err := st.Upload(ctx, bytes.NewReader(other)); err != nil {
		t.Fatalf("second Upload: %v", err)
	}
	es, err := os.ReadDir(st.Hold)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the new one is left: the sweep ran BEFORE the second package was
	// written, so a sweep that took the fresh one with it would show here.
	if len(es) != 1 {
		names := make([]string, 0, len(es))
		for _, e := range es {
			names = append(names, e.Name())
		}
		t.Fatalf("the hold holds %v, want only the package of the second upload", names)
	}
	sum := sha256.Sum256(other)
	name, _ := holdName(sum[:])
	if es[0].Name() != name {
		t.Fatalf("the hold kept %q, want %q", es[0].Name(), name)
	}
}

// TestARefusedUploadKeepsNothing holds what the hold may never carry: the
// package of an upload that was turned away. It is checked for both walls an
// upload has, because they return from different places -- an archive the walk
// refuses, and a file over the bound.
func TestARefusedUploadKeepsNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		body []byte
		max  int64
	}{
		{"an archive that is no package", []byte("this is not a tar at all"), 0},
		{"a package over the bound", buildPackage(t, goodEntries(t)), 32},
	} {
		st, _ := newStores(t)
		st.Hold = t.TempDir()
		st.MaxUpload = c.max
		if _, err := st.Upload(ctx, bytes.NewReader(c.body)); err == nil {
			t.Fatalf("%s: Upload was taken", c.name)
		}
		es, err := os.ReadDir(st.Hold)
		if err != nil {
			t.Fatal(err)
		}
		if len(es) != 0 {
			names := make([]string, 0, len(es))
			for _, e := range es {
				names = append(names, e.Name())
			}
			t.Fatalf("%s: the hold holds %v after a refused upload", c.name, names)
		}
	}
}
