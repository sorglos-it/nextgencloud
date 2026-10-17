package state

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestTwoBackupsAtOnce zieht zwei Sicherungen desselben Stores gleichzeitig.
// Das ist der Fall, den ein Cron und ein Admin von Hand zusammen herstellen:
// zwei Aufrufe auf einer Datei, zwei Ziele.
func TestTwoBackupsAtOnce(t *testing.T) {
	cfg := stateWithAnApp(t)
	dir := filepath.Dir(cfg.Path)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Backup(cfg, filepath.Join(dir, "sicherung-"+string(rune('a'+i))+".db"))
		}(i)
	}
	wg.Wait()
	// Beide duerfen gelingen oder eine an der Schreibsperre scheitern - was
	// hier NICHT passieren darf, ist ein Wettlauf im Programm.
	for i, err := range errs {
		if err != nil {
			t.Logf("Sicherung %d: %v", i, err)
		}
	}
}
