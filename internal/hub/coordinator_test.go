package hub

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type coordinatorProbe struct {
	mu      sync.Mutex
	results map[string]HostSnapshot
	calls   atomic.Int32
	gate    chan struct{}
}

func (p *coordinatorProbe) ProbeHost(_ context.Context, host Host) HostSnapshot {
	p.calls.Add(1)
	if p.gate != nil {
		<-p.gate
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	result := p.results[host.ID]
	result.HostID = host.ID
	return result
}

type coordinatorServices struct {
	fail bool
}

func (s *coordinatorServices) Status(_ context.Context, _ Host, service Service) (ServiceSnapshot, error) {
	if s.fail {
		return ServiceSnapshot{}, errors.New("raw remote failure")
	}
	return ServiceSnapshot{ServiceID: service.ID, Unit: service.Unit, ActiveState: Available[string]{Value: "active", Available: true}, SubState: Available[string]{Value: "running", Available: true}}, nil
}
func (*coordinatorServices) Logs(context.Context, Host, Service) ([]byte, error) { return nil, nil }
func (*coordinatorServices) Restart(context.Context, Host, Service) error        { return nil }

func coordinatorInventory() *Inventory {
	return &Inventory{Hosts: []Host{
		{ID: "first", Target: "a", Services: []Service{{ID: "api", Unit: "api.service", Scope: ScopeSystem, Actions: []Action{ActionStatus}}}},
		{ID: "second", Target: "b"},
	}}
}

func TestCoordinatorPreservesOrderAndServiceOrder(t *testing.T) {
	probe := &coordinatorProbe{results: map[string]HostSnapshot{
		"first":  {Hostname: Available[string]{Value: "one", Available: true}},
		"second": {Hostname: Available[string]{Value: "two", Available: true}},
	}}
	coordinator := NewCoordinator(coordinatorInventory(), probe, &coordinatorServices{})
	result := coordinator.Refresh(context.Background())
	if len(result) != 2 || result[0].HostID != "first" || result[1].HostID != "second" || result[0].Services[0].ServiceID != "api" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCoordinatorRetainsLastGoodAsStale(t *testing.T) {
	probe := &coordinatorProbe{results: map[string]HostSnapshot{"first": {Hostname: Available[string]{Value: "one", Available: true}}, "second": {}}}
	coordinator := NewCoordinator(coordinatorInventory(), probe, &coordinatorServices{})
	first := coordinator.Refresh(context.Background())
	if first[0].Error != "" {
		t.Fatalf("initial refresh = %#v", first[0])
	}
	probe.mu.Lock()
	probe.results["first"] = HostSnapshot{Error: "host probe failed"}
	probe.mu.Unlock()
	second := coordinator.Refresh(context.Background())
	if !second[0].Stale || second[0].Error != "host probe failed" || second[0].Hostname.Value != "one" {
		t.Fatalf("stale refresh = %#v", second[0])
	}
	second[0].Services[0].ServiceID = "mutated"
	third := coordinator.Refresh(context.Background())
	if third[0].Services[0].ServiceID != "api" {
		t.Fatal("caller mutated cached snapshot")
	}
}

func TestCoordinatorServiceFailureDoesNotPoisonHostSnapshot(t *testing.T) {
	probe := &coordinatorProbe{results: map[string]HostSnapshot{
		"first": {Hostname: Available[string]{Value: "one", Available: true}}, "second": {},
	}}
	coordinator := NewCoordinator(coordinatorInventory(), probe, &coordinatorServices{fail: true})
	result := coordinator.Refresh(context.Background())
	if result[0].Error != "" || result[0].Stale || result[0].Hostname.Value != "one" || result[0].Services[0].Error != "service status failed" {
		t.Fatalf("service failure poisoned host: %#v", result[0])
	}
}

func TestCoordinatorServiceFailureDoesNotReplaceCompleteCache(t *testing.T) {
	probe := &coordinatorProbe{results: map[string]HostSnapshot{
		"first": {Hostname: Available[string]{Value: "one", Available: true}}, "second": {},
	}}
	services := &coordinatorServices{}
	coordinator := NewCoordinator(coordinatorInventory(), probe, services)
	initial := coordinator.Refresh(context.Background())
	if initial[0].Services[0].ActiveState.Value != "active" {
		t.Fatalf("initial = %#v", initial[0])
	}
	services.fail = true
	partial := coordinator.Refresh(context.Background())
	if partial[0].Services[0].Error != "service status failed" || partial[0].Stale {
		t.Fatalf("partial = %#v", partial[0])
	}
	probe.mu.Lock()
	probe.results["first"] = HostSnapshot{Error: "host probe failed"}
	probe.mu.Unlock()
	stale := coordinator.Refresh(context.Background())
	if !stale[0].Stale || stale[0].Services[0].Error != "" || stale[0].Services[0].ActiveState.Value != "active" {
		t.Fatalf("complete cache was replaced: %#v", stale[0])
	}
}

func TestCoordinatorFirstFailureIsUnavailableAndSanitized(t *testing.T) {
	probe := &coordinatorProbe{results: map[string]HostSnapshot{"first": {Error: "host probe failed"}, "second": {}}}
	coordinator := NewCoordinator(coordinatorInventory(), probe, &coordinatorServices{})
	result := coordinator.Refresh(context.Background())
	if result[0].Hostname.Available || result[0].Error != "host probe failed" || result[0].Services[0].ServiceID != "api" {
		t.Fatalf("failure snapshot = %#v", result[0])
	}
}

func TestCoordinatorCoalescesOverlappingRefreshes(t *testing.T) {
	gate := make(chan struct{})
	probe := &coordinatorProbe{gate: gate, results: map[string]HostSnapshot{"first": {}, "second": {}}}
	coordinator := NewCoordinator(coordinatorInventory(), probe, &coordinatorServices{})
	results := make(chan []HostSnapshot, 2)
	go func() { results <- coordinator.Refresh(context.Background()) }()
	for probe.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	go func() { results <- coordinator.Refresh(context.Background()) }()
	time.Sleep(10 * time.Millisecond)
	close(gate)
	first, second := <-results, <-results
	if probe.calls.Load() != 2 || len(first) != 2 || len(second) != 2 {
		t.Fatalf("calls=%d first=%d second=%d", probe.calls.Load(), len(first), len(second))
	}
}

func TestCoordinatorBoundsHostConcurrency(t *testing.T) {
	gate := make(chan struct{})
	probe := &coordinatorProbe{gate: gate, results: map[string]HostSnapshot{"a": {}, "b": {}, "c": {}, "d": {}}}
	inventory := &Inventory{Hosts: []Host{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}}
	coordinator := NewCoordinator(inventory, probe, &coordinatorServices{})
	coordinator.MaxConcurrency = 2
	done := make(chan []HostSnapshot, 1)
	go func() { done <- coordinator.Refresh(context.Background()) }()
	deadline := time.After(time.Second)
	for probe.calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("two workers did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(10 * time.Millisecond)
	if got := probe.calls.Load(); got != 2 {
		t.Fatalf("active calls = %d, want bounded at 2", got)
	}
	close(gate)
	if result := <-done; len(result) != 4 {
		t.Fatalf("results = %d", len(result))
	}
}
