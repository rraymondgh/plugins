package plugins

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/rraymondgh/plugins/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

//go:embed testdata/radarr_424783.json
var radarr424783 []byte

//go:embed testdata/radarr_1895.json
var radarr1895 []byte

//go:embed testdata/radarr_339846.json
var radarr339846 []byte

func TestAdapter(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.InfoLevel))
	undo := zap.ReplaceGlobals(logger)

	radarrmock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("tmdbId") {
		case "424783":
			_, err = w.Write(radarr424783)
		case "1895":
			_, err = w.Write(radarr1895)
		case "339846":
			_, err = w.Write(radarr339846)
		}

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	t.Cleanup(func() {
		undo()
		radarrmock.Close()
	})
	t.Log(test.PluginTestDataDir(1))
	test.BuildTestPlugins(t)

	cfg := TestConfig()
	cfg.Folder = test.PluginTestDataDir(1)
	cfg.CacheFolder = t.TempDir()
	cfg.PluginLogging = true
	mgr := TestManager(metrics.NewNoopInstance(), cfg)
	mgr.ScanPlugins()

	err := mgr.EnsureCompiled("multi_plugin")
	require.NoError(t, err)
	err = mgr.EnsureCompiled("fake_test")
	require.NoError(t, err)
	err = mgr.EnsureCompiled("fake_test_as")
	require.NoError(t, err)
	// pluginName := "fake_test_as"

	pluginRun := func(pluginName string, n int, tmdbid string) {
		test := mgr.LoadPlugin(pluginName, CapabilityIntegrationTest)
		assert.NotNil(t, test)
		assert.Equal(t, pluginName, test.PluginID())
		testRPC := test.(*wasmTest)

		t.Run(fmt.Sprintf("[%v] sendTo[%v] %s", pluginName, n, tmdbid), func(t *testing.T) {
			t.Parallel()

			info, err := testRPC.ConfigTest(t.Context(), pluginName)
			require.NoError(t, err)
			assert.NotNil(t, info)
			assert.Equal(t, pluginName, info)

			info, err = testRPC.HTTPTest(
				t.Context(),
				fmt.Sprintf("%s?tmdbId=", radarrmock.URL),
				"secret",
				tmdbid,
			)
			require.NoError(t, err)
			assert.NotNil(t, info)
			assert.Equal(t, fmt.Sprintf("api result: %s", tmdbid), info)
		})
	}
	tmdbids := []string{"424783", "1895", "339846"}
	// tmdbids := []string{"424783"}
	for n := range 200 {
		for _, tmdbid := range tmdbids {
			pluginRun("fake_test_as", n, tmdbid)
			pluginRun("fake_test", n, tmdbid)
		}
	}
}
