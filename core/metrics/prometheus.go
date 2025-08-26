package metrics

import "context"

type Metrics interface {
	RecordPluginRequest(ctx context.Context, plugin, method string, ok bool, elapsed int64)
}

func GetPrometheusInstance() Metrics {
	return noopMetrics{}
}

func NewNoopInstance() Metrics {
	return noopMetrics{}
}

type noopMetrics struct{}

func (noopMetrics) RecordPluginRequest(context.Context, string, string, bool, int64) {}
