package plugins

import (
	"context"

	"github.com/rraymondgh/plugins/api/websocketapi"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/tetratelabs/wazero"
	"go.uber.org/zap"
)

// newWasmWebSocketCallback creates a new adapter for a WebSocketCallback plugin
func newWasmWebSocketCallback(
	wasmPath, pluginID string,
	metrics metrics.Metrics,
	runtime func(context.Context) (wazero.Runtime, error),
	mc wazero.ModuleConfig,
) WasmPlugin {
	loader, err := websocketapi.NewWebSocketCallbackPlugin(
		context.Background(),
		websocketapi.WazeroRuntime(runtime),
		websocketapi.WazeroModuleConfig(mc),
	)
	if err != nil {
		zap.L().Error("Error creating WebSocket callback plugin",
			zap.String("plugin", pluginID), zap.String("path", wasmPath), zap.Error(err))
		return nil
	}

	return &wasmWebSocketCallback{
		BaseCapability: NewBaseCapability[
			websocketapi.WebSocketCallback,
			*websocketapi.WebSocketCallbackPlugin,
		](
			wasmPath,
			pluginID,
			CapabilityWebSocketCallback,
			metrics,
			loader,
			func(
				ctx context.Context,
				l *websocketapi.WebSocketCallbackPlugin,
				path string,
			) (websocketapi.WebSocketCallback, error) {
				return l.Load(ctx, path)
			},
		),
	}
}

// wasmWebSocketCallback adapts a WebSocketCallback plugin
type wasmWebSocketCallback struct {
	*BaseCapability[websocketapi.WebSocketCallback, *websocketapi.WebSocketCallbackPlugin]
}
