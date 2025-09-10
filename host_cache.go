package plugins

import (
	"context"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	cacheproto "github.com/rraymondgh/plugins/host/cache"
	"go.uber.org/zap"
)

const (
	defaultCacheTTL = 24 * time.Hour
)

// cacheServiceImpl implements the cache.CacheService interface
type cacheServiceImpl struct {
	pluginID   string
	defaultTTL time.Duration
}

var (
	_cache        *ttlcache.Cache[string, any]
	initCacheOnce sync.Once
)

// newCacheService creates a new cacheServiceImpl instance
func newCacheService(pluginID string) *cacheServiceImpl {
	initCacheOnce.Do(func() {
		opts := []ttlcache.Option[string, any]{
			ttlcache.WithTTL[string, any](defaultCacheTTL),
		}
		_cache = ttlcache.New[string, any](opts...)

		// Start the janitor goroutine to clean up expired entries
		go _cache.Start()
	})

	return &cacheServiceImpl{
		pluginID:   pluginID,
		defaultTTL: defaultCacheTTL,
	}
}

// mapKey combines the plugin name and a provided key to create a unique cache key.
func (s *cacheServiceImpl) mapKey(key string) string {
	return s.pluginID + ":" + key
}

// getTTL converts seconds to a duration, using default if 0
func (s *cacheServiceImpl) getTTL(seconds int64) time.Duration {
	if seconds <= 0 {
		return s.defaultTTL
	}

	return time.Duration(seconds) * time.Second
}

// setCacheValue is a generic function to set a value in the cache
func setCacheValue[T any](
	_ context.Context,
	cs *cacheServiceImpl,
	key string,
	value T,
	ttlSeconds int64,
) (*cacheproto.SetResponse, error) {
	ttl := cs.getTTL(ttlSeconds)
	key = cs.mapKey(key)
	_cache.Set(key, value, ttl)

	return &cacheproto.SetResponse{Success: true}, nil
}

// getCacheValue is a generic function to get a value from the cache
func getCacheValue[T any](_ context.Context, cs *cacheServiceImpl, key string, typeName string) (T, bool, error) {
	key = cs.mapKey(key)

	var zero T

	item := _cache.Get(key)
	if item == nil {
		return zero, false, nil
	}

	value, ok := item.Value().(T)
	if !ok {
		zap.L().
			Debug("Type mismatch in cache",
				zap.String("plugin", cs.pluginID),
				zap.String("key", key), zap.String("expected", typeName))

		return zero, false, nil
	}

	return value, true, nil
}

// SetString sets a string value in the cache
func (s *cacheServiceImpl) SetString(
	ctx context.Context,
	req *cacheproto.SetStringRequest,
) (*cacheproto.SetResponse, error) {
	return setCacheValue(ctx, s, req.GetKey(), req.GetValue(), req.GetTtlSeconds())
}

// GetString gets a string value from the cache
func (s *cacheServiceImpl) GetString(
	ctx context.Context,
	req *cacheproto.GetRequest,
) (*cacheproto.GetStringResponse, error) {
	value, exists, err := getCacheValue[string](ctx, s, req.GetKey(), "string")
	if err != nil {
		return nil, err
	}

	return &cacheproto.GetStringResponse{Exists: exists, Value: value}, nil
}

// SetInt sets an integer value in the cache
func (s *cacheServiceImpl) SetInt(ctx context.Context, req *cacheproto.SetIntRequest) (*cacheproto.SetResponse, error) {
	return setCacheValue(ctx, s, req.GetKey(), req.GetValue(), req.GetTtlSeconds())
}

// GetInt gets an integer value from the cache
func (s *cacheServiceImpl) GetInt(ctx context.Context, req *cacheproto.GetRequest) (*cacheproto.GetIntResponse, error) {
	value, exists, err := getCacheValue[int64](ctx, s, req.GetKey(), "int64")
	if err != nil {
		return nil, err
	}

	return &cacheproto.GetIntResponse{Exists: exists, Value: value}, nil
}

// SetFloat sets a float value in the cache
func (s *cacheServiceImpl) SetFloat(
	ctx context.Context,
	req *cacheproto.SetFloatRequest,
) (*cacheproto.SetResponse, error) {
	return setCacheValue(ctx, s, req.GetKey(), req.GetValue(), req.GetTtlSeconds())
}

// GetFloat gets a float value from the cache
func (s *cacheServiceImpl) GetFloat(
	ctx context.Context,
	req *cacheproto.GetRequest,
) (*cacheproto.GetFloatResponse, error) {
	value, exists, err := getCacheValue[float64](ctx, s, req.GetKey(), "float64")
	if err != nil {
		return nil, err
	}

	return &cacheproto.GetFloatResponse{Exists: exists, Value: value}, nil
}

// SetBytes sets a byte slice value in the cache
func (s *cacheServiceImpl) SetBytes(
	ctx context.Context,
	req *cacheproto.SetBytesRequest,
) (*cacheproto.SetResponse, error) {
	return setCacheValue(ctx, s, req.GetKey(), req.GetValue(), req.GetTtlSeconds())
}

// GetBytes gets a byte slice value from the cache
func (s *cacheServiceImpl) GetBytes(
	ctx context.Context,
	req *cacheproto.GetRequest,
) (*cacheproto.GetBytesResponse, error) {
	value, exists, err := getCacheValue[[]byte](ctx, s, req.GetKey(), "[]byte")
	if err != nil {
		return nil, err
	}

	return &cacheproto.GetBytesResponse{Exists: exists, Value: value}, nil
}

// Remove removes a value from the cache
func (s *cacheServiceImpl) Remove(
	_ context.Context,
	req *cacheproto.RemoveRequest,
) (*cacheproto.RemoveResponse, error) {
	key := s.mapKey(req.GetKey())
	_cache.Delete(key)

	return &cacheproto.RemoveResponse{Success: true}, nil
}

// Has checks if a key exists in the cache
func (s *cacheServiceImpl) Has(_ context.Context, req *cacheproto.HasRequest) (*cacheproto.HasResponse, error) {
	key := s.mapKey(req.GetKey())
	item := _cache.Get(key)

	return &cacheproto.HasResponse{Exists: item != nil}, nil
}
