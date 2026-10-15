package appstore

import (
	"context"
	"errors"
	"slices"

	"github.com/sorglos-it/nextgencloud/shared/core"
	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/enc"
	"github.com/sorglos-it/nextgencloud/shared/sign"
	"github.com/sorglos-it/nextgencloud/shared/store"
	"github.com/sorglos-it/nextgencloud/shared/trust"
	"github.com/sorglos-it/nextgencloud/shared/wire"
)

// maxReason bounds a reason in core.SourceInfo.Error. It is roomy enough for
// every text of reasons and short enough that no text of a store's could hide
// in it -- which cannot happen anyway, because the texts are a closed list.
const maxReason = 120

// The reasons ngc.store/sources shows, in German. They are a closed list and
// not the error of the store, because an administrator reads this line: a text
// a stranger could fill is a tool against that reader, and a line break in it
// would forge a second row. Each one says what nobody can mend by asking
// again, or what the store itself has to put right.
const (
	ReasonUnreachable = "Der Store hat nicht oder nicht brauchbar geantwortet."
	ReasonSignature   = "Die Unterschrift des Katalogs stimmt nicht."
	ReasonForeignKey  = "Der Katalog ist mit einem anderen Schlüssel unterschrieben als dem aus der Vertrauensliste."
	ReasonCatalog     = "Der Katalog ist fehlerhaft oder gehört zu einem anderen Store."
	ReasonRollback    = "Der Store hat einen älteren Katalog geliefert als den zuletzt gesehenen."
	ReasonEnvelope    = "Die Antwort des Stores ist kein gültiger Umschlag."
	ReasonOther       = "Der Katalog konnte nicht geprüft werden."
)

// reasons is the whole list, so a test can show that no other text ever
// reaches an administrator (TestTheReasonIsGermanAndBounded).
var reasons = []string{ReasonUnreachable, ReasonSignature, ReasonForeignKey, ReasonCatalog,
	ReasonRollback, ReasonEnvelope, ReasonOther}

// reasonFor turns a cause into one of reasons. The order matters: ErrRollback
// is checked before the general catalog fault, because it is the one an
// administrator has to see as an attack and not as a broken file.
func reasonFor(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, store.ErrRollback):
		return ReasonRollback
	case errors.Is(err, sign.ErrWrongKey):
		return ReasonForeignKey
	case errors.Is(err, sign.ErrBadSignature):
		return ReasonSignature
	case errors.Is(err, sign.ErrEnvelope), errors.Is(err, sign.ErrTooLarge):
		return ReasonEnvelope
	case errors.Is(err, store.ErrCatalog), errors.Is(err, store.ErrRelease):
		return ReasonCatalog
	case errors.Is(err, ErrStore):
		return ReasonUnreachable
	default:
		return ReasonOther
	}
}

// readSource reads the row behind SettingStore. The row is this API's own, and
// it is read back through the same grammar all the same: whoever can write the
// database writes it too, and a reason outside the closed list must never
// reach an administrator's screen.
func (st *Stores) readSource(ctx context.Context, id string) (sourceState, error) {
	b, err := st.S.DB.BlobGet(ctx, dbapi.BlobSetting, SettingStore+id)
	if err != nil {
		return sourceState{}, err
	}
	var s sourceState
	// Strict JSON within a budget, as for anything else read back: the row has
	// no array, so the budget only bounds its size.
	if err := (enc.Budget{Size: 4096}).Decode(b.Data, &s); err != nil {
		return sourceState{}, err
	}
	// An empty reason is the normal case and no text at all, so it is not put
	// to wire.ValidText: that function refuses the empty string.
	if s.Checked < 0 || (s.Error != "" && !wire.ValidText(s.Error, maxReason)) {
		return sourceState{}, ErrStore
	}
	// A text outside the closed list is dropped rather than shown: this
	// package writes none other, so whoever wrote it is unknown.
	if s.Error != "" && !slices.Contains(reasons, s.Error) {
		s.Error = ""
	}
	return s, nil
}

// Sources answers ngc.store/sources: one row per store of the trust list, in
// the order of the list (sorted by ID). A store NGC never reached is listed
// too, with nothing seen -- the row an administrator needs most is the one of
// the store that is silent.
func (st *Stores) Sources(ctx context.Context) ([]core.SourceInfo, error) {
	v, err := st.S.Trust(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.SourceInfo, 0, len(v.List.Stores))
	for _, s := range v.List.Stores {
		row := core.SourceInfo{ID: s.ID, Name: s.Name, URL: s.URL}
		seq, err := st.lastSeq(ctx, dbapi.BlobCatalog, s.ID)
		if err != nil {
			return nil, err
		}
		row.Seq = seq
		if state, err := st.readSource(ctx, s.ID); err == nil {
			row.Checked, row.Error = state.Checked, state.Error
		} else if !errors.Is(err, dbapi.ErrNotFound) {
			// A row that does not read is not a reason to hide the store: the
			// store is listed, and what the row claimed is dropped for this
			// package's own text. A store NGC has not tried yet has no row at
			// all, and that one keeps an empty line.
			row.Error = ReasonOther
		}
		out = append(out, row)
	}
	return out, nil
}

// Catalog answers ngc.store/catalog: the stored envelope of one store of the
// trust list, and the catalog inside it.
//
// The envelope is verified once more on the way out, against the key the trust
// list names today. The row was verified when it was written, but a key an
// admin has since withdrawn would otherwise go on serving apps out of a
// catalog nobody trusts any more -- and the row is in a database, which is not
// where trust lives. The shell checks it a third time, against its own copy of
// the list (core.CatalogResult), so it never has to believe this API at all.
func (st *Stores) Catalog(ctx context.Context, id string) (*store.Catalog, sign.Envelope, error) {
	v, err := st.S.Trust(ctx)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	var s *trust.Store
	for i := range v.List.Stores {
		if v.List.Stores[i].ID == id {
			s = &v.List.Stores[i]
			break
		}
	}
	// An ID that is not in the list is not looked up at all: it comes from a
	// request, and a database query built from it is a query built from input.
	if s == nil {
		return nil, sign.Envelope{}, ErrStore
	}
	b, err := st.S.DB.BlobGet(ctx, dbapi.BlobCatalog, id)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	env, err := sign.ParseEnvelope(b.Data, sign.MaxPayload)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	// lastSeq is the row's own sequence, so a catalog stored at the right
	// number is not refused by its own record -- what this call must catch is
	// a signature, a key or a store that no longer fits, not the order.
	c, err := store.VerifyCatalog(env, *s, b.Seq)
	if err != nil {
		return nil, sign.Envelope{}, err
	}
	return c, env, nil
}
