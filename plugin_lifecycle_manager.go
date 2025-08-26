package plugins

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/consts"
	"github.com/rraymondgh/plugins/core/metrics"
	"go.uber.org/zap"
)

// pluginLifecycleManager tracks which plugins have been initialized and manages their lifecycle
type pluginLifecycleManager struct {
	plugins sync.Map // string -> bool
	config  map[string]map[string]string
	metrics metrics.Metrics
}

// newPluginLifecycleManager creates a new plugin lifecycle manager
func newPluginLifecycleManager(metrics metrics.Metrics, cfg *Config) *pluginLifecycleManager {
	config := maps.Clone(cfg.PluginConfig)

	return &pluginLifecycleManager{
		config:  config,
		metrics: metrics,
	}
}

// isInitialized checks if a plugin has been initialized
func (m *pluginLifecycleManager) isInitialized(plugin *plugin) bool {
	key := plugin.ID + consts.Zwsp + plugin.Manifest.Version
	value, exists := m.plugins.Load(key)

	return exists && value.(bool)
}

// markInitialized marks a plugin as initialized
func (m *pluginLifecycleManager) markInitialized(plugin *plugin) {
	key := plugin.ID + consts.Zwsp + plugin.Manifest.Version
	m.plugins.Store(key, true)
}

// clearInitialized removes the initialization state of a plugin
func (m *pluginLifecycleManager) clearInitialized(plugin *plugin) {
	key := plugin.ID + consts.Zwsp + plugin.Manifest.Version
	m.plugins.Delete(key)
}

// callOnInit calls the OnInit method on a plugin that implements LifecycleManagement
func (m *pluginLifecycleManager) callOnInit(plugin *plugin) error {
	ctx := context.Background()

	zap.L().Debug("Initializing plugin", zap.String("name", plugin.ID))

	start := time.Now()

	// Create LifecycleManagement plugin instance
	loader, err := api.NewLifecycleManagementPlugin(
		ctx,
		api.WazeroRuntime(plugin.Runtime),
		api.WazeroModuleConfig(plugin.ModConfig),
	)
	if loader == nil || err != nil {
		zap.L().
			Error("Error creating LifecycleManagement plugin", zap.String("plugin", plugin.ID), zap.Error(err))
		return err
	}

	initPlugin, err := loader.Load(ctx, plugin.WasmPath)
	if err != nil {
		zap.L().Error("Error loading LifecycleManagement plugin",
			zap.String("plugin", plugin.ID), zap.String("path", plugin.WasmPath), zap.Error(err))
		return err
	}
	defer initPlugin.Close(ctx)

	// Prepare the request with plugin-specific configuration
	req := &api.InitRequest{}

	// Add plugin configuration if available
	if m.config != nil {
		if pluginConfig, ok := m.config[plugin.ID]; ok && len(pluginConfig) > 0 {
			req.Config = maps.Clone(pluginConfig)
			zap.L().Debug("Passing configuration to plugin",
				zap.String("plugin", plugin.ID), zap.Int("configKeys", len(pluginConfig)))
		}
	}

	// Call OnInit
	callStart := time.Now()
	_, err = checkErr(initPlugin.OnInit(ctx, req))
	m.metrics.RecordPluginRequest(ctx, plugin.ID, "OnInit", err == nil, time.Since(callStart).Milliseconds())

	if err != nil {
		zap.L().Error("Error initializing plugin",
			zap.String("plugin", plugin.ID), zap.Duration("elapsed", time.Since(start)), zap.Error(err))
		return err
	}

	// Mark the plugin as initialized
	m.markInitialized(plugin)
	zap.L().Debug("Plugin initialized successfully",
		zap.String("plugin", plugin.ID), zap.Duration("elapsed", time.Since(start)))

	return nil
}
