package plugins

import (
	"context"

	"github.com/rraymondgh/plugins/host/config"
)

type configServiceImpl struct {
	pluginID string
	config   *Config
}

func (c *configServiceImpl) GetPluginConfig(
	_ context.Context,
	_ *config.GetPluginConfigRequest,
) (*config.GetPluginConfigResponse, error) {
	cfg, ok := c.config.PluginConfig[c.pluginID]
	if !ok {
		cfg = map[string]string{}
	}

	return &config.GetPluginConfigResponse{
		Config: cfg,
	}, nil
}
