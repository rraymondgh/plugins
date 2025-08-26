package plugins

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hostconfig "github.com/rraymondgh/plugins/host/config"
)

var _ = Describe("configServiceImpl", func() {
	var (
		svc        *configServiceImpl
		pluginName string
		cfg        *Config
	)

	BeforeEach(func() {
		pluginName = "testplugin"
		cfg = TestConfig()
		cfg.PluginConfig = map[string]map[string]string{
			pluginName: {"foo": "bar", "baz": "qux"},
		}
		svc = &configServiceImpl{pluginID: pluginName, config: cfg}
	})

	It("returns config for known plugin", func() {
		resp, err := svc.GetPluginConfig(context.Background(), &hostconfig.GetPluginConfigRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.GetConfig()).To(HaveKeyWithValue("foo", "bar"))
		Expect(resp.GetConfig()).To(HaveKeyWithValue("baz", "qux"))
	})

	It("returns error for unknown plugin", func() {
		svc.pluginID = "unknown"
		resp, err := svc.GetPluginConfig(context.Background(), &hostconfig.GetPluginConfigRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.GetConfig()).To(BeEmpty())
	})

	It("returns empty config if plugin config is empty", func() {
		svc.config.PluginConfig[pluginName] = map[string]string{}
		resp, err := svc.GetPluginConfig(context.Background(), &hostconfig.GetPluginConfigRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.GetConfig()).To(BeEmpty())
	})
})
