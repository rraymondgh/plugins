package plugins

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"net/http"
	"time"

	hosthttp "github.com/rraymondgh/plugins/host/http"
	"go.uber.org/zap"
)

type httpServiceImpl struct {
	pluginID    string
	permissions *httpPermissions
}

const defaultTimeout = 10 * time.Second

func (s *httpServiceImpl) Get(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodGet, req)
}

func (s *httpServiceImpl) Post(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodPost, req)
}

func (s *httpServiceImpl) Put(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodPut, req)
}

func (s *httpServiceImpl) Delete(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodDelete, req)
}

func (s *httpServiceImpl) Patch(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodPatch, req)
}

func (s *httpServiceImpl) Head(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodHead, req)
}

func (s *httpServiceImpl) Options(ctx context.Context, req *hosthttp.HttpRequest) (*hosthttp.HttpResponse, error) {
	return s.doHTTP(ctx, http.MethodOptions, req)
}

func (s *httpServiceImpl) doHTTP(
	ctx context.Context,
	method string,
	req *hosthttp.HttpRequest,
) (*hosthttp.HttpResponse, error) {
	// Check permissions if they exist
	if s.permissions != nil {
		if err := s.permissions.IsRequestAllowed(req.GetUrl(), method); err != nil {
			zap.L().
				Warn("HTTP request blocked by permissions", zap.String("plugin", s.pluginID),
					zap.String("url", req.GetUrl()), zap.String("method", method), zap.Error(err))

			return &hosthttp.HttpResponse{
				Error: "Request blocked by plugin permissions: " + err.Error(),
			}, nil
		}
	}

	client := &http.Client{
		Timeout: cmp.Or(time.Duration(req.GetTimeoutMs())*time.Millisecond, defaultTimeout),
	}

	// Configure redirect policy based on permissions
	if s.permissions != nil {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			// Enforce maximum redirect limit
			if len(via) >= httpMaxRedirects {
				zap.L().
					Warn("HTTP redirect limit exceeded", zap.String("plugin", s.pluginID),
						zap.String("url", req.URL.String()), zap.Int("redirectCount", len(via)))

				return http.ErrUseLastResponse
			}

			// Check if redirect destination is allowed
			if err := s.permissions.IsRequestAllowed(req.URL.String(), req.Method); err != nil {
				zap.L().
					Warn("HTTP redirect blocked by permissions", zap.String("plugin", s.pluginID),
						zap.String("url", req.URL.String()),
						zap.String("method", req.Method), zap.Error(err))

				return http.ErrUseLastResponse
			}

			return nil // Allow redirect
		}
	}

	var body io.Reader
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		body = bytes.NewReader(req.GetBody())
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.GetUrl(), body)
	if err != nil {
		return nil, err
	}

	for k, v := range req.GetHeaders() {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		zap.L().
			Debug("HttpService request error", zap.String("method", method),
				zap.String("url", req.GetUrl()), zap.Any("headers", req.GetHeaders()), zap.Error(err))

		return &hosthttp.HttpResponse{Error: err.Error()}, nil
	}

	zap.L().
		Debug("HttpService request", zap.String("method", method), zap.String("url", req.GetUrl()),
			zap.Any("headers", req.GetHeaders()), zap.Int("resp.status", resp.StatusCode))

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		zap.L().
			Debug("HttpService request error", zap.String("method", method), zap.String("url", req.GetUrl()),
				zap.Any("headers", req.GetHeaders()),
				zap.Int("resp.status", resp.StatusCode), zap.Error(err))

		return &hosthttp.HttpResponse{Error: err.Error()}, nil
	}

	headers := map[string]string{}

	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &hosthttp.HttpResponse{
		Status:  int32(resp.StatusCode),
		Body:    respBody,
		Headers: headers,
	}, nil
}
