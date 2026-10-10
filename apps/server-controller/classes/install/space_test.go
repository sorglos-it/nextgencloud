package install

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// fakeDisk puts a disk of this case behind diskFree. Renamed on merge:
// install.disk is the production mapping to ErrDisk (package.go). The real answer is a call of
// Linux, so without this the arithmetic that decides what may be written would
// have no case at all where this is written.
func fakeDisk(t *testing.T, free int64, err error) {
	t.Helper()
	was := diskFree
	t.Cleanup(func() { diskFree = was })
	diskFree = func(string) (int64, error) { return free, err }
}

// The bound of the caller is cut down to what the disk carries twice over, and
// never the other way round.
func TestBoundFollowsTheDisk(t *testing.T) {
	const gib = int64(1) << 30
	cases := map[string]struct {
		free, max, want int64
		err             error
	}{
		// 100 GiB free: the caller's bound stands.
		"room for everything": {100 * gib, store.MaxPackage, store.MaxPackage, nil},
		// 1.5 GiB free, and the answer written out as a number: the reserve of
		// 512 MiB goes off and what is left is halved, because a package costs
		// twice its size while it is checked. Nothing of this line is computed
		// from StageReserve or StageFactor - a bound that is only held against
		// the constant it is made of is not held at all, and both constants
		// were measured to survive a change while this case still passed.
		"a gigabyte and a half free": {gib + 512<<20, store.MaxPackage, 512 << 20, nil},
		"two gigabytes free":         {2 * gib, store.MaxPackage, 768 << 20, nil},
		"600 MiB free":               {600 << 20, store.MaxPackage, 44 << 20, nil},
		// Exactly the reserve, and one byte above it: the sharp edge of the
		// refusal, from both sides.
		"exactly the reserve": {512 << 20, store.MaxPackage, 0, ErrSpace},
		// Two bytes above the reserve would be a bound of one byte, and then
		// every real package is "too large" instead of "the disk is full".
		// That is the full disk, and it has to read as one.
		"two bytes above it":           {512<<20 + 2, store.MaxPackage, 0, ErrSpace},
		"one byte below it":            {512<<20 - 1, store.MaxPackage, 0, ErrSpace},
		"room for a megabyte, twice":   {512<<20 + 2<<20, store.MaxPackage, 1 << 20, nil},
		"one byte less than that":      {512<<20 + 2<<20 - 2, store.MaxPackage, 0, ErrSpace},
		"a caller that asks for a bit": {512<<20 + 2<<20, 4711, 4711, nil},
		"nothing free at all":          {0, store.MaxPackage, 0, ErrSpace},
		"a caller's small bound":       {100 * gib, 1 << 20, 1 << 20, nil},
		"a bound below the disk":       {gib, 1 << 20, 1 << 20, nil},
		"a bound that is none":         {100 * gib, 0, 0, ErrInternal},
		"a bound above the limit":      {100 * gib, store.MaxPackage + 1, 0, ErrInternal},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fakeDisk(t, c.free, nil)
			got, err := bound(t.TempDir(), c.max)
			if !errors.Is(err, c.err) {
				t.Fatalf("bound answered %v, want %v", err, c.err)
			}
			if got != c.want {
				t.Fatalf("bound answered %d, want %d", got, c.want)
			}
		})
	}
}

// A disk that cannot be asked at all is a refusal and not a shrug: going on
// with the caller's bound is the one answer that can fill the disk.
func TestBoundRefusesADiskItCannotAsk(t *testing.T) {
	fakeDisk(t, 0, errors.New("the disk says nothing"))
	if _, err := bound(t.TempDir(), store.MaxPackage); !errors.Is(err, ErrSpace) {
		t.Fatalf("bound answered %v, want ErrSpace", err)
	}
	// And a system that has no such question keeps the caller's bound: that is
	// the machine this is written on, not a server.
	fakeDisk(t, 0, errNoSpaceCheck)
	got, err := bound(t.TempDir(), 4711)
	if err != nil || got != 4711 {
		t.Fatalf("bound answered %d, %v, want 4711, nil", got, err)
	}
}

// An activation on a disk that is too full is refused, and the mirror case is
// what makes this measure the disk: the same package goes through with room.
func TestActivateRefusesAPackageTheDiskCannotCarry(t *testing.T) {
	s := newStand(t)
	// A disk with the reserve and nothing else.
	fakeDisk(t, StageReserve, nil)
	if _, err := s.activate(); !errors.Is(err, ErrSpace) {
		t.Fatalf("a full disk answered %v, want ErrSpace", err)
	}
	if len(s.sys.calls) != 0 || len(s.eng.log()) != 0 {
		t.Fatalf("a full disk set something up: %v %v", s.sys.calls, s.eng.log())
	}
	if s.staged() != 0 {
		t.Fatal("a full disk staged something")
	}

	// And a disk with room for the file but not for twice it. The numbers are
	// written out rather than computed from the two constants: 512 MiB of
	// reserve, and twice the size of the package on top of it.
	// The bound has to stay above MinBound here, or the disk would be refused
	// as full and the case would measure that wall instead of the bound: so
	// the room is 2 MiB (the smallest bound this hands out) minus two bytes,
	// and the package is grown to just over that bound.
	s = newStand(t)
	s.pad = 1 << 20
	s.setPackage()
	size := int64(len(s.f.file))
	if size <= 1<<20 {
		t.Fatalf("the package of this case is %d bytes, and it has to be above MinBound", size)
	}
	fakeDisk(t, 512<<20+2*size-2, nil)
	if _, err := s.activate(); !errors.Is(err, ErrVerify) {
		t.Fatalf("a package one byte over the bound answered %v, want ErrVerify", err)
	}
	// The same package, two bytes of room more: it comes off. Without this the
	// case above would pass for any reason at all.
	s2 := newStand(t)
	s2.pad = 1 << 20
	s2.setPackage()
	fakeDisk(t, 512<<20+2*size, nil)
	if _, err := s2.activate(); err != nil {
		t.Fatalf("a package exactly within the bound: %v", err)
	}
}

// The two numbers the decision of space.go consists of, pinned. They are a
// decision and not a measurement (space.go), so a change to either is
// deliberate - and this is the case that makes it deliberate instead of quiet.
// Every case above holds its own numbers against them without naming them, so
// this one says what they are.
func TestTheSpaceDecisionIsWhatItSays(t *testing.T) {
	if StageReserve != 512<<20 {
		t.Fatalf("the reserve is %d bytes, and this case was written for 512 MiB", StageReserve)
	}
	if StageFactor != 2 {
		t.Fatalf("a package costs %dx its size, and this case was written for 2", StageFactor)
	}
	if MinBound != 1<<20 {
		t.Fatalf("the smallest bound is %d bytes, and this case was written for 1 MiB", MinBound)
	}
}

// The real answer of the system, on the disk this test runs on. It measures
// the one thing the stand-in cannot: that freeSpace answers at all, and a
// number that is not nonsense.
func TestFreeSpaceAnswersOnThisDisk(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("how much room is left is a call of Linux; NGC runs on Debian and Ubuntu")
	}
	free, err := freeSpace(t.TempDir())
	if err != nil {
		t.Fatalf("freeSpace: %v", err)
	}
	if free <= 0 {
		t.Fatalf("freeSpace answered %d bytes free", free)
	}
	// A directory that is not there yet is the normal case: config.StageDir is
	// made by Accept, and the question is about the filesystem under it.
	missing := filepath.Join(t.TempDir(), "not", "there", "yet")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("the directory of this case exists")
	}
	alsoFree, err := freeSpace(missing)
	if err != nil {
		t.Fatalf("freeSpace of a directory that is not there: %v", err)
	}
	if alsoFree <= 0 {
		t.Fatalf("freeSpace of a directory that is not there answered %d", alsoFree)
	}
}
