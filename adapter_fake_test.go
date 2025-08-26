package plugins

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rraymondgh/plugins/core/metrics"
)

var _ = Describe("Adapter Test", func() {
	var ctx context.Context
	var mgr Manager

	BeforeEach(func() {
		ctx = GinkgoT().Context()

		mgr = createManager(metrics.NewNoopInstance(), TestConfig())
		mgr.ScanPlugins()

		// Wait for all plugins to compile to avoid race conditions
		err := mgr.EnsureCompiled("multi_plugin")
		Expect(err).NotTo(HaveOccurred(), "multi_plugin should compile successfully")
		err = mgr.EnsureCompiled("fake_test")
		Expect(err).NotTo(HaveOccurred(), "fake_test should compile successfully")
	})

	Describe("PluginName", func() {
		It("should return the plugin name", func() {
			test := mgr.LoadPlugin("multi_plugin", CapabilityIntegrationTest)
			Expect(test).NotTo(BeNil(), "multi_plugin should be loaded")
			Expect(test.PluginID()).To(Equal("multi_plugin"))
		})
	})

	Describe("Test methods", func() {
		var testRPC *wasmTest

		BeforeEach(func() {
			testP := mgr.LoadPlugin("fake_test", CapabilityIntegrationTest)
			Expect(testP.PluginID()).To(Equal("fake_test"))
			testRPC = testP.(*wasmTest)
		})

		Context("SendTo", func() {
			It("should return name of plugin", func() {
				info, err := testRPC.SendTo(ctx, "fake_test")

				Expect(err).NotTo(HaveOccurred())
				Expect(info).NotTo(BeNil())
				Expect(info).To(Equal("fake_test"))
			})
		})
	})
})
