package plugins

import (
	"context"

	"github.com/rraymondgh/plugins/api/testapi"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/tetratelabs/wazero"
	"go.uber.org/zap"
)

type IntegrationTestInterface interface {
	ConfigTest(ctx context.Context, id string) (string, error)
	HTTPTest(ctx context.Context, url string, apikey string, id string) (string, error)
}

// NewWasmMediaAgent creates a new adapter for a Client plugin
func newWasmTest(
	wasmPath, pluginID string,
	metrics metrics.Metrics,
	runtime func(context.Context) (wazero.Runtime, error),
	mc wazero.ModuleConfig,
) WasmPlugin {
	loader, err := testapi.NewIntegrationTestPlugin(
		context.Background(),
		testapi.WazeroRuntime(runtime),
		testapi.WazeroModuleConfig(mc),
	)
	if err != nil {
		zap.L().Error("Error creating test service plugin",
			zap.String("plugin", pluginID), zap.String("path", wasmPath), zap.Error(err))
		return nil
	}

	return &wasmTest{
		BaseCapability: NewBaseCapability[testapi.IntegrationTest, *testapi.IntegrationTestPlugin](
			wasmPath,
			pluginID,
			CapabilityIntegrationTest,
			metrics,
			loader,
			func(ctx context.Context, l *testapi.IntegrationTestPlugin, path string) (testapi.IntegrationTest, error) {
				return l.Load(ctx, path)
			},
		),
	}
}

// wasmClient adapts a Client plugin to implement the IntegrationTestInterface
type wasmTest struct {
	*BaseCapability[testapi.IntegrationTest, *testapi.IntegrationTestPlugin]
}

func (w *wasmTest) ConfigTest(ctx context.Context, id string) (string, error) {
	res, err := CallMethod(
		ctx,
		w,
		"ConfigTest",
		func(inst testapi.IntegrationTest) (*testapi.SimpleResponse, error) {
			return inst.ConfigTest(ctx, &testapi.SimpleRequest{
				ID: id,
			})
		},
	)
	if err != nil {
		return "", err
	}

	return res.GetMessage(), nil
}

func (w *wasmTest) HTTPTest(ctx context.Context, url string, apikey string, id string) (string, error) {
	res, err := CallMethod(
		ctx,
		w,
		"HttpTest",
		func(inst testapi.IntegrationTest) (*testapi.SimpleResponse, error) {
			return inst.HTTPTest(ctx, &testapi.HttpRequest{
				Url:    url,
				Apikey: apikey,
				Id:     id,
			})
		},
	)
	if err != nil {
		return "", err
	}

	return res.GetMessage(), nil
}
