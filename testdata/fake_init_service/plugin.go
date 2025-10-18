//go:build wasip1

package main

import (
	"context"
	"errors"
	"log"

	"github.com/rraymondgh/plugins/api/lifecycleapi"
)

type initServicePlugin struct{}

func (p *initServicePlugin) OnInit(ctx context.Context, req *lifecycleapi.InitRequest) (*lifecycleapi.InitResponse, error) {
	log.Printf("OnInit called with %v", req)

	// Check for specific error conditions in the config
	if req.Config != nil {
		if errorType, exists := req.Config["returnError"]; exists {
			switch errorType {
			case "go_error":
				return nil, errors.New("initialization failed with Go error")
			case "response_error":
				return &lifecycleapi.InitResponse{
					Error: "initialization failed with response error",
				}, nil
			}
		}
	}

	// Default: successful initialization
	return &lifecycleapi.InitResponse{}, nil
}

// Required by Go WASI build
func main() {}

// Register the LifecycleManagement implementation
func init() {
	lifecycleapi.RegisterLifecycleManagement(&initServicePlugin{})
}
