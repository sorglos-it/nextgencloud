package memstore_test

import (
	"testing"

	"github.com/sorglos-it/nextgencloud/contracts/dbapi"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi/dbtest"
	"github.com/sorglos-it/nextgencloud/contracts/dbapi/memstore"
)

func TestMemstore(t *testing.T) {
	dbtest.Run(t, func(t *testing.T) dbapi.Store { return memstore.New() })
}
