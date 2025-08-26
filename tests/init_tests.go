package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var once sync.Once

func Init(t *testing.T, skipOnShort bool) {
	t.Helper()

	if skipOnShort && testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	once.Do(func() {
		_, file, _, _ := runtime.Caller(0)
		appPath, _ := filepath.Abs(filepath.Join(filepath.Dir(file), ".."))
		_ = os.Chdir(appPath) //nolint:usetesting
	})
}
