package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// wasmInstancePool is a generic pool using channels for simplicity and Go idioms
type wasmInstancePool[T any] struct {
	name       string
	new        func(ctx context.Context) (T, error)
	poolSize   int
	getTimeout time.Duration
	ttl        time.Duration

	mu        sync.RWMutex
	instances chan poolItem[T]
	semaphore chan struct{}
	closing   chan struct{}
	closed    bool
}

type poolItem[T any] struct {
	value   T
	created time.Time
}

func newWasmInstancePool[T any](
	name string,
	poolSize int,
	maxConcurrentInstances int,
	getTimeout time.Duration,
	ttl time.Duration,
	newFn func(ctx context.Context) (T, error),
) *wasmInstancePool[T] {
	p := &wasmInstancePool[T]{
		name:       name,
		new:        newFn,
		poolSize:   poolSize,
		getTimeout: getTimeout,
		ttl:        ttl,
		instances:  make(chan poolItem[T], poolSize),
		semaphore:  make(chan struct{}, maxConcurrentInstances),
		closing:    make(chan struct{}),
	}

	// Fill semaphore to allow maxConcurrentInstances
	for range maxConcurrentInstances {
		p.semaphore <- struct{}{}
	}

	zap.L().
		Debug("wasmInstancePool: created new pool", zap.String("pool",
			p.name), zap.Int("poolSize", p.poolSize), zap.Int("maxConcurrentInstances",
			maxConcurrentInstances), zap.Duration("getTimeout", p.getTimeout), zap.Duration("ttl", p.ttl))

	go p.cleanupLoop()

	return p
}

func getInstanceID(inst any) string {
	return fmt.Sprintf("%p", inst)
}

func (p *wasmInstancePool[T]) Get(ctx context.Context) (T, error) {
	// First acquire a semaphore slot (concurrent limit)
	select {
	case <-p.semaphore:
		// Got slot, continue
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-time.After(p.getTimeout):
		var zero T
		return zero, fmt.Errorf("timeout waiting for available instance after %v", p.getTimeout)
	case <-p.closing:
		var zero T
		return zero, fmt.Errorf("pool is closing")
	}

	// Try to get from pool first
	p.mu.RLock()
	instances := p.instances
	p.mu.RUnlock()

	select {
	case item := <-instances:
		zap.L().Debug("wasmInstancePool: got instance from pool",
			zap.String("pool", p.name), zap.String("instanceID", getInstanceID(item.value)))
		return item.value, nil
	default:
		// Pool empty, create new instance
		instance, err := p.new(ctx)
		if err != nil {
			// Failed to create, return semaphore slot
			zap.L().
				Debug("wasmInstancePool: failed to create new instance", zap.String("pool", p.name), zap.Error(err))
			p.semaphore <- struct{}{}

			var zero T

			return zero, err
		}

		zap.L().Debug("wasmInstancePool: new instance created",
			zap.String("pool", p.name), zap.String("instanceID", getInstanceID(instance)))

		return instance, nil
	}
}

func (p *wasmInstancePool[T]) Put(ctx context.Context, v T) {
	p.mu.RLock()
	instances := p.instances
	closed := p.closed
	p.mu.RUnlock()

	if closed {
		zap.L().Debug(
			"wasmInstancePool: pool closed, closing instance",
			zap.String("pool",
				p.name),
			zap.String("instanceID",
				getInstanceID(v)),
		)
		p.closeItem(ctx, v)
		// Return semaphore slot only if this instance came from Get()
		select {
		case p.semaphore <- struct{}{}:
		case <-p.closing:
		default: // Semaphore full, this instance didn't come from Get()
		}

		return
	}

	// Try to return to pool
	item := poolItem[T]{value: v, created: time.Now()}
	select {
	case instances <- item:
		zap.L().Debug("wasmInstancePool: returned instance to pool",
			zap.String("pool", p.name), zap.String("instanceID", getInstanceID(v)))
	default:
		// Pool full, close instance
		zap.L().Debug("wasmInstancePool: pool full, closing instance",
			zap.String("pool", p.name), zap.String("instanceID", getInstanceID(v)))
		p.closeItem(ctx, v)
	}

	// Return semaphore slot only if this instance came from Get()
	// If semaphore is full, this instance didn't come from Get(), so don't block
	select {
	case p.semaphore <- struct{}{}:
		// Successfully returned token
	case <-p.closing:
		// Pool closing, don't block
	default: // Semaphore full, this instance didn't come from Get()
	}
}

func (p *wasmInstancePool[T]) Close(ctx context.Context) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}

	p.closed = true
	close(p.closing)
	instances := p.instances
	p.mu.Unlock()

	zap.L().Debug("wasmInstancePool: closing pool and all instances", zap.String("pool", p.name))

	// Drain and close all instances
	for {
		select {
		case item := <-instances:
			p.closeItem(ctx, item.value)
		default:
			return
		}
	}
}

func (p *wasmInstancePool[T]) cleanupLoop() {
	ticker := time.NewTicker(p.ttl / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanupExpired()
		case <-p.closing:
			return
		}
	}
}

func (p *wasmInstancePool[T]) cleanupExpired() {
	ctx := context.Background()
	now := time.Now()

	// Create new channel with same capacity
	newInstances := make(chan poolItem[T], p.poolSize)

	// Atomically swap channels
	p.mu.Lock()
	oldInstances := p.instances
	p.instances = newInstances
	p.mu.Unlock()

	// Drain old channel, keeping fresh items
	var expiredCount int

	for {
		select {
		case item := <-oldInstances:
			if now.Sub(item.created) <= p.ttl {
				// Item is still fresh, move to new channel
				select {
				case newInstances <- item:
					// Successfully moved
				default:
					// New channel full, close excess item
					p.closeItem(ctx, item.value)
				}
			} else {
				// Item expired, close it
				expiredCount++

				p.closeItem(ctx, item.value)
			}
		default:
			// Old channel drained
			if expiredCount > 0 {
				zap.L().Debug(
					"wasmInstancePool: cleaned up expired instances",
					zap.String("pool",
						p.name),
					zap.Int("expiredCount",
						expiredCount),
				)
			}

			return
		}
	}
}

func (*wasmInstancePool[T]) closeItem(ctx context.Context, v T) {
	if closer, ok := any(v).(interface{ Close(context.Context) error }); ok {
		_ = closer.Close(ctx)
	}
}
