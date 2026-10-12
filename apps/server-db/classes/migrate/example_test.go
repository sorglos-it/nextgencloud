package migrate

// config/migrate.json.example is what the controller with role db writes to
// /run/ngc/ with mode 0600 before an update, hands to ngc-api migrate and
// deletes afterwards. The file is documentation, and documentation that
// nothing reads goes stale: this case reads it.

import (
	"os"
	"slices"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
	"github.com/sorglos-it/nextgencloud/shared/enc"
)

// TestTheExampleIsTheMigrateBlockOfAControllerWithRoleDB holds the one
// sentence the example makes: it is exactly the migrate block of
// controller.json. So it is held against that block as the repository itself
// carries it, not against values written out here - a block that changes and
// an example that does not would otherwise stay green.
//
// Only password and ca may differ, and they have to: the example is checked
// into the repository, so it carries a placeholder where the controller
// carries the access to the database.
func TestTheExampleIsTheMigrateBlockOfAControllerWithRoleDB(t *testing.T) {
	var example config.DB
	body, err := os.ReadFile("../../config/migrate.json.example")
	if err != nil {
		t.Fatal(err)
	}
	// The real reader of such a file is strict about unknown, repeated and
	// differently spelled member names, so the example goes through the same
	// door: a field the configuration does not have would fail here.
	if err := enc.DecodeStrict(body, config.MaxConfig, &example); err != nil {
		t.Fatalf("the example is no config.DB: %v", err)
	}
	ctl, err := config.LoadController("../../../../shared/testdata/config/controller-db.json")
	if err != nil {
		t.Fatal(err)
	}
	if ctl.Migrate == nil {
		t.Fatal("the controller of the testdata has no migrate block, so there is nothing to compare")
	}
	// The example cannot pass config.DB.Validate as it stands, and that is
	// the point of it: its ca is three dots, not a certificate. So the claim
	// is made exactly: with a real CA in place of the placeholder, and
	// nothing else touched, every other field of the example is one the
	// configuration accepts.
	if example.Validate() == nil {
		t.Error("the example validates with a placeholder for the CA, so the placeholder is not being read")
	}
	filled := example
	filled.CA = ctl.Migrate.CA
	if err := filled.Validate(); err != nil {
		t.Errorf("the example does not pass config.DB.Validate with a real CA: %v", err)
	}
	if example.Driver != ctl.Migrate.Driver || example.Name != ctl.Migrate.Name ||
		example.User != ctl.Migrate.User || !slices.Equal(example.Nodes, ctl.Migrate.Nodes) {
		t.Errorf("example %q/%q/%q/%v is not the migrate block %q/%q/%q/%v",
			example.Driver, example.Name, example.User, example.Nodes,
			ctl.Migrate.Driver, ctl.Migrate.Name, ctl.Migrate.User, ctl.Migrate.Nodes)
	}
	// And the placeholder stays a placeholder: whoever copies the example has
	// to put the password of the installation in, and a real one must never
	// be committed here.
	if example.Password.Text() == ctl.Migrate.Password.Text() {
		t.Error("the example carries the same password as the controller of the testdata")
	}
	if example.CA == ctl.Migrate.CA {
		t.Error("the example carries a real CA certificate instead of the placeholder")
	}
}
