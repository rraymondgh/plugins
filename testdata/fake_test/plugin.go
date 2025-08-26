//go:build wasip1

package main

import (
	"context"
	"log"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/host/config"
)

type FakeTest struct {
	cfg config.ConfigService
}

func (t *FakeTest) SendTo(ctx context.Context, req *api.TestSendToRequest) (*api.TestSendToResponse, error) {
	log.Printf("[FakeTest] SendTo")
	_, err := t.cfg.GetPluginConfig(ctx, &config.GetPluginConfigRequest{})
	log.Print(err)
	return &api.TestSendToResponse{Message: req.GetID()}, nil
}

func main() {}

var plugin = &FakeTest{
	cfg: config.NewConfigService(),
}

func init() {
	api.RegisterIntegrationTest(plugin)
}
