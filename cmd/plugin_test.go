package cmd

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rraymondgh/plugins"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

var _ = Describe("Plugin CLI Commands", func() {
	var obslogger zapcore.Core
	var obs *observer.ObservedLogs

	config := zap.NewDevelopmentConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.TimeKey = ""
	config.EncoderConfig = encoderConfig
	std, _ := config.Build()

	var tempDir string
	// var cmd *cobra.Command
	cfg := plugins.TestConfig()
	p := params{
		Config: cfg,
	}

	// Helper to create a test plugin with the given name and details
	createTestPlugin := func(name, author, version string, capabilities []string) string {
		pluginDir := filepath.Join(tempDir, name)
		Expect(os.MkdirAll(pluginDir, 0o755)).To(Succeed())

		// Create a properly formatted capabilities JSON array
		capabilitiesJSON := `"` + strings.Join(capabilities, `", "`) + `"`

		manifest := `{
			"name": "` + name + `",
			"author": "` + author + `",
			"version": "` + version + `",
			"description": "Plugin for testing",
			"website": "https://test.navidrome.org/` + name + `",
			"capabilities": [` + capabilitiesJSON + `],
			"permissions": {}
		}`

		Expect(os.WriteFile(filepath.Join(pluginDir, "manifest.json"), []byte(manifest), 0o600)).To(Succeed())

		// Create a dummy WASM file
		wasmContent := []byte("dummy wasm content for testing")
		Expect(os.WriteFile(filepath.Join(pluginDir, "plugin.wasm"), wasmContent, 0o600)).To(Succeed())

		return pluginDir
	}

	BeforeEach(func() {
		obslogger, obs = observer.New(zap.InfoLevel)
		p.Logger = zap.New(zapcore.NewTee(std.Core(), zap.New(obslogger).Core()))

		tempDir = GinkgoT().TempDir()

		// Setup config
		cfg.Enabled = true
		cfg.Folder = tempDir
	})

	AfterEach(func() {
	})

	Describe("Plugin list command", func() {
		It("should list installed plugins", func() {
			// Create test plugins
			createTestPlugin("plugin1", "Test Author", "1.0.0", []string{"IntegrationTest"})
			createTestPlugin("plugin2", "Another Author", "2.1.0", []string{"WebSocketCallback"})

			// Execute command
			_ = p.pluginList(&cli.Context{})

			Expect(obs.Len()).To(Equal(3))

			Expect(obs.All()[1].Message).To(ContainSubstring("plugin1"))
			Expect(obs.All()[1].Message).To(ContainSubstring("Test Author"))
			Expect(obs.All()[1].Message).To(ContainSubstring("1.0.0"))
			Expect(obs.All()[1].Message).To(ContainSubstring("IntegrationTest"))

			Expect(obs.All()[2].Message).To(ContainSubstring("plugin2"))
			Expect(obs.All()[2].Message).To(ContainSubstring("Another Author"))
			Expect(obs.All()[2].Message).To(ContainSubstring("2.1.0"))
			Expect(obs.All()[2].Message).To(ContainSubstring("WebSocketCallback"))
		})
	})

	Describe("Plugin info command", func() {
		It("should display information about an installed plugin", func() {
			// Create test plugin with multiple capabilities
			createTestPlugin("test-plugin", "Test Author", "1.0.0",
				[]string{"IntegrationTest", "WebSocketCallback"})

			// Execute command
			f := &flag.FlagSet{}
			_ = f.Parse([]string{"test-plugin"})
			_ = p.pluginInfo(cli.NewContext(nil, f, nil))

			Expect(obs.All()[2].Message).To(ContainSubstring("Name:        test-plugin"))
			Expect(obs.All()[3].Message).To(ContainSubstring("Author:      Test Author"))
			Expect(obs.All()[4].Message).To(ContainSubstring("Version:     1.0.0"))
			Expect(obs.All()[5].Message).To(ContainSubstring("Description: Plugin for testing"))
			Expect(
				obs.All()[6].Message,
			).To(ContainSubstring("Capabilities:    IntegrationTest, WebSocketCallback"))
		})
	})

	Describe("Plugin remove command", func() {
		It("should remove a regular plugin directory", func() {
			// Create test plugin
			pluginDir := createTestPlugin("regular-plugin", "Test Author", "1.0.0",
				[]string{"IntegrationTest"})

			// Execute command
			f := &flag.FlagSet{}
			_ = f.Parse([]string{"regular-plugin"})
			_ = p.pluginRemove(cli.NewContext(nil, f, nil))

			// Verify output
			Expect(
				obs.All()[0].Message,
			).To(ContainSubstring("Plugin 'regular-plugin' removed successfully"))

			// Verify directory is actually removed
			_, err := os.Stat(pluginDir)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("should remove only the symlink for a development plugin", func() {
			// Create a real source directory
			sourceDir := filepath.Join(GinkgoT().TempDir(), "dev-plugin-source")
			Expect(os.MkdirAll(sourceDir, 0o755)).To(Succeed())

			manifest := `{
				"name": "dev-plugin",
				"author": "Dev Author",
				"version": "0.1.0",
				"description": "Development plugin for testing",
				"website": "https://test.navidrome.org/dev-plugin",
				"capabilities": ["IntegrationTest"],
				"permissions": {}
			}`
			Expect(
				os.WriteFile(filepath.Join(sourceDir, "manifest.json"), []byte(manifest), 0o600),
			).To(Succeed())

			// Create a dummy WASM file
			wasmContent := []byte("dummy wasm content for testing")
			Expect(os.WriteFile(filepath.Join(sourceDir, "plugin.wasm"), wasmContent, 0o600)).To(Succeed())

			// Create a symlink in the plugins directory
			symlinkPath := filepath.Join(tempDir, "dev-plugin")
			Expect(os.Symlink(sourceDir, symlinkPath)).To(Succeed())

			// Execute command
			f := &flag.FlagSet{}
			_ = f.Parse([]string{"dev-plugin"})
			_ = p.pluginRemove(cli.NewContext(nil, f, nil))

			// Verify output
			Expect(
				obs.All()[0].Message,
			).To(ContainSubstring("Development plugin symlink 'dev-plugin' removed successfully"))
			Expect(obs.All()[0].Message).To(ContainSubstring("target directory preserved"))

			// Verify the symlink is removed but source directory exists
			_, err := os.Lstat(symlinkPath)
			Expect(os.IsNotExist(err)).To(BeTrue())

			_, err = os.Stat(sourceDir)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
