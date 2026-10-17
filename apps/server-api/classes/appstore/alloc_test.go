package appstore

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"runtime"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/store"
)

// allocSink holds a reference allocation so the compiler cannot drop it.
var allocSink []byte

// heapGrowth reports how many bytes fn took from the heap. TotalAlloc only
// ever grows, so what is collected again still counts -- which is the point: a
// reader that asks for four gibibytes and frees them again has still asked for
// four gibibytes, and on a machine without them it dies before it can free
// anything.
func heapGrowth(fn func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	fn()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// tarHeader writes a tar header that promises size bytes and then stops. The
// body is deliberately missing: what is measured is whether the promise alone
// buys the memory.
func tarHeader(t *testing.T, name string, size int64) []byte {
	t.Helper()
	// The writer of the standard library refuses to hand out a header whose
	// body never comes, so the header is written with the real length and the
	// length field is then overwritten in the raw block: offset 124, twelve
	// bytes of octal with a trailing NUL. That is what a sender who lies
	// about the length puts on the wire.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: 0, Mode: 0o644}); err != nil {
		t.Fatalf("header: %v", err)
	}
	if err := tw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	b := buf.Bytes()
	if len(b) < 512 {
		t.Fatalf("header is %d bytes", len(b))
	}
	copy(b[124:136], []byte(fmt.Sprintf("%011o\x00", size)))
	// The checksum covers the block, so it is computed again over the lie.
	copy(b[148:156], []byte("        "))
	sum := 0
	for _, c := range b[:512] {
		sum += int(c)
	}
	copy(b[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
	// A header that promises bytes which never come is the whole point, so the
	// block stands alone with nothing behind it.
	return b[:512]
}

// TestManifestOfRefusesAPromiseItCannotAfford measures the exhaustion case of
// a declared length: a tar header that says the manifest is 4 GiB long, in a
// file of a few hundred bytes. The bound (store.MaxManifest) sits before the
// allocation, so the refusal must be cheap -- and "cheap" is a number here,
// not a reading of the code.
func TestManifestOfRefusesAPromiseItCannotAfford(t *testing.T) {
	const huge = int64(4) << 30
	head := tarHeader(t, store.ManifestFile, huge)

	var err error
	grew := heapGrowth(func() { _, err = manifestOf(bytes.NewReader(head)) })
	if err == nil {
		t.Fatalf("a %d-byte promise in %d bytes of file was accepted", huge, len(head))
	}
	// A megabyte of slack: the tar reader has its own buffers, and this is
	// about the difference between a bound and no bound, not about bytes.
	if grew > 1<<20 {
		t.Fatalf("refusing a %d-byte promise cost %d bytes of heap", huge, grew)
	}

	// The counter-check that the measurement can see the thing it looks for:
	// the same promise, honoured, does cost the memory. Without this the test
	// above would also pass if manifestOf never allocated at all.
	// allocSink keeps the reference allocation alive: assigning it to _ lets
	// the compiler drop it, and the probe then measures 0 bytes and declares
	// every bound sound.
	ref := heapGrowth(func() { allocSink = make([]byte, store.MaxManifest) })
	if ref < uint64(store.MaxManifest)/2 {
		t.Fatalf("the probe cannot see an allocation: %d bytes for %d", ref, store.MaxManifest)
	}

	// And the counter-check that the way to the bound is open at all: a
	// manifest just inside it is read, so the refusal above came from the size
	// and not from an earlier wall.
	ok := tarHeader(t, store.ManifestFile, store.MaxManifest)
	ok = append(ok, make([]byte, store.MaxManifest)...)
	if _, err := manifestOf(bytes.NewReader(ok)); err != nil {
		t.Fatalf("a manifest of exactly %d bytes was refused: %v", store.MaxManifest, err)
	}
}

// TestWalkRefusesAPromiseItCannotAfford is the same measurement one layer
// down, on the contract's own reader (store.Walk, shared/store/pack.go:68).
// It reads from shared/ and changes nothing there.
func TestWalkRefusesAPromiseItCannotAfford(t *testing.T) {
	const huge = int64(4) << 30
	head := tarHeader(t, store.ManifestFile, huge)

	var err error
	grew := heapGrowth(func() {
		_, err = store.Walk(bytes.NewReader(head), func(*store.Manifest, store.PackFile, io.Reader) error {
			return nil
		})
	})
	if err == nil {
		t.Fatalf("Walk accepted a %d-byte promise in %d bytes of file", huge, len(head))
	}
	if grew > 1<<20 {
		t.Fatalf("Walk spent %d bytes of heap refusing a %d-byte promise", grew, huge)
	}
}
