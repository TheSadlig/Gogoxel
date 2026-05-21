//go:build godog

package bdd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	options := &godog.Options{
		Format:   "pretty",
		Paths:    []string{featuresDir()},
		Tags:     os.Getenv("GODOG_TAGS"),
		TestingT: t,
		Strict:   true,
	}
	suite := godog.TestSuite{
		Name:                "gogoxel-automation",
		ScenarioInitializer: InitializeScenario,
		Options:             options,
	}
	if suite.Run() != 0 {
		t.Fail()
	}
}

func featuresDir() string {
	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "features"))
}
