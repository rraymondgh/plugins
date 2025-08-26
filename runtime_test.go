package plugins

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Runtime", func() {
	Describe("pluginCompilationTimeout", func() {
		It("should use DevPluginCompilationTimeout config for plugin compilation timeout", func() {
			cfg := TestConfig()
			originalTimeout := cfg.DevPluginCompilationTimeout
			DeferCleanup(func() {
				cfg.DevPluginCompilationTimeout = originalTimeout
			})

			cfg.DevPluginCompilationTimeout = 123 * time.Second
			Expect(pluginCompilationTimeout(cfg)).To(Equal(123 * time.Second))

			cfg.DevPluginCompilationTimeout = 0
			Expect(pluginCompilationTimeout(cfg)).To(Equal(time.Minute))
		})
	})
})
