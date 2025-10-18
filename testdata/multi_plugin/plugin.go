//go:build wasip1

package main

import (
	"context"
	"log"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/api/lifecycleapi"
	"github.com/rraymondgh/plugins/api/testapi"
)

// MultiPlugin implements the MetadataAgent interface for testing
type MultiPlugin struct{}

var ErrNotFound = api.ErrNotFound

func (MultiPlugin) ConfigTest(ctx context.Context, req *testapi.SimpleRequest) (*testapi.SimpleResponse, error) {
	return &testapi.SimpleResponse{}, nil
}

func (MultiPlugin) OnInit(ctx context.Context, req *lifecycleapi.InitRequest) (*lifecycleapi.InitResponse, error) {
	log.Printf("OnInit called with %v", req)

	return &lifecycleapi.InitResponse{}, nil
}

// Required by Go WASI build
func main() {}

// Register the service implementations
func init() {
	lifecycleapi.RegisterLifecycleManagement(MultiPlugin{})
}
