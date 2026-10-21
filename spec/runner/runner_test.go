package runner_test

import (
	"path/filepath"
	"testing"

	"github.com/gammazero/nexus/v3/spec/runner"
)

// TestSpecConformance walks every YAML plan under spec/plans/ and runs it
// as a Go subtest. Use `go test -run "TestSpecConformance/<plan-file>"` to
// run a single plan.
func TestSpecConformance(t *testing.T) {
	planDirs := []string{"basic"}
	for _, dir := range planDirs {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			runner.RunDir(t, filepath.Join("..", "plans", dir))
		})
	}
}
