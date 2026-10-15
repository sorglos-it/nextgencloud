package appstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/proof"
	"github.com/sorglos-it/nextgencloud/shared/trust/trusttest"
)

// proofVectors is shared/testdata/uiproof.json: twenty-three UI proofs made
// against the trust list newStores builds, four of which hold. Not one of them
// is made here, so no case can pass by signing its own evidence.
type proofVectors struct {
	Cases []proofCase `json:"cases"`
}

type proofCase struct {
	Name    string    `json:"name"`
	App     string    `json:"app"`
	Version string    `json:"version"`
	UI      enc.Bytes `json:"ui"` // the hash a valid case proves
	Valid   bool      `json:"valid"`
	Proof   enc.Bytes `json:"proof"` // the UIProof as JSON
}

// The split of uiproof.json as numbers of its own: a vector file that lost its
// rejects would otherwise still read as a full run.
const (
	wantProofs      = 23
	wantValidProofs = 4
	// The rejects that name the app and the version they are asked about;
	// the four others are proofs that hold for another app or version.
	wantInwardRejects = 15
)

func loadProofVectors(t *testing.T) proofVectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "shared", "testdata", "uiproof.json"))
	if err != nil {
		t.Fatalf("uiproof.json: %v", err)
	}
	var v proofVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("uiproof.json: %v", err)
	}
	valid := 0
	for _, c := range v.Cases {
		if c.Valid {
			valid++
		}
	}
	if len(v.Cases) != wantProofs || valid != wantValidProofs {
		t.Fatalf("vectors: %d cases, %d of them valid; want %d and %d",
			len(v.Cases), valid, wantProofs, wantValidProofs)
	}
	return v
}

// caseOf is the vector of that name.
func (v proofVectors) caseOf(t *testing.T, name string) proofCase {
	t.Helper()
	for _, c := range v.Cases {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no proof vector named %q", name)
	return proofCase{}
}

// uiProof is the vector read by encoding/json and not by core.ParseUIProof:
// what KeepProof has to refuse is exactly what that parser refuses, so a case
// that put its vector through it first would measure the parser instead.
func (c proofCase) uiProof(t *testing.T) core.UIProof {
	t.Helper()
	var p core.UIProof
	if err := json.Unmarshal(c.Proof, &p); err != nil {
		t.Fatalf("vector %q: %v", c.Name, err)
	}
	return p
}

// The two vectors the cases below work with by name.
const (
	uploadedProof = "uploaded app activated by the admin"
	storeAppProof = "store app from the global store"
)

// TestKeepUIKeepsTheBytesAsTheyAre: ngc.desktop/ui sends what is kept here
// byte for byte, so KeepUI must not touch one of them, and they must land
// under "<app>@<version>" -- the ID the shell's request is answered from.
func TestKeepUIKeepsTheBytesAsTheyAre(t *testing.T) {
	st, s := newStores(t)
	ui := []byte("ui-tar bytes, no JSON, not padded by us\x00\x01")
	if err := st.KeepUI(ctx, "org.example.notes", "0.1.0", ui); err != nil {
		t.Fatalf("KeepUI: %v", err)
	}
	b, err := s.DB.BlobGet(ctx, dbapi.BlobUI, "org.example.notes@0.1.0")
	if err != nil {
		t.Fatalf("BlobGet: %v", err)
	}
	if !bytes.Equal(b.Data, ui) {
		t.Fatalf("kept %q, want %q", b.Data, ui)
	}
	if b.Updated != testNow {
		t.Fatalf("Updated %d, want %d", b.Updated, testNow)
	}
}

// TestKeepProofTakesOnlyWhatHolds: every vector of uiproof.json through
// KeepProof and then out again through Proof, asked for the app and the
// version of the case. Only the four that hold come back.
//
// The question of the vectors is the pair of row and question, not the row
// alone: four of the nineteen rejects are proofs that hold for themselves and
// name another app or another version than the one asked for. KeepProof takes
// those -- they are evidence of the version they name -- and Proof refuses
// them, because it is asked about a version they say nothing about. A case
// that only measured KeepProof would call those four a finding; one that only
// measured Proof would not see that a malformed row is never written at all,
// so both are measured here.
func TestKeepProofTakesOnlyWhatHolds(t *testing.T) {
	v := loadProofVectors(t)
	held, inward := 0, 0
	for _, c := range v.Cases {
		// One fixture per case: a row of an earlier case would otherwise
		// stand in for the row of this one.
		st, s := newStores(t)
		p := c.uiProof(t)
		keep := st.KeepProof(ctx, p)
		// A reject that speaks of the very app and version it is asked about
		// has to be refused on the way in: for those KeepProof is the wall,
		// and Proof refusing them later would say nothing about it.
		if !c.Valid && p.App == c.App && p.Version == c.Version {
			inward++
			if keep == nil {
				t.Errorf("%s: written, and only Proof refused it", c.Name)
			}
		}
		rows, rerr := s.DB.BlobFrom(ctx, dbapi.BlobApproval, 0)
		if rerr != nil {
			t.Fatalf("%s: BlobFrom: %v", c.Name, rerr)
		}
		if keep != nil && len(rows) != 0 {
			// Refused on the way in: not a row anywhere, under any ID.
			t.Errorf("%s: refused with %v, yet %d rows exist", c.Name, keep, len(rows))
		}
		for _, row := range rows {
			// A row that was written is bytes the contract's parser reads
			// back: that is what ngc.desktop/ui-proof does with it, and it
			// cannot ask whoever wrote it to try again.
			if _, perr := core.ParseUIProof(row.Data); perr != nil {
				t.Errorf("%s: the kept row %q does not read back: %v", c.Name, row.ID, perr)
			}
		}
		_, out := st.Proof(ctx, c.App, c.Version)
		switch {
		case c.Valid && keep != nil:
			t.Errorf("%s: refused a proof that holds: %v", c.Name, keep)
		case c.Valid && out != nil:
			t.Errorf("%s: kept, yet Proof says %v", c.Name, out)
		case c.Valid:
			held++
		case out == nil:
			t.Errorf("%s: Proof answered for %s %s", c.Name, c.App, c.Version)
		}
	}
	if held != wantValidProofs {
		t.Fatalf("%d proofs came back, want %d", held, wantValidProofs)
	}
	if inward != wantInwardRejects {
		t.Fatalf("%d rejects speak of the app they are asked about, want %d", inward, wantInwardRejects)
	}
}

// TestProofOfAnUploadedApp: the evidence of an uploaded app is the activate
// task with the client certificate and the user's proof over it. Proof hands
// it out only while core.VerifyUIProof confirms it against the trust list, so
// once the admin's key is replaced the very same row is no evidence any more.
func TestProofOfAnUploadedApp(t *testing.T) {
	v := loadProofVectors(t)
	p := v.caseOf(t, uploadedProof).uiProof(t)
	if p.Store != core.LocalStore || p.Task == nil || len(p.Client) == 0 || p.Proof == nil {
		t.Fatalf("the vector is not the evidence of an uploaded app: %+v", p)
	}
	st, s := newStores(t)
	if err := st.KeepProof(ctx, p); err != nil {
		t.Fatalf("KeepProof: %v", err)
	}
	// The way is open: against the list that signed it the proof comes back.
	// Without this step the refusal below would prove nothing.
	got, err := st.Proof(ctx, p.App, p.Version)
	if err != nil {
		t.Fatalf("Proof: %v", err)
	}
	if got.App != p.App || got.Version != p.Version || got.Task == nil || !bytes.Equal(got.Client, p.Client) {
		t.Fatalf("Proof answered %+v", got)
	}
	before, err := s.DB.BlobGet(ctx, dbapi.BlobApproval, p.App+"@"+p.Version)
	if err != nil {
		t.Fatalf("BlobGet: %v", err)
	}
	// A next version of the list in which the admin who approved the install
	// has another key. The row itself is not touched.
	admin := trusttest.NewSigner(1)
	next := nextVersion(t, s)
	replaced := false
	for i := range next.Users {
		if next.Users[i].Name == "admin" {
			next.Users[i].Keys = []proof.UserKey{trusttest.NewSigner(0x42).Key}
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("no user admin in the list")
	}
	if _, err := s.AddVersion(ctx, admin.Sign(t, next)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	after, err := s.DB.BlobGet(ctx, dbapi.BlobApproval, p.App+"@"+p.Version)
	if err != nil || !bytes.Equal(after.Data, before.Data) {
		t.Fatalf("the stored row changed: %v", err)
	}
	if _, err := st.Proof(ctx, p.App, p.Version); !errors.Is(err, core.ErrUIProof) {
		t.Fatalf("after the key change: got %v, want core.ErrUIProof", err)
	}
}

// TestProofAndUITarMustAgree: a proof is about one UI. Where the ui-tar of
// that version is here too, Proof compares what the proof vouches for against
// what ngc.desktop/ui would send, so bytes nobody signed for that app are not
// handed out with evidence for other bytes.
func TestProofAndUITarMustAgree(t *testing.T) {
	v := loadProofVectors(t)
	c := v.caseOf(t, uploadedProof)
	p := c.uiProof(t)
	st, s := newStores(t)
	if err := st.KeepProof(ctx, p); err != nil {
		t.Fatalf("KeepProof: %v", err)
	}
	// With no ui-tar here the proof stands: that is the state a version has
	// before its ui-tar is written, and it is the step that shows the
	// comparison below is what refuses.
	if _, err := st.Proof(ctx, p.App, p.Version); err != nil {
		t.Fatalf("without a ui-tar: %v", err)
	}
	other := []byte("some other ui-tar")
	sum := sha256.Sum256(other)
	if bytes.Equal(sum[:], c.UI) {
		t.Fatal("the bytes of this case hash to the proven UI; pick others")
	}
	if err := st.KeepUI(ctx, p.App, p.Version, other); err != nil {
		t.Fatalf("KeepUI: %v", err)
	}
	if _, err := st.Proof(ctx, p.App, p.Version); !errors.Is(err, core.ErrUIProof) {
		t.Fatalf("with a ui-tar of other bytes: got %v, want core.ErrUIProof", err)
	}
	if err := s.DB.BlobDelete(ctx, dbapi.BlobUI, p.App+"@"+p.Version); err != nil {
		t.Fatalf("BlobDelete: %v", err)
	}
	if _, err := st.Proof(ctx, p.App, p.Version); err != nil {
		t.Fatalf("after the ui-tar is gone: %v", err)
	}
}

// TestProofOfAnAppWithoutOne: nothing is invented for a version no evidence
// was kept for.
func TestProofOfAnAppWithoutOne(t *testing.T) {
	st, _ := newStores(t)
	if _, err := st.Proof(ctx, "org.example.notes", "0.1.0"); !errors.Is(err, dbapi.ErrNotFound) {
		t.Fatalf("got %v, want dbapi.ErrNotFound", err)
	}
}

// TestProofOfAStoreApp: the evidence of a store app is the catalog of a store
// of the list, and a store the list no longer carries vouches for nothing.
func TestProofOfAStoreApp(t *testing.T) {
	v := loadProofVectors(t)
	p := v.caseOf(t, storeAppProof).uiProof(t)
	st, s := newStores(t)
	if err := st.KeepProof(ctx, p); err != nil {
		t.Fatalf("KeepProof: %v", err)
	}
	if _, err := st.Proof(ctx, p.App, p.Version); err != nil {
		t.Fatalf("Proof: %v", err)
	}
	admin := trusttest.NewSigner(1)
	next := nextVersion(t, s)
	next.Stores = nil
	if _, err := s.AddVersion(ctx, admin.Sign(t, next)); err != nil {
		t.Fatalf("version 2: %v", err)
	}
	if _, err := st.Proof(ctx, p.App, p.Version); !errors.Is(err, core.ErrUIProof) {
		t.Fatalf("without the store in the list: got %v, want core.ErrUIProof", err)
	}
}
