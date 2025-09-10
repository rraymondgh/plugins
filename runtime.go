package plugins

import (
	"context"
	"crypto/md5"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/host/cache"
	"github.com/rraymondgh/plugins/host/config"
	"github.com/rraymondgh/plugins/host/http"
	"github.com/rraymondgh/plugins/host/websocket"
	"github.com/rraymondgh/plugins/schema"
	"github.com/tetratelabs/wazero"
	wazeroapi "github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"go.uber.org/zap"
	"go.uber.org/zap/zapio"
)

const maxParallelCompilations = 2 // Limit to 2 concurrent compilations

var (
	compileSemaphore = make(chan struct{}, maxParallelCompilations)
	compilationCache wazero.CompilationCache
	cacheOnce        sync.Once
	runtimePool      sync.Map // map[string]*cachingRuntime
)

// createRuntime returns a function that creates a new wazero runtime and instantiates the required host functions
// based on the given plugin permissions
func (m *managerImpl) createRuntime(
	pluginID string,
	permissions schema.PluginManifestPermissions,
) api.WazeroNewRuntime {
	return func(ctx context.Context) (wazero.Runtime, error) {
		// Check if runtime already exists
		if rt, ok := runtimePool.Load(pluginID); ok {
			zap.L().Debug("Using existing runtime",
				zap.String("plugin", pluginID), zap.String("runtime", fmt.Sprintf("%p", rt)))
			// Return a new wrapper for each call, so each instance gets its own module capture
			return newScopedRuntime(rt.(wazero.Runtime)), nil
		}

		// Create new runtime with all the setup
		cachingRT, err := m.createCachingRuntime(ctx, pluginID, permissions)
		if err != nil {
			return nil, err
		}

		// Use LoadOrStore to atomically check and store, preventing race conditions
		if existing, loaded := runtimePool.LoadOrStore(pluginID, cachingRT); loaded {
			// Another goroutine created the runtime first, close ours and return the existing one
			zap.L().
				Debug("Race condition detected, using existing runtime", zap.String("plugin",
					pluginID), zap.String("runtime", fmt.Sprintf("%p", existing)))

			_ = cachingRT.Close(ctx)

			return newScopedRuntime(existing.(wazero.Runtime)), nil
		}

		zap.L().Debug("Created new runtime",
			zap.String("plugin", pluginID), zap.String("runtime", fmt.Sprintf("%p", cachingRT)))

		return newScopedRuntime(cachingRT), nil
	}
}

// createCachingRuntime handles the complex logic of setting up a new cachingRuntime
func (m *managerImpl) createCachingRuntime(
	ctx context.Context,
	pluginID string,
	permissions schema.PluginManifestPermissions,
) (*cachingRuntime, error) {
	// Get compilation cache
	compCache, err := getCompilationCache(m.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get compilation cache: %w", err)
	}

	// Create the runtime
	runtimeConfig := wazero.NewRuntimeConfig().WithCompilationCache(compCache)

	r := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		return nil, err
	}

	// Setup host services
	if err := m.setupHostServices(ctx, r, pluginID, permissions); err != nil {
		_ = r.Close(ctx)
		return nil, err
	}

	return newCachingRuntime(r, pluginID), nil
}

// setupHostServices configures all the permitted host services for a plugin
func (m *managerImpl) setupHostServices(
	ctx context.Context,
	r wazero.Runtime,
	pluginID string,
	permissions schema.PluginManifestPermissions,
) error {
	// Define all available host services
	type hostService struct {
		name        string
		isPermitted bool
		loadFunc    func() (map[string]wazeroapi.FunctionDefinition, error)
	}

	// List of all available host services with their permissions and loading functions
	availableServices := []hostService{
		{"config", permissions.Config != nil, func() (map[string]wazeroapi.FunctionDefinition, error) {
			return loadHostLibrary[config.ConfigService](
				ctx,
				config.Instantiate,
				&configServiceImpl{pluginID: pluginID, config: m.config},
			)
		}},
		{"cache", permissions.Cache != nil, func() (map[string]wazeroapi.FunctionDefinition, error) {
			return loadHostLibrary[cache.CacheService](ctx, cache.Instantiate, newCacheService(pluginID))
		}},
		{"http", permissions.Http != nil, func() (map[string]wazeroapi.FunctionDefinition, error) {
			httpPerms, err := parseHTTPPermissions(permissions.Http)
			if err != nil {
				return nil, fmt.Errorf("invalid http permissions for plugin %s: %w", pluginID, err)
			}
			return loadHostLibrary[http.HttpService](ctx, http.Instantiate, &httpServiceImpl{
				pluginID:    pluginID,
				permissions: httpPerms,
			})
		}},
		{"websocket", permissions.Websocket != nil, func() (map[string]wazeroapi.FunctionDefinition, error) {
			wsPerms, err := parseWebSocketPermissions(permissions.Websocket)
			if err != nil {
				return nil, fmt.Errorf("invalid websocket permissions for plugin %s: %w", pluginID, err)
			}
			return loadHostLibrary[websocket.WebSocketService](
				ctx,
				websocket.Instantiate,
				m.websocketService.HostFunctions(pluginID, wsPerms),
			)
		}},
	}

	// Load only permitted services
	var grantedPermissions []string

	var libraries []map[string]wazeroapi.FunctionDefinition

	for _, service := range availableServices {
		if service.isPermitted {
			lib, err := service.loadFunc()
			if err != nil {
				return fmt.Errorf("error loading %s lib: %w", service.name, err)
			}

			libraries = append(libraries, lib)
			grantedPermissions = append(grantedPermissions, service.name)
		}
	}

	zap.L().Debug("Granting permissions for plugin",
		zap.String("plugin", pluginID), zap.Any("permissions", grantedPermissions))

	// Combine the permitted libraries
	return combineLibraries(ctx, r, libraries...)
}

// purgeCacheBySize removes the oldest files in dir until its total size is
// lower than or equal to maxSize. maxSize should be a human-readable string
// like "10MB" or "200K". If parsing fails or maxSize is "0", the function is
// a no-op.
func purgeCacheBySize(dir, maxSize string) {
	sizeLimit, err := humanize.ParseBytes(maxSize)
	if err != nil || sizeLimit == 0 {
		return
	}

	type fileInfo struct {
		path string
		size uint64
		mod  int64
	}

	var files []fileInfo

	var total uint64

	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			zap.L().Debug("Failed to access plugin cache entry", zap.String("path", path), zap.Error(err))
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			zap.L().
				Debug("Failed to get file info for plugin cache entry", zap.String("path", path), zap.Error(err))
			return nil
		}

		files = append(files, fileInfo{
			path: path,
			size: uint64(info.Size()),
			mod:  info.ModTime().UnixMilli(),
		})
		total += uint64(info.Size())

		return nil
	}

	if err := filepath.WalkDir(dir, walk); err != nil {
		if !os.IsNotExist(err) {
			zap.L().
				Warn("Failed to traverse plugin cache directory", zap.String("path", dir), zap.Error(err))
		}

		return
	}

	zap.L().
		Debug("Current plugin cache size", zap.String("path", dir), zap.String("size",
			humanize.Bytes(total)), zap.String("sizeLimit", humanize.Bytes(sizeLimit)))

	if total <= sizeLimit {
		return
	}

	zap.L().
		Debug("Purging plugin cache", zap.String("path", dir), zap.String("sizeLimit",
			humanize.Bytes(sizeLimit)), zap.String("currentSize", humanize.Bytes(total)))
	sort.Slice(files, func(i, j int) bool { return files[i].mod < files[j].mod })

	for _, f := range files {
		if total <= sizeLimit {
			break
		}

		if err := os.Remove(f.path); err != nil {
			zap.L().Warn("Failed to remove plugin cache entry",
				zap.String("path", f.path), zap.String("size", humanize.Bytes(f.size)), zap.Error(err))
			continue
		}

		total -= f.size
		zap.L().
			Debug("Removed plugin cache entry", zap.String("path", f.path), zap.String("size",
				humanize.Bytes(f.size)), zap.Time("time",
				time.UnixMilli(f.mod)), zap.String("remainingSize", humanize.Bytes(total)))

		// Remove empty parent directories
		dirPath := filepath.Dir(f.path)
		for dirPath != dir {
			if err := os.Remove(dirPath); err != nil {
				break
			}

			dirPath = filepath.Dir(dirPath)
		}
	}
}

// getCompilationCache returns the global compilation cache, creating it if necessary
func getCompilationCache(cfg *Config) (wazero.CompilationCache, error) {
	var err error

	cacheOnce.Do(func() {
		cacheDir := filepath.Join(cfg.CacheFolder, "plugins")
		purgeCacheBySize(cacheDir, cfg.CacheSize)
		compilationCache, err = wazero.NewCompilationCacheWithDir(cacheDir)
	})

	return compilationCache, err
}

// newWazeroModuleConfig creates the correct ModuleConfig for plugins
func newWazeroModuleConfig() wazero.ModuleConfig {
	return wazero.NewModuleConfig().
		WithStartFunctions("_initialize").
		WithStderr(&zapio.Writer{Log: zap.L().Named("runtime").WithOptions(zap.WithCaller(false))})
}

// pluginCompilationTimeout returns the timeout for plugin compilation
func pluginCompilationTimeout(cfg *Config) time.Duration {
	if cfg.DevPluginCompilationTimeout > 0 {
		return cfg.DevPluginCompilationTimeout
	}

	return time.Minute
}

// precompilePlugin compiles the WASM module in the background and updates the pluginState.
func precompilePlugin(p *plugin) {
	compileSemaphore <- struct{}{}
	defer func() { <-compileSemaphore }()

	ctx := context.Background()

	r, err := p.Runtime(ctx)
	if err != nil {
		p.compilationErr = fmt.Errorf("failed to create runtime for plugin %s: %w", p.ID, err)
		close(p.compilationReady)

		return
	}

	b, err := os.ReadFile(p.WasmPath)
	if err != nil {
		p.compilationErr = fmt.Errorf("failed to read wasm file: %w", err)
		close(p.compilationReady)

		return
	}

	// We know r is always a *scopedRuntime from createRuntime
	scopedRT := r.(*scopedRuntime)

	cachingRT := scopedRT.GetCachingRuntime()
	if cachingRT == nil {
		p.compilationErr = fmt.Errorf("failed to get cachingRuntime for plugin %s", p.ID)
		close(p.compilationReady)

		return
	}

	_, err = cachingRT.CompileModule(ctx, b)
	if err != nil {
		p.compilationErr = fmt.Errorf("failed to compile WASM for plugin %s: %w", p.ID, err)
		zap.L().
			Warn("Plugin compilation failed", zap.String("name", p.ID), zap.String("path", p.WasmPath), zap.Error(err))
	} else {
		p.compilationErr = nil
		zap.L().Debug("Plugin compilation completed", zap.String("name", p.ID), zap.String("path", p.WasmPath))
	}

	close(p.compilationReady)
}

// loadHostLibrary loads the given host library and returns its exported functions
func loadHostLibrary[S any](
	ctx context.Context,
	instantiateFn func(context.Context, wazero.Runtime, S) error,
	service S,
) (map[string]wazeroapi.FunctionDefinition, error) {
	r := wazero.NewRuntime(ctx)
	if err := instantiateFn(ctx, r, service); err != nil {
		return nil, err
	}

	m := r.Module("env")

	return m.ExportedFunctionDefinitions(), nil
}

// combineLibraries combines the given host libraries into a single "env" module
func combineLibraries(ctx context.Context, r wazero.Runtime, libs ...map[string]wazeroapi.FunctionDefinition) error {
	// Merge the libraries
	hostLib := map[string]wazeroapi.FunctionDefinition{}
	for _, lib := range libs {
		maps.Copy(hostLib, lib)
	}

	// Create the combined host module
	envBuilder := r.NewHostModuleBuilder("env")

	for name, fd := range hostLib {
		fn, ok := fd.GoFunction().(wazeroapi.GoModuleFunction)
		if !ok {
			return fmt.Errorf("invalid function definition: %s", fd.DebugName())
		}

		envBuilder.NewFunctionBuilder().
			WithGoModuleFunction(fn, fd.ParamTypes(), fd.ResultTypes()).
			WithParameterNames(fd.ParamNames()...).Export(name)
	}

	// Instantiate the combined host module
	if _, err := envBuilder.Instantiate(ctx); err != nil {
		return err
	}

	return nil
}

const (
	// WASM Instance pool configuration
	// defaultPoolSize is the maximum number of instances per plugin that are kept in the pool for reuse
	defaultPoolSize = 8
	// defaultInstanceTTL is the time after which an instance is considered stale and can be evicted
	defaultInstanceTTL = time.Minute
	// defaultMaxConcurrentInstances is the hard limit on total instances that can exist simultaneously
	defaultMaxConcurrentInstances = 10
	// defaultGetTimeout is the maximum time to wait when getting an instance if at the concurrent limit
	defaultGetTimeout = 5 * time.Second

	// Compiled module cache configuration
	// defaultCompiledModuleTTL is the time after which a compiled module is evicted from the cache
	defaultCompiledModuleTTL = 5 * time.Minute
)

// cachedCompiledModule encapsulates a compiled WebAssembly module with TTL management
type cachedCompiledModule struct {
	module     wazero.CompiledModule
	hash       [16]byte
	lastAccess time.Time
	timer      *time.Timer
	mu         sync.Mutex
	pluginID   string // for logging purposes
}

// newCachedCompiledModule creates a new cached compiled module with TTL management
func newCachedCompiledModule(module wazero.CompiledModule, wasmBytes []byte, pluginID string) *cachedCompiledModule {
	c := &cachedCompiledModule{
		module:     module,
		hash:       md5.Sum(wasmBytes),
		lastAccess: time.Now(),
		pluginID:   pluginID,
	}

	// Set up the TTL timer
	c.timer = time.AfterFunc(defaultCompiledModuleTTL, c.evict)

	return c
}

// get returns the cached module if the hash matches, nil otherwise
// Also resets the TTL timer on successful access
func (c *cachedCompiledModule) get(wasmHash [16]byte) wazero.CompiledModule {
	c.mu.Lock() // Use write lock because we modify state in resetTimer
	defer c.mu.Unlock()

	if c.module != nil && c.hash == wasmHash {
		// Reset TTL timer on access
		c.resetTimer()
		return c.module
	}

	return nil
}

// resetTimer resets the TTL timer (must be called with lock held)
func (c *cachedCompiledModule) resetTimer() {
	c.lastAccess = time.Now()

	if c.timer != nil {
		c.timer.Stop()
		c.timer = time.AfterFunc(defaultCompiledModuleTTL, c.evict)
	}
}

// evict removes the cached module and cleans up resources
func (c *cachedCompiledModule) evict() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.module != nil {
		zap.L().
			Debug("cachedCompiledModule: evicting due to TTL expiry",
				zap.String("plugin", c.pluginID), zap.Duration("ttl", defaultCompiledModuleTTL))
		c.module.Close(context.Background())
		c.module = nil
		c.hash = [16]byte{}
		c.lastAccess = time.Time{}
	}

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

// close cleans up the cached module and stops the timer
func (c *cachedCompiledModule) close(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}

	if c.module != nil {
		c.module.Close(ctx)
		c.module = nil
	}
}

// pooledModule wraps a wazero Module and returns it to the pool when closed.
type pooledModule struct {
	wazeroapi.Module
	pool   *wasmInstancePool[wazeroapi.Module]
	closed bool
}

func (m *pooledModule) Close(ctx context.Context) error {
	if !m.closed {
		m.closed = true
		m.pool.Put(ctx, m.Module)
	}

	return nil
}

func (m *pooledModule) CloseWithExitCode(ctx context.Context, _ uint32) error {
	return m.Close(ctx)
}

func (m *pooledModule) IsClosed() bool {
	return m.closed
}

// newScopedRuntime creates a new scopedRuntime that wraps the given runtime
func newScopedRuntime(runtime wazero.Runtime) *scopedRuntime {
	return &scopedRuntime{Runtime: runtime}
}

// scopedRuntime wraps a cachingRuntime and captures a specific module
// so that Close() only affects that module, not the entire shared runtime
type scopedRuntime struct {
	wazero.Runtime
	capturedModule wazeroapi.Module
}

func (w *scopedRuntime) InstantiateModule(
	ctx context.Context,
	code wazero.CompiledModule,
	config wazero.ModuleConfig,
) (wazeroapi.Module, error) {
	module, err := w.Runtime.InstantiateModule(ctx, code, config)
	if err != nil {
		return nil, err
	}
	// Capture the module for later cleanup
	w.capturedModule = module
	zap.L().Debug("scopedRuntime: captured module", zap.String("moduleID", getInstanceID(module)))

	return module, nil
}

func (w *scopedRuntime) Close(ctx context.Context) error {
	// Close only the captured module, not the entire runtime
	if w.capturedModule != nil {
		zap.L().
			Debug("scopedRuntime: closing captured module", zap.String("moduleID", getInstanceID(w.capturedModule)))
		return w.capturedModule.Close(ctx)
	}

	zap.L().Debug("scopedRuntime: no captured module to close")

	return nil
}

func (w *scopedRuntime) CloseWithExitCode(ctx context.Context, _ uint32) error {
	return w.Close(ctx)
}

// GetCachingRuntime returns the underlying cachingRuntime for internal use
func (w *scopedRuntime) GetCachingRuntime() *cachingRuntime {
	if cr, ok := w.Runtime.(*cachingRuntime); ok {
		return cr
	}

	return nil
}

// cachingRuntime wraps wazero.Runtime and pools module instances per plugin,
// while also caching the compiled module in memory.
type cachingRuntime struct {
	wazero.Runtime

	// pluginID is required to differentiate between different plugins that use the same file to initialize their
	// runtime. The runtime will serve as a singleton for all instances of a given plugin.
	pluginID string

	// cachedModule manages the compiled module cache with TTL
	cachedModule atomic.Pointer[cachedCompiledModule]

	// pool manages reusable module instances
	pool *wasmInstancePool[wazeroapi.Module]

	// poolInitOnce ensures the pool is initialized only once
	poolInitOnce sync.Once

	// compilationMu ensures only one compilation happens at a time per runtime
	compilationMu sync.Mutex
}

func newCachingRuntime(runtime wazero.Runtime, pluginID string) *cachingRuntime {
	return &cachingRuntime{
		Runtime:  runtime,
		pluginID: pluginID,
	}
}

func (r *cachingRuntime) initPool(code wazero.CompiledModule, config wazero.ModuleConfig) {
	r.poolInitOnce.Do(func() {
		r.pool = newWasmInstancePool[wazeroapi.Module](
			r.pluginID,
			defaultPoolSize,
			defaultMaxConcurrentInstances,
			defaultGetTimeout,
			defaultInstanceTTL,
			func(ctx context.Context) (wazeroapi.Module, error) {
				zap.L().
					Debug("cachingRuntime: creating new module instance", zap.String("plugin", r.pluginID))
				return r.Runtime.InstantiateModule(ctx, code, config)
			},
		)
	})
}

//nolint:contextcheck
func (r *cachingRuntime) InstantiateModule(
	ctx context.Context,
	code wazero.CompiledModule,
	config wazero.ModuleConfig,
) (wazeroapi.Module, error) {
	r.initPool(code, config)

	mod, err := r.pool.Get(ctx)
	if err != nil {
		return nil, err
	}

	wrapped := &pooledModule{Module: mod, pool: r.pool}
	zap.L().Debug(
		"cachingRuntime: created wrapper for module", zap.String("plugin",
			r.pluginID),
		zap.String("underlyingModuleID",
			fmt.Sprintf("%p", mod)),
		zap.String("wrapperID",
			fmt.Sprintf("%p", wrapped)),
	)

	return wrapped, nil
}

func (r *cachingRuntime) Close(ctx context.Context) error {
	zap.L().Debug("cachingRuntime: closing runtime", zap.String("plugin", r.pluginID))

	// Clean up compiled module cache
	if cached := r.cachedModule.Swap(nil); cached != nil {
		cached.close(ctx)
	}

	// Close the instance pool
	if r.pool != nil {
		r.pool.Close(ctx)
	}
	// Close the underlying runtime
	return r.Runtime.Close(ctx)
}

// setCachedModule stores a newly compiled module in the cache with TTL management
func (r *cachingRuntime) setCachedModule(module wazero.CompiledModule, wasmBytes []byte) {
	newCached := newCachedCompiledModule(module, wasmBytes, r.pluginID)

	// Replace old cached module and clean it up
	if old := r.cachedModule.Swap(newCached); old != nil {
		old.close(context.Background())
	}
}

// CompileModule checks if the provided bytes match our cached hash and returns
// the cached compiled module if so, avoiding both file read and compilation.
//
//nolint:contextcheck
func (r *cachingRuntime) CompileModule(ctx context.Context, wasmBytes []byte) (wazero.CompiledModule, error) {
	incomingHash := md5.Sum(wasmBytes)

	// Try to get from cache first (without lock for performance)
	if cached := r.cachedModule.Load(); cached != nil {
		if module := cached.get(incomingHash); module != nil {
			zap.L().Debug("cachingRuntime: using cached compiled module", zap.String("plugin", r.pluginID))
			return module, nil
		}
	}

	// Synchronize compilation to prevent concurrent compilation issues
	r.compilationMu.Lock()
	defer r.compilationMu.Unlock()

	// Double-check cache after acquiring lock (another goroutine might have compiled it)
	if cached := r.cachedModule.Load(); cached != nil {
		if module := cached.get(incomingHash); module != nil {
			zap.L().
				Debug("cachingRuntime: using cached compiled module (after lock)", zap.String("plugin", r.pluginID))
			return module, nil
		}
	}

	// Fall back to normal compilation for different bytes
	zap.L().Debug("cachingRuntime: hash doesn't match cache, compiling normally", zap.String("plugin", r.pluginID))

	module, err := r.Runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}

	// Cache the newly compiled module
	r.setCachedModule(module, wasmBytes)

	return module, nil
}
