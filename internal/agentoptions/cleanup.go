package agentoptions

import (
	"context"
	"errors"
	"sync"
)

// DiscoveryCleanup owns authenticated probes after their catalogs are returned.
// Its zero value is ready to use. Owners must call Close before process exit.
type DiscoveryCleanup struct {
	mu      sync.Mutex
	pending int
	changed chan struct{}
	closed  bool
	err     error
}

func (c *DiscoveryCleanup) begin() (func(error), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("model discovery cleanup is closed")
	}
	if c.changed == nil {
		c.changed = make(chan struct{})
	}
	c.pending++
	return func(err error) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.pending--
		c.err = errors.Join(c.err, err)
		close(c.changed)
		c.changed = make(chan struct{})
	}, nil
}

// Wait waits for registered probes to exit before another credential consumer
// starts. Callers must serialize discovery and launch around this barrier.
// Shutdown errors are reported once, after every pending probe has finished.
func (c *DiscoveryCleanup) Wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return err
		}
		if c.pending == 0 {
			err := c.err
			c.err = nil
			c.mu.Unlock()
			return err
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Close prevents further probes and joins all pending cleanup without a kill
// deadline. It is safe to call concurrently with discovery registration.
func (c *DiscoveryCleanup) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.Wait(context.Background())
}
