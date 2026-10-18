package install

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/tunnel"
)

// The version the staged update brings and what the app kept before it.
const (
	testNewVer   = "1.1.0"
	testKeptData = int64(4242)
)

// upd is one call to Update: the row of the app as it stands, the staged file
// and the AppUpdate a caller hands over. Every case bends one thing of it and
// calls run, so what the case measures is that one thing.
type upd struct {
	t   *testing.T
	s   *stand
	mf  *store.Manifest
	dir string
	in  AppUpdate
}

// newUpd is an update that has to come off: version 1.0.0 of the app is
// installed with the grants root and network, and 1.1.0 lies ready and needs
// network alone - so the case also sees the grants being cut down.
func newUpd(t *testing.T) *upd {
	t.Helper()
	s := newStand(t)
	s.apps.rows[testApp] = dbapi.App{ID: testApp, Version: testVer, Store: testStore,
		Node: "ctl-1", Active: true, SHA256: enc.Bytes(bytes.Repeat([]byte{0x11}, 32)),
		Grants: []string{store.NeedRoot, store.NeedNetwork}, KeptData: testKeptData,
		Updated: testNow - 1}
	mf := appManifest(testApp, testNewVer, "")
	mf.Needs = []string{store.NeedNetwork}
	u := &upd{t: t, s: s, mf: mf, dir: t.TempDir()}
	u.setPackage(nil)
	return u
}

// setPackage builds the package of the current manifest, writes it where the
// staged file lies and carries its checksum into the AppUpdate - so a case
// that bends the manifest measures the manifest and not a stale checksum. bend
// changes the bytes after they were built, for the one case that wants a file
// which is not the one the checksum names.
func (u *upd) setPackage(bend func([]byte) []byte) {
	u.t.Helper()
	pkg := buildTar(u.t, appEntries(u.t, u.mf, 0))
	sum := sha(pkg)
	if bend != nil {
		pkg = bend(pkg)
	}
	file := filepath.Join(u.dir, "staged-"+testApp)
	if err := os.WriteFile(file, pkg, 0o600); err != nil {
		u.t.Fatal(err)
	}
	u.in = AppUpdate{App: testApp, Version: u.mf.Version, SHA256: sum, File: file,
		Needs: slices.Clone(u.mf.Needs)}
}

// withManifest rewrites the manifest and builds the package again.
func (u *upd) withManifest(fn func(m *store.Manifest)) *upd {
	fn(u.mf)
	u.setPackage(nil)
	return u
}

func (u *upd) run() error { return New(u.s.deps).Update(u.t.Context(), u.in) }

// row is the row of the app as it stands now.
func (u *upd) row() dbapi.App { return u.s.apps.rows[testApp] }

// left is how many folders Update left under StageDir.
func (u *upd) left() int {
	u.t.Helper()
	es, err := os.ReadDir(u.s.deps.StageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		u.t.Fatal(err)
	}
	return len(es)
}

// TestUpdateSetsTheNewVersionUpWithItsOwnNeeds is the one case that has to go
// through. Everything below bends one thing of this fixture, so this case is
// the proof that the way to the bent line was open.
func TestUpdateSetsTheNewVersionUpWithItsOwnNeeds(t *testing.T) {
	u := newUpd(t)
	if err := u.run(); err != nil {
		t.Fatalf("the unbent update does not install: %v", err)
	}
	row := u.row()
	switch {
	case row.Version != testNewVer:
		t.Errorf("row version %q, want %q", row.Version, testNewVer)
	case row.Store != testStore:
		t.Errorf("row store %q, want %q", row.Store, testStore)
	case !slices.Equal(row.Grants, []string{store.NeedNetwork}):
		t.Errorf("row grants %v, want the needs of the new version", row.Grants)
	case !bytes.Equal(row.SHA256, u.in.SHA256):
		t.Error("the row does not carry the checksum of the staged file")
	case row.KeptData != testKeptData:
		t.Errorf("kept data %d, want %d", row.KeptData, testKeptData)
	}
	// The data of the app stays: an update never drops the directory or the
	// user, and neither of the two may be made again either - EnsureUser and
	// EnsureData are idempotent and are what the setup walks.
	if slices.Contains(u.s.sys.calls, "DropData") || slices.Contains(u.s.sys.calls, "DropUser") {
		t.Errorf("the setup touched user or data: %v", u.s.sys.calls)
	}
	// The container is built out of the NEW needs: network yes, root no, even
	// though the old row was granted root.
	if c := u.s.eng.created; !c.Network || c.Root {
		t.Errorf("container network=%v root=%v, want true/false", c.Network, c.Root)
	}
	if n := u.left(); n != 0 {
		t.Errorf("%d folders left under StageDir, want 0", n)
	}
}

// TestUpdateRefusesBytesThatAreNotTheStagedOnes nails the one wall that makes
// the whole call safe: the file lay on a disk between staging and installing,
// and only its SHA-256 says it is still the file a signature covered.
func TestUpdateRefusesBytesThatAreNotTheStagedOnes(t *testing.T) {
	cases := []struct {
		name string
		bend func([]byte) []byte
	}{
		{"one byte turned", func(b []byte) []byte {
			c := slices.Clone(b)
			c[len(c)/2] ^= 0xff
			return c
		}},
		{"a byte more", func(b []byte) []byte { return append(slices.Clone(b), 0) }},
		{"a byte less", func(b []byte) []byte { return slices.Clone(b[:len(b)-1]) }},
		{"another whole package", func([]byte) []byte {
			return buildTar(t, appEntries(t, appManifest(testApp, testNewVer, ""), 0))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpd(t)
			u.setPackage(c.bend)
			if err := u.run(); !errors.Is(err, ErrVerify) {
				t.Fatalf("%v, want ErrVerify", err)
			}
			if u.row().Version != testVer {
				t.Errorf("the row says %q, want the old version", u.row().Version)
			}
			if len(u.s.sys.calls) != 0 {
				t.Errorf("this machine was changed: %v", u.s.sys.calls)
			}
			if n := u.left(); n != 0 {
				t.Errorf("%d folders left under StageDir, want 0", n)
			}
		})
	}
}

// TestUpdateRefusesAPackageThatSaysSomethingElse: the bytes may be the staged
// ones and the manifest still name another app, another version or an NGC this
// one is not.
func TestUpdateRefusesAPackageThatSaysSomethingElse(t *testing.T) {
	cases := []struct {
		name string
		bend func(u *upd)
	}{
		{"another app", func(u *upd) { u.withManifest(func(m *store.Manifest) { m.ID = "org.example.other" }) }},
		{"another version", func(u *upd) {
			u.withManifest(func(m *store.Manifest) { m.Version = "2.0.0" })
			u.in.Version = testNewVer
		}},
		{"an NGC this is not", func(u *upd) {
			u.withManifest(func(m *store.Manifest) { m.MinNGC = "99.0.0" })
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpd(t)
			// INTERMEDIATE: before the bend this very fixture goes through, so
			// the refusal below is the bend and not an earlier wall.
			if err := newUpd(t).run(); err != nil {
				t.Fatalf("the unbent fixture does not install: %v", err)
			}
			c.bend(u)
			if err := u.run(); !errors.Is(err, ErrVerify) {
				t.Fatalf("%v, want ErrVerify", err)
			}
			if u.row().Version != testVer {
				t.Errorf("the row says %q, want the old version", u.row().Version)
			}
			if len(u.s.sys.calls) != 0 {
				t.Errorf("this machine was changed: %v", u.s.sys.calls)
			}
		})
	}
}

// TestUpdateRefusesNeedsThatAreNotTheManifests is the join between the caller's
// read of the package and this one. The caller checks the grants against what
// Needs answered; if the manifest says something else, the container would get
// rights nobody held against anything.
func TestUpdateRefusesNeedsThatAreNotTheManifests(t *testing.T) {
	cases := []struct {
		name  string
		needs []string
	}{
		{"fewer than the manifest", []string{}},
		{"more than the manifest", []string{store.NeedNetwork, store.NeedRoot}},
		{"another one", []string{store.NeedRoot}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpd(t)
			u.in.Needs = c.needs
			if err := u.run(); !errors.Is(err, ErrVerify) {
				t.Fatalf("%v, want ErrVerify", err)
			}
			if len(u.s.sys.calls) != 0 {
				t.Errorf("this machine was changed: %v", u.s.sys.calls)
			}
		})
	}
}

// TestUpdateOnlyOfAnAppThatIsInstalled: an update is an update of something.
func TestUpdateOnlyOfAnAppThatIsInstalled(t *testing.T) {
	cases := []struct {
		name string
		bend func(u *upd)
	}{
		{"no row at all", func(u *upd) { delete(u.s.apps.rows, testApp) }},
		{"a row of kept data alone", func(u *upd) {
			row := u.s.apps.rows[testApp]
			row.Version = ""
			u.s.apps.rows[testApp] = row
		}},
		{"a row without a store", func(u *upd) {
			row := u.s.apps.rows[testApp]
			row.Store = ""
			u.s.apps.rows[testApp] = row
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpd(t)
			c.bend(u)
			if err := u.run(); !errors.Is(err, ErrForbidden) {
				t.Fatalf("%v, want ErrForbidden", err)
			}
			if len(u.s.sys.calls) != 0 {
				t.Errorf("this machine was changed: %v", u.s.sys.calls)
			}
		})
	}
}

// TestUpdateWithServicesNeedsTheRoleServices: a server that runs no app
// containers does not start one through an update either.
func TestUpdateWithServicesNeedsTheRoleServices(t *testing.T) {
	// INTERMEDIATE: with the role, this very fixture goes through - so the
	// refusal below is the role and not a wall in front of it.
	if err := newUpd(t).run(); err != nil {
		t.Fatalf("the unbent fixture does not install: %v", err)
	}
	u := newUpd(t)
	u.s.deps.Roles = []string{tunnel.RoleWeb}
	if err := u.run(); !errors.Is(err, ErrForbidden) {
		t.Fatalf("%v, want ErrForbidden", err)
	}
	if len(u.s.sys.calls) != 0 {
		t.Errorf("this machine was changed: %v", u.s.sys.calls)
	}
}

// TestUpdateLeavesTheOldVersionInTheRowAtEveryStep holds the standard the
// program swap of an NGC part was measured against
// (update.TestInstallKeepsOneWholeProgramAtEveryStep): a broken-off update
// never counts as a finished one, and running it again sets the new version
// up over whatever the broken one left.
//
// Every step of the setup that can fail is one case, and the list is the steps
// in their order - a step added to setUp without a case here is seen by the
// count below, not by a server.
func TestUpdateLeavesTheOldVersionInTheRowAtEveryStep(t *testing.T) {
	// The steps of setUp that can fail, in order. A step more or less than
	// this and the count at the end of this case fails.
	const steps = 7
	cases := []struct {
		name string
		fail func(u *upd)
	}{
		{"the system user", func(u *upd) { u.s.sys.fail = "EnsureUser" }},
		{"the data directory", func(u *upd) { u.s.sys.fail = "EnsureData" }},
		{"the runtime directory", func(u *upd) { u.s.sys.fail = "EnsureRun" }},
		{"loading the image", func(u *upd) { u.s.eng.fail = "LoadImage" }},
		{"creating the container", func(u *upd) { u.s.eng.fail = "Create" }},
		{"starting the container", func(u *upd) { u.s.eng.fail = "Start" }},
		{"writing the row", func(u *upd) { u.s.apps.putErr = errors.New("the database says no") }},
	}
	if len(cases) != steps {
		t.Fatalf("%d cases for %d steps", len(cases), steps)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpd(t)
			c.fail(u)
			if err := u.run(); err == nil {
				t.Fatal("the update came off although this step failed")
			}
			// Not finished: the row still names the version that runs.
			if got := u.row().Version; got != testVer {
				t.Fatalf("the row says %q, want the old version %q", got, testVer)
			}
			if got := u.row().Grants; !slices.Equal(got, []string{store.NeedRoot, store.NeedNetwork}) {
				t.Errorf("the row says grants %v, want the old ones", got)
			}
			// The data of the app is never dropped by a failed update: it is
			// the one thing of an app that cannot be fetched again.
			if slices.Contains(u.s.sys.calls, "DropData") || slices.Contains(u.s.sys.calls, "DropUser") {
				t.Errorf("a failed update took user or data away: %v", u.s.sys.calls)
			}
			// Nothing of the package is left lying about.
			if n := u.left(); n != 0 {
				t.Errorf("%d folders left under StageDir, want 0", n)
			}
			// And it can be run again: the retry sets the new version up over
			// whatever the failed one left behind.
			u.s.sys.fail, u.s.eng.fail, u.s.apps.putErr = "", "", nil
			if err := u.run(); err != nil {
				t.Fatalf("the retry after %s fails: %v", c.name, err)
			}
			if got := u.row().Version; got != testNewVer {
				t.Errorf("after the retry the row says %q, want %q", got, testNewVer)
			}
		})
	}
}

// TestNeedsReadsTheManifestAndChangesNothing: the call a caller makes before
// it refuses an update whose needs the app was not granted.
func TestNeedsReadsTheManifestAndChangesNothing(t *testing.T) {
	u := newUpd(t)
	got, err := New(u.s.deps).Needs(t.Context(), u.in.File)
	if err != nil {
		t.Fatalf("Needs: %v", err)
	}
	if !slices.Equal(got, []string{store.NeedNetwork}) {
		t.Errorf("needs %v, want the ones of the manifest", got)
	}
	if len(u.s.sys.calls) != 0 || len(u.s.eng.log()) != 0 {
		t.Errorf("Needs changed this machine: %v %v", u.s.sys.calls, u.s.eng.log())
	}
	if u.row().Version != testVer {
		t.Error("Needs wrote a row")
	}

	t.Run("a file that is no package", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad")
		if err := os.WriteFile(bad, []byte("not a tar at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(u.s.deps).Needs(t.Context(), bad); err == nil {
			t.Fatal("a file that is no package answered needs")
		}
	})

	t.Run("a file that is not there", func(t *testing.T) {
		_, err := New(u.s.deps).Needs(t.Context(), filepath.Join(t.TempDir(), "nothing"))
		if !errors.Is(err, ErrDisk) {
			t.Fatalf("%v, want ErrDisk", err)
		}
	})
}

// TestUpdateOfASealedPackageIsRefused measures the consequence named in
// update.go: the content key of a paid app arrives in the transfer header, and
// laying an update ready keeps no header - so a sealed file is read without a
// key and refused. It is NOT refused by a check of its own, and this case says
// which answer it really gets, so nobody reads a wall into it that is not
// there.
func TestUpdateOfASealedPackageIsRefused(t *testing.T) {
	u := newUpd(t)
	key := bytes.Repeat([]byte{0x5a}, 32)
	u.setPackage(func(b []byte) []byte { return encrypt(t, b, key) })
	// The checksum of the sealed bytes, so the case gets past sameFile and
	// measures the unpacking and not the earlier wall.
	sealed, err := os.ReadFile(u.in.File)
	if err != nil {
		t.Fatal(err)
	}
	u.in.SHA256 = sha(sealed)
	err = u.run()
	if !errors.Is(err, store.ErrPackage) {
		t.Fatalf("%v, want store.ErrPackage", err)
	}
	if len(u.s.sys.calls) != 0 {
		t.Errorf("this machine was changed: %v", u.s.sys.calls)
	}
	if n := u.left(); n != 0 {
		t.Errorf("%d folders left under StageDir, want 0", n)
	}
}

// TestUpdateIsNotWiredUp: everything of an AppUpdate comes out of this
// controller's own rows, so an empty field is this program wired wrong and
// gets one answer.
func TestUpdateIsNotWiredUp(t *testing.T) {
	cases := []struct {
		name string
		bend func(u *upd)
	}{
		{"no app", func(u *upd) { u.in.App = "" }},
		{"no version", func(u *upd) { u.in.Version = "" }},
		{"no checksum", func(u *upd) { u.in.SHA256 = nil }},
		{"half a checksum", func(u *upd) { u.in.SHA256 = u.in.SHA256[:16] }},
		{"no file", func(u *upd) { u.in.File = "" }},
		{"no stage directory", func(u *upd) { u.s.deps.StageDir = "" }},
		{"no clock", func(u *upd) { u.s.deps.Now = nil }},
		{"no engine", func(u *upd) { u.s.deps.Engine = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUpd(t)
			c.bend(u)
			if err := u.run(); !errors.Is(err, ErrInternal) {
				t.Fatalf("%v, want ErrInternal", err)
			}
		})
	}
	t.Run("no manager at all", func(t *testing.T) {
		var m *Manager
		if err := m.Update(context.Background(), AppUpdate{}); !errors.Is(err, ErrInternal) {
			t.Fatalf("%v, want ErrInternal", err)
		}
		if _, err := m.Needs(context.Background(), "x"); !errors.Is(err, ErrInternal) {
			t.Fatalf("%v, want ErrInternal", err)
		}
	})
}
