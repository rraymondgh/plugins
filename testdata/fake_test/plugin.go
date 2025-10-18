//go:build wasip1

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/rraymondgh/plugins/api/testapi"
	"github.com/rraymondgh/plugins/host/cache"
	"github.com/rraymondgh/plugins/host/config"
	"github.com/rraymondgh/plugins/host/http"
)

type FakeTest struct {
	cfg   config.ConfigService
	cache cache.CacheService
	http  http.HttpService
}

func (t *FakeTest) ConfigTest(ctx context.Context, req *testapi.SimpleRequest) (*testapi.SimpleResponse, error) {
	_, err := t.cfg.GetPluginConfig(ctx, &config.GetPluginConfigRequest{})
	if err != nil {
		log.Print(err)
	}
	return &testapi.SimpleResponse{Message: req.GetID()}, nil
}

func (t *FakeTest) HTTPTest(ctx context.Context, req *testapi.HttpRequest) (*testapi.SimpleResponse, error) {
	type servarr struct {
		TvdbID int `json:"tvdbId,omitempty"`
		TmdbID int `json:"tmdbId,omitempty"`
	}
	url := fmt.Sprintf("%s%s", req.Url, req.Id)
	resp, err := t.http.Get(ctx, &http.HttpRequest{
		Url:       url,
		Headers:   map[string]string{"X-Api-Key": req.Apikey},
		TimeoutMs: -1,
	})
	if err != nil {
		return nil, err
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("[%v] %s", resp.Status, url)
	}

	var decodedResp []servarr
	err = json.Unmarshal(resp.Body, &decodedResp)
	if err != nil {
		return nil, err
	}

	if len(decodedResp) != 1 {
		return nil, fmt.Errorf("bad decoded length: %v", len(decodedResp))
	}

	return &testapi.SimpleResponse{Message: fmt.Sprintf("api result: %v", decodedResp[0].TmdbID)}, nil
}

func main() {}

var plugin = &FakeTest{
	cfg:   config.NewConfigService(),
	cache: cache.NewCacheService(),
	http:  http.NewHttpService(),
}

func init() {
	testapi.RegisterIntegrationTest(plugin)
}
