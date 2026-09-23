package state

import (
	"bytes"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/trust"
)

// FuzzRefresh feeds Refresh with rows a damaged or foreign database could
// hold. A row it accepts must be a version 1 of this installation that
// trust.Open verifies against the hash Refresh cached, and Chain must hand
// that very row back; anything else is an error, and nothing panics.
func FuzzRefresh(f *testing.F) {
	// The keys and the list are made once: doing it per run would cost more
	// than the code under test.
	id := genesisID(f)
	f.Add(genesisSeed(f))
	f.Add([]byte("{}"))
	f.Add([]byte(`{"payload":"","proof":{}}`))
	f.Add([]byte(`{"payload":"AA","proof":{"kind":"password","keyId":"x","sig":"AA"}}`))
	f.Fuzz(func(t *testing.T, row []byte) {
		s, db := newState(t)
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobSetting, ID: dbapi.SettingInstallation,
			Data: []byte(id), Updated: s.Now()}); err != nil {
			t.Fatalf("installation: %v", err)
		}
		if err := db.BlobPut(ctx, dbapi.Blob{Kind: dbapi.BlobTrust, ID: "1", Seq: 1, Data: row}); err != nil {
			t.Fatalf("put: %v", err)
		}
		v, err := s.Refresh(ctx)
		if err != nil {
			return
		}
		if v.List == nil || v.List.Seq != 1 {
			t.Fatalf("accepted a row that is no version 1: %v", v.List)
		}
		if v.List.Installation != id {
			t.Fatalf("accepted a version of installation %q", v.List.Installation)
		}
		sg, err := trust.ParseSigned(row)
		if err != nil {
			t.Fatalf("accepted a row trust.ParseSigned refuses: %v", err)
		}
		if !bytes.Equal(v.Hash, trust.Hash(sg)) {
			t.Fatal("cached hash is not the hash of the stored row")
		}
		// The cached version is the anchor this installation hands out, so it
		// has to verify on its own.
		if _, err := trust.Open(trust.Anchor{Seq: 1, Hash: v.Hash}, []trust.Signed{sg}); err != nil {
			t.Fatalf("accepted a version trust.Open refuses: %v", err)
		}
		again, err := s.Refresh(ctx)
		if err != nil || again.List == nil || again.List.Seq != 1 || !bytes.Equal(again.Hash, v.Hash) {
			t.Fatalf("second refresh walked back: %v %v", again.List, err)
		}
		c, err := s.Chain(ctx, 1)
		if err != nil || len(c.Versions) != 1 || !bytes.Equal(c.Versions[0].Payload, sg.Payload) {
			t.Fatalf("chain from 1: %v %d", err, len(c.Versions))
		}
	})
}
