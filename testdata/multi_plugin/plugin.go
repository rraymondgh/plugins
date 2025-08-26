//go:build wasip1

package main

import (
	"context"
	"log"

	"github.com/rraymondgh/plugins/api"
)

// MultiPlugin implements the MetadataAgent interface for testing
type MultiPlugin struct{}

var ErrNotFound = api.ErrNotFound

func (MultiPlugin) SendTo(ctx context.Context, req *api.TestSendToRequest) (*api.TestSendToResponse, error) {
	return &api.TestSendToResponse{}, nil
}

func (MultiPlugin) OnInit(ctx context.Context, req *api.InitRequest) (*api.InitResponse, error) {
	log.Printf("OnInit called with %v", req)

	return &api.InitResponse{}, nil
}

// Required by Go WASI build
func main() {}

// Register the service implementations
func init() {
	api.RegisterLifecycleManagement(MultiPlugin{})
}
