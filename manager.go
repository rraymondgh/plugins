package plugins

//go:generate protoc --go-plugin_out=. --go-plugin_opt=paths=source_relative api/api.proto
//go:generate protoc --go-plugin_out=. --go-plugin_opt=paths=source_relative host/http/http.proto
//go:generate protoc --go-plugin_out=. --go-plugin_opt=paths=source_relative host/config/config.proto
//go:generate protoc --go-plugin_out=. --go-plugin_opt=paths=source_relative host/websocket/websocket.proto

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/rraymondgh/plugins/schema"
	"github.com/rraymondgh/plugins/utils/singleton"
	"github.com/rraymondgh/plugins/utils/slice"
	"github.com/tetratelabs/wazero"
	"go.uber.org/zap"
)

const (
	CapabilityWebSocketCallback   = "WebSocketCallback"
	CapabilityLifecycleManagement = "LifecycleManagement"
	CapabilityIntegrationTest     = "IntegrationTest"
)

// pluginCreators maps capability types to their respective creator functions
type pluginConstructor func(wasmPath, pluginID string, metrics metrics.Metrics,
	runtime func(context.Context) (wazero.Runtime, error), mc wazero.ModuleConfig) WasmPlugin

var pluginCreators = map[string]pluginConstructor{
	CapabilityWebSocketCallback: newWasmWebSocketCallback,
	CapabilityIntegrationTest:   newWasmTest,
}

// WasmPlugin is the base interface that all WASM plugins implement
type WasmPlugin interface {
	// PluginID returns the unique identifier of the plugin (folder name)
	PluginID() string
}

type plugin struct {
	ID               string
	Path             string
	Capabilities     []string
	WasmPath         string
	Manifest         *schema.PluginManifest // Loaded manifest
	Runtime          api.WazeroNewRuntime
	ModConfig        wazero.ModuleConfig
	compilationReady chan struct{}
	compilationErr   error
	config           *Config
}

func (p *plugin) waitForCompilation() error {
	timeout := pluginCompilationTimeout(p.config)
	select {
	case <-p.compilationReady:
	case <-time.After(timeout):
		err := fmt.Errorf("timed out waiting for plugin %s to compile", p.ID)
		zap.L().
			Error("Timed out waiting for plugin compilation", zap.String("name", p.ID), zap.String("path",
				p.WasmPath), zap.Duration("timeout", timeout), zap.Error(err))

		return err
	}

	if p.compilationErr != nil {
		zap.L().Error("Failed to compile plugin",
			zap.String("name", p.ID), zap.String("path", p.WasmPath), zap.Error(p.compilationErr))
	}

	return p.compilationErr
}

type Manager interface {
	EnsureCompiled(name string) error
	PluginNames(serviceName string) []string
	LoadPlugin(name string, capability string) WasmPlugin
	ScanPlugins()
	WithAdapter(capability string, adapter pluginConstructor) Manager
}

// managerImpl is a singleton that manages plugins
type managerImpl struct {
	plugins          map[string]*plugin      // Map of plugin folder name to plugin info
	mu               sync.RWMutex            // Protects plugins map
	websocketService *websocketService       // Service for handling WebSocket connections
	lifecycle        *pluginLifecycleManager // Manages plugin lifecycle and initialization
	adapters         map[string]WasmPlugin   // Map of plugin folder name + capability to adapter
	metrics          metrics.Metrics
	config           *Config
	constructors     map[string]pluginConstructor
}

// GetManager returns the singleton instance of managerImpl
func GetManager(metrics metrics.Metrics, cfg *Config) Manager {
	if !cfg.Enabled {
		return &noopManager{}
	}

	return singleton.GetInstance(func() *managerImpl {
		return createManager(metrics, cfg)
	})
}

// createManager creates a new managerImpl instance. Used in tests
func createManager(metrics metrics.Metrics, cfg *Config) *managerImpl {
	m := &managerImpl{
		plugins:      make(map[string]*plugin),
		lifecycle:    newPluginLifecycleManager(metrics, cfg),
		metrics:      metrics,
		config:       cfg,
		constructors: maps.Clone(pluginCreators),
	}

	// Create the host services
	m.websocketService = newWebsocketService(m)

	return m
}

func TestManager(metrics metrics.Metrics, cfg *Config) Manager {
	return createManager(metrics, cfg)
}

// registerPlugin adds a plugin to the registry with the given parameters
// Used internally by ScanPlugins to register plugins
func (m *managerImpl) registerPlugin(pluginID, pluginDir, wasmPath string, manifest *schema.PluginManifest) *plugin {
	// Create custom runtime function
	customRuntime := m.createRuntime(pluginID, manifest.Permissions)

	// Configure module and determine plugin name
	mc := newWazeroModuleConfig()

	// Check if it's a symlink, indicating development mode
	isSymlink := false
	if fileInfo, err := os.Lstat(pluginDir); err == nil {
		isSymlink = fileInfo.Mode()&os.ModeSymlink != 0
	}

	// Store plugin info
	p := &plugin{
		ID:   pluginID,
		Path: pluginDir,
		Capabilities: slice.Map(
			manifest.Capabilities,
			func(capElem schema.PluginManifestCapabilitiesElem) string { return string(capElem) },
		),
		WasmPath:         wasmPath,
		Manifest:         manifest,
		Runtime:          customRuntime,
		ModConfig:        mc,
		compilationReady: make(chan struct{}),
		config:           m.config,
	}

	// Register the plugin first
	m.mu.Lock()
	m.plugins[pluginID] = p

	// Register one plugin adapter for each capability
	for _, capability := range manifest.Capabilities {
		capabilityStr := string(capability)

		constructor := m.constructors[capabilityStr]
		if constructor == nil {
			// Warn about unknown capabilities, except for LifecycleManagement (it does not have an adapter)
			if capability != CapabilityLifecycleManagement {
				zap.L().Warn("Unknown plugin capability type",
					zap.String("capability", string(capability)), zap.String("plugin", pluginID))
			}

			continue
		}

		adapter := constructor(wasmPath, pluginID, m.metrics, customRuntime, mc)
		if adapter == nil {
			zap.L().
				Error("Failed to create plugin adapter", zap.String(
					"plugin",
					pluginID,
				), zap.String("capability", capabilityStr), zap.String("path", wasmPath))

			continue
		}

		m.adapters[pluginID+"_"+capabilityStr] = adapter
	}
	m.mu.Unlock()

	zap.L().
		Info("Discovered plugin", zap.String("folder", pluginID), zap.String("name", manifest.Name),
			zap.Any("capabilities", manifest.Capabilities),
			zap.String("wasm", wasmPath), zap.Bool("dev_mode", isSymlink))

	return m.plugins[pluginID]
}

// initializePluginIfNeeded calls OnInit on plugins that implement LifecycleManagement
func (m *managerImpl) initializePluginIfNeeded(plugin *plugin) {
	// Skip if already initialized
	if m.lifecycle.isInitialized(plugin) {
		return
	}

	// Check if the plugin implements LifecycleManagement
	if slices.Contains(plugin.Manifest.Capabilities, CapabilityLifecycleManagement) {
		if err := m.lifecycle.callOnInit(plugin); err != nil {
			m.unregisterPlugin(plugin.ID)
		}
	}
}

// unregisterPlugin removes a plugin from the manager
func (m *managerImpl) unregisterPlugin(pluginID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, ok := m.plugins[pluginID]
	if !ok {
		return
	}

	// Clear initialization state from lifecycle manager
	m.lifecycle.clearInitialized(plugin)

	// Unregister plugin adapters
	for _, capability := range plugin.Manifest.Capabilities {
		delete(m.adapters, pluginID+"_"+string(capability))
	}

	// Unregister plugin
	delete(m.plugins, pluginID)
	zap.L().Info("Unregistered plugin", zap.String("plugin", pluginID))
}

// ScanPlugins scans the plugins directory, discovers all valid plugins, and registers them for use.
func (m *managerImpl) ScanPlugins() {
	// Clear existing plugins
	m.mu.Lock()
	m.plugins = make(map[string]*plugin)
	m.adapters = make(map[string]WasmPlugin)
	m.mu.Unlock()

	// Get plugins directory from config
	root := m.config.Folder
	zap.L().Debug("Scanning plugins folder", zap.String("root", root))

	// Fail fast if the compilation cache cannot be initialized
	_, err := getCompilationCache(m.config)
	if err != nil {
		zap.L().Error("Failed to initialize plugins compilation cache. Disabling plugins", zap.Error(err))
		return
	}

	// Discover all plugins using the shared discovery function
	discoveries := DiscoverPlugins(root)

	validPluginNames := make([]string, 0)

	registeredPlugins := make([]*plugin, 0)

	for _, discovery := range discoveries {
		if discovery.Error != nil {
			// Handle global errors (like directory read failure)
			if discovery.ID == "" {
				zap.L().Error("Plugin discovery failed", zap.Error(discovery.Error))
				return
			}
			// Handle individual plugin errors
			zap.L().
				Error("Failed to process plugin", zap.String("plugin", discovery.ID), zap.Error(discovery.Error))

			continue
		}

		// Log discovery details
		zap.L().
			Debug("Processing entry", zap.String("name", discovery.ID), zap.Bool("isSymlink", discovery.IsSymlink))

		if discovery.IsSymlink {
			zap.L().Debug("Processing symlinked plugin directory",
				zap.String("name", discovery.ID), zap.String("target", discovery.Path))
		}

		zap.L().Debug("Checking for plugin.wasm", zap.String("wasmPath", discovery.WasmPath))
		zap.L().
			Debug("Manifest loaded successfully", zap.String("folder", discovery.ID), zap.String("name",
				discovery.Manifest.Name), zap.Any("capabilities", discovery.Manifest.Capabilities))

		validPluginNames = append(validPluginNames, discovery.ID)

		// Register the plugin
		plugin := m.registerPlugin(discovery.ID, discovery.Path, discovery.WasmPath, discovery.Manifest)
		if plugin != nil {
			registeredPlugins = append(registeredPlugins, plugin)
		}
	}

	// Start background processing for all registered plugins after registration is complete
	// This avoids race conditions between registration and goroutines that might unregister plugins
	for _, p := range registeredPlugins {
		go func(plugin *plugin) {
			precompilePlugin(plugin)
			// Check if this plugin implements InitService and hasn't been initialized yet
			m.initializePluginIfNeeded(plugin)
		}(p)
	}

	zap.L().
		Debug("Found valid plugins", zap.Int("count", len(validPluginNames)), zap.Any("plugins", validPluginNames))
}

// PluginNames returns the folder names of all plugins that implement the specified capability
func (m *managerImpl) PluginNames(capability string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string

	for name, plugin := range m.plugins {
		for _, c := range plugin.Manifest.Capabilities {
			if string(c) == capability {
				names = append(names, name)
				break
			}
		}
	}

	return names
}

func (m *managerImpl) getPlugin(name string, capability string) (*plugin, WasmPlugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, infoOk := m.plugins[name]
	adapter, adapterOk := m.adapters[name+"_"+capability]

	if !infoOk {
		return nil, nil, fmt.Errorf("plugin not registered: %s", name)
	}

	if !adapterOk {
		return nil, nil, fmt.Errorf("plugin adapter not registered: %s, capability: %s", name, capability)
	}

	return info, adapter, nil
}

// LoadPlugin instantiates and returns a plugin by folder name
func (m *managerImpl) LoadPlugin(name string, capability string) WasmPlugin {
	info, adapter, err := m.getPlugin(name, capability)
	if err != nil {
		zap.L().Warn("Error loading plugin", zap.Error(err))
		return nil
	}

	zap.L().Debug("Loading plugin", zap.String("name", name), zap.String("path", info.Path))

	// Wait for the plugin to be ready before using it.
	if err := info.waitForCompilation(); err != nil {
		zap.L().Error("Plugin is not ready, cannot be loaded",
			zap.String("plugin", name), zap.String("capability", capability), zap.Error(err))
		return nil
	}

	if adapter == nil {
		zap.L().Warn("Plugin adapter not found", zap.String("name", name), zap.String("capability", capability))
		return nil
	}

	return adapter
}

// EnsureCompiled waits for a plugin to finish compilation and returns any compilation error.
// This is useful when you need to wait for compilation without loading a specific capability,
// such as during plugin refresh operations or health checks.
func (m *managerImpl) EnsureCompiled(name string) error {
	m.mu.RLock()
	plugin, ok := m.plugins[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}

	return plugin.waitForCompilation()
}

func (m *managerImpl) WithAdapter(capability string, adapter pluginConstructor) Manager {
	schema.WithCapability(capability)

	m.constructors[capability] = adapter

	return m
}

type noopManager struct{}

func (noopManager) EnsureCompiled(_ string) error { return nil }

func (noopManager) PluginNames(_ string) []string { return nil }

func (noopManager) LoadPlugin(_ string, _ string) WasmPlugin { return nil }

func (noopManager) ScanPlugins() {}

func (n noopManager) WithAdapter(_ string, _ pluginConstructor) Manager {
	return n
}
