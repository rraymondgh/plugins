//go:build wasip1

package main

import (
	"context"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/api/testapi"
	"github.com/rraymondgh/plugins/host/http"
)

type UnauthorizedPlugin struct{}

var ErrNotFound = api.ErrNotFound

func (UnauthorizedPlugin) ConfigTest(ctx context.Context, req *testapi.SimpleRequest) (*testapi.SimpleResponse, error) {
	// This plugin attempts to make an HTTP call without having HTTP permission
	// This should fail since the plugin has no permissions in its manifest
	httpClient := http.NewHttpService()

	request := &http.HttpRequest{
		Url: "https://example.com/test",
		Headers: map[string]string{
			"Accept": "application/json",
		},
		TimeoutMs: 5000,
	}

	_, err := httpClient.Get(ctx, request)
	if err != nil {
		// Expected to fail due to missing permission
		return nil, err
	}

	return &testapi.SimpleResponse{
		Message: "https://example.com/unauthorized",
	}, nil
}

func (UnauthorizedPlugin) HTTPTest(_ context.Context, _ *testapi.HttpRequest) (*testapi.SimpleResponse, error) {
	return nil, nil
}

func main() {}

// Register the plugin implementation
func init() {
	testapi.RegisterIntegrationTest(UnauthorizedPlugin{})
}
