package plugins

import (
	"context"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/tetratelabs/wazero"
	"go.uber.org/zap"
)

type IntegrationTestInterface interface {
	SendTo(ctx context.Context, id string) (string, error)
}

// NewWasmMediaAgent creates a new adapter for a Client plugin
func newWasmTest(
	wasmPath, pluginID string,
	metrics metrics.Metrics,
	runtime func(context.Context) (wazero.Runtime, error),
	mc wazero.ModuleConfig,
) WasmPlugin {
	loader, err := api.NewIntegrationTestPlugin(
		context.Background(),
		api.WazeroRuntime(runtime),
		api.WazeroModuleConfig(mc),
	)
	if err != nil {
		zap.L().Error("Error creating test service plugin",
			zap.String("plugin", pluginID), zap.String("path", wasmPath), zap.Error(err))
		return nil
	}

	return &wasmTest{
		BaseCapability: NewBaseCapability[api.IntegrationTest, *api.IntegrationTestPlugin](
			wasmPath,
			pluginID,
			CapabilityIntegrationTest,
			metrics,
			loader,
			func(ctx context.Context, l *api.IntegrationTestPlugin, path string) (api.IntegrationTest, error) {
				return l.Load(ctx, path)
			},
		),
	}
}

// wasmClient adapts a Client plugin to implement the IntegrationTestInterface
type wasmTest struct {
	*BaseCapability[api.IntegrationTest, *api.IntegrationTestPlugin]
}

func (w *wasmTest) SendTo(ctx context.Context, id string) (string, error) {
	res, err := CallMethod(ctx, w, "SendTo", func(inst api.IntegrationTest) (*api.TestSendToResponse, error) {
		return inst.SendTo(ctx, &api.TestSendToRequest{
			ID: id,
		})
	})
	if err != nil {
		return "", err
	}

	return res.GetMessage(), nil
}
