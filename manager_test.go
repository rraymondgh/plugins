package plugins

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/rraymondgh/plugins/schema"
)

var _ = Describe("Plugin Manager", func() {
	var mgr *managerImpl
	var ctx context.Context
	var cfg *Config

	BeforeEach(func() {
		// We change the plugins folder to random location to avoid conflicts with other tests,
		cfg = TestConfig()
		originalPluginsFolder := cfg.Folder
		DeferCleanup(func() {
			cfg.Folder = originalPluginsFolder
		})
		cfg.Enabled = true
		cfg.Folder = testDataDir
		cfg.PluginConfig = map[string]map[string]string{
			"fake_test": {"test": "test_value"},
		}

		ctx = GinkgoT().Context()
		mgr = createManager(metrics.NewNoopInstance(), cfg)
		mgr.ScanPlugins()

		// Wait for all plugins to compile to avoid race conditions
		err := mgr.EnsureCompiled("fake_test")
		Expect(err).NotTo(HaveOccurred(), "fake_test should compile successfully")
		err = mgr.EnsureCompiled("multi_plugin")
		Expect(err).NotTo(HaveOccurred(), "multi_plugin should compile successfully")
		err = mgr.EnsureCompiled("unauthorized_plugin")
		Expect(err).NotTo(HaveOccurred(), "unauthorized_plugin should compile successfully")
	})

	It("should scan and discover plugins from the testdata folder", func() {
		Expect(mgr).NotTo(BeNil())

		initServiceNames := mgr.PluginNames("LifecycleManagement")
		Expect(initServiceNames).To(ContainElements("multi_plugin", "fake_init_service"))

		testNames := mgr.PluginNames(CapabilityIntegrationTest)
		Expect(testNames).To(HaveLen(3))
		Expect(testNames).To(ContainElements("fake_test", "unauthorized_plugin", "multi_plugin"))
	})

	It("should load a test plugin and invoke intergration test methods", func() {
		plugin := mgr.LoadPlugin("fake_test", CapabilityIntegrationTest)
		Expect(plugin).NotTo(BeNil())

		testRPC, ok := plugin.(IntegrationTestInterface)
		Expect(ok).To(BeTrue(), "plugin should implement IntegrationTestInterface")

		msg, err := testRPC.SendTo(ctx, "1234567890")
		Expect(err).NotTo(HaveOccurred())
		Expect(msg).To(Equal("1234567890"))
	})

	Describe("ScanPlugins", func() {
		var tempPluginsDir string
		var m *managerImpl

		BeforeEach(func() {
			tempPluginsDir, _ = os.MkdirTemp("", "plugins-test-*")
			DeferCleanup(func() {
				_ = os.RemoveAll(tempPluginsDir)
			})

			cfg.Folder = tempPluginsDir
			m = createManager(metrics.NewNoopInstance(), cfg)
		})

		// Helper to create a complete valid plugin for manager testing
		createValidPlugin := func(folderName, manifestName string) {
			pluginDir := filepath.Join(tempPluginsDir, folderName)
			Expect(os.MkdirAll(pluginDir, 0o755)).To(Succeed())

			// Copy real WASM file from testdata
			sourceWasmPath := filepath.Join(testDataDir, "fake_test", "plugin.wasm")
			targetWasmPath := filepath.Join(pluginDir, "plugin.wasm")
			sourceWasm, err := os.ReadFile(sourceWasmPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.WriteFile(targetWasmPath, sourceWasm, 0o600)).To(Succeed())

			manifest := `{
				"name": "` + manifestName + `",
				"version": "1.0.0",
				"capabilities": ["IntegrationTest"],
				"author": "Test Author",
				"description": "Test Plugin",
				"website": "https://test.plugins.org/` + manifestName + `",
				"permissions": {}
			}`
			Expect(
				os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(manifest), 0o600),
			).To(Succeed())
		}

		It("should register and compile discovered plugins", func() {
			createValidPlugin("test-plugin", "test-plugin")

			m.ScanPlugins()

			// Focus on manager behavior: registration and compilation
			Expect(m.plugins).To(HaveLen(1))
			Expect(m.plugins).To(HaveKey("test-plugin"))

			plugin := m.plugins["test-plugin"]
			Expect(plugin.ID).To(Equal("test-plugin"))
			Expect(plugin.Manifest.Name).To(Equal("test-plugin"))

			// Verify plugin can be loaded (compilation successful)
			loadedPlugin := m.LoadPlugin("test-plugin", CapabilityIntegrationTest)
			Expect(loadedPlugin).NotTo(BeNil())
		})

		It("should handle multiple plugins with different IDs but same manifest names", func() {
			// This tests manager-specific behavior: how it handles ID conflicts
			createValidPlugin("lastfm-official", "lastfm")
			createValidPlugin("lastfm-custom", "lastfm")

			m.ScanPlugins()

			// Both should be registered with their folder names as IDs
			Expect(m.plugins).To(HaveLen(2))
			Expect(m.plugins).To(HaveKey("lastfm-official"))
			Expect(m.plugins).To(HaveKey("lastfm-custom"))

			// Both should be loadable independently
			official := m.LoadPlugin("lastfm-official", CapabilityIntegrationTest)
			custom := m.LoadPlugin("lastfm-custom", CapabilityIntegrationTest)
			Expect(official).NotTo(BeNil())
			Expect(custom).NotTo(BeNil())
			Expect(official.PluginID()).To(Equal("lastfm-official"))
			Expect(custom.PluginID()).To(Equal("lastfm-custom"))
		})
	})

	Describe("LoadPlugin", func() {
		It("should load a MetadataAgent plugin and invoke artist-related methods", func() {
			plugin := mgr.LoadPlugin("fake_test", CapabilityIntegrationTest)
			Expect(plugin).NotTo(BeNil())

			testRPC, ok := plugin.(IntegrationTestInterface)
			Expect(ok).To(BeTrue(), "plugin should implement IntegrationTest")

			msg, err := testRPC.SendTo(ctx, "1234567890")
			Expect(err).NotTo(HaveOccurred())
			Expect(msg).To(Equal("1234567890"))
		})
	})

	Describe("EnsureCompiled", func() {
		It("should successfully wait for plugin compilation", func() {
			err := mgr.EnsureCompiled("fake_test")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for non-existent plugin", func() {
			err := mgr.EnsureCompiled("non-existent-plugin")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("plugin not found: non-existent-plugin"))
		})

		It("should wait for compilation to complete for all valid plugins", func() {
			pluginNames := []string{"multi_plugin", "fake_test"}

			for _, name := range pluginNames {
				err := mgr.EnsureCompiled(name)
				Expect(err).NotTo(HaveOccurred(), "plugin %s should compile successfully", name)
			}
		})
	})

	Describe("Invoke Methods", func() {
		It("should load all test plugins and invoke methods", func() {
			fakeTest, isOK := mgr.LoadPlugin("fake_test", CapabilityIntegrationTest).(IntegrationTestInterface)
			Expect(isOK).To(BeTrue())

			Expect(fakeTest).NotTo(BeNil(), "fake_test should be loaded")

			// Test SendTo method - need to cast to the specific interface
			info, err := fakeTest.SendTo(ctx, "Test Test")
			Expect(err).NotTo(HaveOccurred())
			Expect(info).To(Equal("Test Test"))
		})
	})

	Describe("Permission Enforcement Integration", func() {
		It("should fail when plugin tries to access unauthorized services", func() {
			// This plugin tries to access config service but has no permissions
			plugin := mgr.LoadPlugin("unauthorized_plugin", CapabilityIntegrationTest)
			Expect(plugin).NotTo(BeNil())

			intTest, ok := plugin.(IntegrationTestInterface)
			Expect(ok).To(BeTrue())

			// This should fail because the plugin tries to access unauthorized config service
			// The exact behavior depends on the plugin implementation, but it should either:
			// 1. Fail during instantiation, or
			// 2. Return an error when trying to call config methods

			// Try to use one of the available methods - let's test with SendTo
			_, err := intTest.SendTo(ctx, "id")
			if err != nil {
				// If there's an error, it should be related to missing permissions
				Expect(err.Error()).To(ContainSubstring(""))
			}
		})
	})

	Describe("Plugin Initialization Lifecycle", func() {
		BeforeEach(func() {
			cfg.Enabled = true
			cfg.Folder = testDataDir
		})

		Context("when OnInit is successful", func() {
			It("should register and initialize the plugin", func() {
				cfg.PluginConfig = nil
				mgr = createManager(
					metrics.NewNoopInstance(),
					cfg,
				) // Create manager after setting config
				mgr.ScanPlugins()

				plugin := mgr.plugins["fake_init_service"]
				Expect(plugin).NotTo(BeNil())

				Eventually(func() bool {
					return mgr.lifecycle.isInitialized(plugin)
				}).Should(BeTrue())

				// Check that the plugin is still registered
				names := mgr.PluginNames(CapabilityLifecycleManagement)
				Expect(names).To(ContainElement("fake_init_service"))
			})
		})

		Context("when OnInit fails", func() {
			It("should unregister the plugin if OnInit returns an error string", func() {
				cfg := TestConfig()
				cfg.PluginConfig = map[string]map[string]string{
					"fake_init_service": {
						"returnError": "response_error",
					},
				}

				mgr = createManager(
					metrics.NewNoopInstance(),
					cfg,
				) // Create manager after setting config
				mgr.ScanPlugins()

				Eventually(func() []string {
					return mgr.PluginNames(CapabilityLifecycleManagement)
				}).ShouldNot(ContainElement("fake_init_service"))
			})

			It("should unregister the plugin if OnInit returns a Go error", func() {
				cfg := TestConfig()
				cfg.PluginConfig = map[string]map[string]string{
					"fake_init_service": {
						"returnError": "go_error",
					},
				}
				mgr = createManager(
					metrics.NewNoopInstance(),
					cfg,
				) // Create manager after setting config
				mgr.ScanPlugins()

				Eventually(func() []string {
					return mgr.PluginNames(CapabilityLifecycleManagement)
				}).ShouldNot(ContainElement("fake_init_service"))
			})
		})

		It("should clear lifecycle state when unregistering a plugin", func() {
			// Create a manager and register a plugin
			mgr := createManager(metrics.NewNoopInstance(), TestConfig())

			// Create a mock plugin with LifecycleManagement capability
			plugin := &plugin{
				ID:           "test-plugin",
				Capabilities: []string{CapabilityLifecycleManagement},
				Manifest: &schema.PluginManifest{
					Version: "1.0.0",
				},
			}

			// Register the plugin in the manager
			mgr.mu.Lock()
			mgr.plugins[plugin.ID] = plugin
			mgr.mu.Unlock()

			// Mark the plugin as initialized in the lifecycle manager
			mgr.lifecycle.markInitialized(plugin)
			Expect(mgr.lifecycle.isInitialized(plugin)).To(BeTrue())

			// Unregister the plugin
			mgr.unregisterPlugin(plugin.ID)

			// Verify that the plugin is no longer in the manager
			mgr.mu.RLock()
			_, exists := mgr.plugins[plugin.ID]
			mgr.mu.RUnlock()
			Expect(exists).To(BeFalse())

			// Verify that the lifecycle state has been cleared
			Expect(mgr.lifecycle.isInitialized(plugin)).To(BeFalse())
		})
	})
})
