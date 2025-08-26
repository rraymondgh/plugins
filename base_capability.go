package plugins

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rraymondgh/plugins/api"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/rraymondgh/plugins/utils"
	"go.uber.org/zap"
)

// NewBaseCapability creates a new instance of baseCapability with the required parameters.
func NewBaseCapability[S any, P any](
	wasmPath, id, capability string,
	m metrics.Metrics,
	loader P,
	loadFunc loaderFunc[S, P],
) *BaseCapability[S, P] {
	return &BaseCapability[S, P]{
		wasmPath:   wasmPath,
		id:         id,
		capability: capability,
		loader:     loader,
		loadFunc:   loadFunc,
		metrics:    m,
	}
}

// LoaderFunc is a generic function type that loads a plugin instance.
type loaderFunc[S any, P any] func(ctx context.Context, loader P, path string) (S, error)

// BaseCapability is a generic base implementation for WASM plugins.
// S is the capability interface type and P is the plugin loader type.
type BaseCapability[S any, P any] struct {
	wasmPath   string
	id         string
	capability string
	loader     P
	loadFunc   loaderFunc[S, P]
	metrics    metrics.Metrics
}

func (w *BaseCapability[S, P]) PluginID() string {
	return w.id
}

func (w *BaseCapability[S, P]) serviceName() string {
	return w.id + "_" + w.capability
}

func (w *BaseCapability[S, P]) getMetrics() metrics.Metrics {
	return w.metrics
}

// getInstance loads a new plugin instance and returns a cleanup function.
func (w *BaseCapability[S, P]) getInstance(ctx context.Context, _ string) (S, func(), error) {
	start := time.Now()
	// Add context metadata for tracing
	// ctx = log.NewContext(ctx, "capability", w.serviceName(), "method", methodName)

	inst, err := w.loadFunc(ctx, w.loader, w.wasmPath)
	if err != nil {
		var zero S

		return zero, func() {}, fmt.Errorf(
			"baseCapability: failed to load instance for %s: %w",
			w.serviceName(),
			err,
		)
	}
	// Add context metadata for tracing
	// ctx = log.NewContext(ctx, "instanceID", getInstanceID(inst))
	log := zap.L().Named(getInstanceID(inst))
	log.Debug("baseCapability: loaded instance", zap.Duration("elapsed", time.Since(start)))

	return inst, func() {
		log.Debug("baseCapability: finished using instance", zap.Duration("elapsed", time.Since(start)))

		if closer, ok := any(inst).(interface{ Close(context.Context) error }); ok {
			_ = closer.Close(ctx)
		}
	}, nil
}

type wasmPlugin[S any] interface {
	PluginID() string
	getInstance(ctx context.Context, methodName string) (S, func(), error)
	getMetrics() metrics.Metrics
}

func CallMethod[S any, R any](
	ctx context.Context,
	wp WasmPlugin,
	methodName string,
	fn func(inst S) (R, error),
) (R, error) {
	// Add a unique call ID to the name of logger for tracing
	var r R

	log := zap.L().Named(utils.NewRandom())

	p, ok := wp.(wasmPlugin[S])
	if !ok {
		log.Error(
			"callMethod: not a wasm plugin",
			zap.String("method", methodName),
			zap.String("pluginID", wp.PluginID()),
		)

		return r, fmt.Errorf("wasm plugin: not a wasm plugin: %s", wp.PluginID())
	}

	inst, done, err := p.getInstance(ctx, methodName)
	if err != nil {
		return r, err
	}

	start := time.Now()

	defer done()

	r, err = checkErr(fn(inst))
	elapsed := time.Since(start)

	if !errors.Is(err, api.ErrNotImplemented) {
		id := p.PluginID()
		isOk := err == nil
		metrics := p.getMetrics()

		if metrics != nil {
			metrics.RecordPluginRequest(ctx, id, methodName, isOk, elapsed.Milliseconds())
			log.
				Debug(
					"callMethod: sending metrics",
					zap.String("plugin", id),
					zap.String(
						"method",
						methodName,
					),
					zap.Bool("ok", isOk),
					zap.Duration("elapsed", elapsed),
				)
		}
	}

	return r, err
}

// errorResponse is an interface that defines a method to retrieve an error message.
// It is automatically implemented (generated) by all plugin responses that have an Error field
type errorResponse interface {
	GetError() string
}

// checkErr returns an updated error if the response implements errorResponse and contains an error message.
// If the response is nil, it returns the original error. Otherwise, it wraps or creates an error as needed.
// It also maps error strings to their corresponding api.Err* constants.
func checkErr[T any](resp T, err error) (T, error) {
	if any(resp) == nil {
		return resp, mapAPIError(err)
	}

	respErr, ok := any(resp).(errorResponse)
	if ok && respErr.GetError() != "" {
		respErrMsg := respErr.GetError()
		respErrErr := errors.New(respErrMsg)
		mappedErr := mapAPIError(respErrErr)
		// Check if the error was mapped to an API error (different from the temp error)
		if errors.Is(mappedErr, api.ErrNotImplemented) || errors.Is(mappedErr, api.ErrNotFound) {
			// Return the mapped API error instead of wrapping
			return resp, mappedErr
		}
		// For non-API errors, use wrap the original error if it is not nil
		return resp, errors.Join(respErrErr, err)
	}

	return resp, mapAPIError(err)
}

// mapAPIError maps error strings to their corresponding api.Err* constants.
// This is needed as errors from plugins may not be of type api.Error, due to serialization/deserialization.
func mapAPIError(err error) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()
	switch errStr {
	case api.ErrNotImplemented.Error():
		return api.ErrNotImplemented
	case api.ErrNotFound.Error():
		return api.ErrNotFound
	default:
		return err
	}
}
