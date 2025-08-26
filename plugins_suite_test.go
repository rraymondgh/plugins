package plugins

import (
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rraymondgh/plugins/tests"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const testDataDir = "testdata"

func TestPlugins(t *testing.T) {
	tests.Init(t, false)
	t.Parallel()

	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.TimeKey = ""
	encoderConfig.StacktraceKey = ""
	config.EncoderConfig = encoderConfig
	logger, _ := config.Build()
	zap.ReplaceGlobals(logger)
	buildTestPlugins(t, testDataDir)
	// log.SetLevel(log.LevelFatal)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugins Suite")
}

func buildTestPlugins(t *testing.T, path string) {
	t.Helper()
	t.Logf("[BeforeSuite] Current working directory: %s", path)
	cmd := exec.Command("make", "-C", path)
	out, err := cmd.CombinedOutput()
	t.Logf("[BeforeSuite] Make output: %s", string(out))

	if err != nil {
		t.Fatalf("Failed to build test plugins: %v", err)
	}
}
