package hub

import (
	"context"
	"sync"
	"time"
)

type HostProber interface {
	ProbeHost(context.Context, Host) HostSnapshot
}

type Coordinator struct {
	Inventory      *Inventory
	Probe          HostProber
	Services       ServiceManager
	Now            func() time.Time
	MaxConcurrency int

	mu         sync.Mutex
	inflight   chan struct{}
	lastResult []HostSnapshot
	lastGood   map[string]HostSnapshot
}

func NewCoordinator(inventory *Inventory, probe HostProber, services ServiceManager) *Coordinator {
	return &Coordinator{Inventory: inventory, Probe: probe, Services: services, Now: time.Now, MaxConcurrency: DefaultProbeConcurrency, lastGood: make(map[string]HostSnapshot)}
}

// Refresh coalesces overlapping callers onto one refresh and always returns
// snapshots in inventory declaration order.
func (c *Coordinator) Refresh(ctx context.Context) []HostSnapshot {
	c.mu.Lock()
	if active := c.inflight; active != nil {
		c.mu.Unlock()
		select {
		case <-active:
			c.mu.Lock()
			result := cloneHostSnapshots(c.lastResult)
			c.mu.Unlock()
			return result
		case <-ctx.Done():
			return nil
		}
	}
	c.inflight = make(chan struct{})
	active := c.inflight
	c.mu.Unlock()

	result := c.refresh(ctx)
	c.mu.Lock()
	c.lastResult = cloneHostSnapshots(result)
	c.inflight = nil
	close(active)
	c.mu.Unlock()
	return result
}

func (c *Coordinator) refresh(ctx context.Context) []HostSnapshot {
	if c == nil || c.Inventory == nil {
		return nil
	}
	result := make([]HostSnapshot, len(c.Inventory.Hosts))
	if len(result) == 0 {
		return result
	}
	limit := c.MaxConcurrency
	if limit <= 0 {
		limit = DefaultProbeConcurrency
	}
	if limit > len(result) {
		limit = len(result)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range limit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				host := c.Inventory.Hosts[index]
				snapshot := c.refreshHost(ctx, host)
				if snapshot.Error == "" {
					c.mu.Lock()
					if c.lastGood == nil {
						c.lastGood = make(map[string]HostSnapshot)
					}
					c.lastGood[host.ID] = cloneHostSnapshot(snapshot)
					c.mu.Unlock()
					result[index] = snapshot
					continue
				}
				c.mu.Lock()
				cached, ok := c.lastGood[host.ID]
				c.mu.Unlock()
				if ok {
					cached = cloneHostSnapshot(cached)
					cached.Stale = true
					cached.Error = snapshot.Error
					result[index] = cached
				} else {
					result[index] = unavailableHostSnapshot(host, snapshot.Error, c.now())
				}
			}
		}()
	}
	for index := range c.Inventory.Hosts {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return result
}

func (c *Coordinator) refreshHost(ctx context.Context, host Host) HostSnapshot {
	if c.Probe == nil {
		return unavailableHostSnapshot(host, "host probe unavailable", c.now())
	}
	snapshot := c.Probe.ProbeHost(ctx, host)
	snapshot.HostID = host.ID
	snapshot.Services = make([]ServiceSnapshot, len(host.Services))
	if snapshot.Error != "" {
		return snapshot
	}
	if c.Services == nil && len(host.Services) != 0 {
		snapshot.Error = "service inspection unavailable"
		return snapshot
	}
	for index, service := range host.Services {
		status, err := c.Services.Status(ctx, host, service)
		if err != nil {
			snapshot.Services[index] = ServiceSnapshot{ServiceID: service.ID, Unit: service.Unit, Error: "service status failed"}
			continue
		}
		snapshot.Services[index] = status
	}
	return snapshot
}

func (c *Coordinator) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func unavailableHostSnapshot(host Host, message string, collectedAt time.Time) HostSnapshot {
	services := make([]ServiceSnapshot, len(host.Services))
	for index, service := range host.Services {
		services[index] = ServiceSnapshot{ServiceID: service.ID, Unit: service.Unit}
	}
	return HostSnapshot{HostID: host.ID, CollectedAt: collectedAt, Error: message, Services: services}
}

func cloneHostSnapshots(values []HostSnapshot) []HostSnapshot {
	result := make([]HostSnapshot, len(values))
	for index, value := range values {
		result[index] = cloneHostSnapshot(value)
	}
	return result
}

func cloneHostSnapshot(value HostSnapshot) HostSnapshot {
	value.Services = append([]ServiceSnapshot(nil), value.Services...)
	return value
}
