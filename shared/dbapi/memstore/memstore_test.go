package memstore_test

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/dbapi"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/dbtest"
	"github.com/sorglos-it/nextgencloud/shared/dbapi/memstore"
)

func TestMemstore(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) dbapi.Store { return memstore.New() })
}
