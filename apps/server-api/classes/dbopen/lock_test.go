package dbopen

import (
	"context"
	"sync"
	"testing"

	"github.com/sorglos-it/nextgencloud/shared/config"
)

// TestBackupSurvivesARegistrationAtTheSameTime drives the one shared value of
// this seam from two goroutines: the init phase sets it, and a program that
// backs up reads it. Nothing in production does both at once today, so this
// case is what makes the lock measurable at all.
func TestBackupSurvivesARegistrationAtTheSameTime(t *testing.T) {
	t.Cleanup(func() { RegisterBackuper(nil) })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			RegisterBackuper(func(context.Context, config.DB, string) error { return nil })
		}()
		go func() { defer wg.Done(); _ = Backup(context.Background(), config.DB{}, "/tmp/b.db") }()
	}
	wg.Wait()
}
