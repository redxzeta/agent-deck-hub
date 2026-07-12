package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

const completeProbeOutput = "hostname=host-a\ntimestamp=1700000000\nuptime_seconds=12.5\nload1=1.25\nmemory_total_kib=1000\nmemory_available_kib=400\nroot_total_kib=2000\nroot_used_kib=500\n"

func TestParseLinuxProbeComplete(t *testing.T) {
	snapshot, err := parseLinuxProbe([]byte(completeProbeOutput), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Hostname.Available || snapshot.Hostname.Value != "host-a" || snapshot.Uptime.Value != 12500*time.Millisecond || snapshot.Load1.Value != 1.25 {
		t.Fatalf("basic snapshot = %#v", snapshot)
	}
	if snapshot.MemoryTotal.Value != 1000*1024 || snapshot.MemoryUsed.Value != 600*1024 || snapshot.RootDiskTotal.Value != 2000*1024 || snapshot.RootDiskUsed.Value != 500*1024 {
		t.Fatalf("byte snapshot = %#v", snapshot)
	}
	if got := snapshot.CollectedAt; !got.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("collected at = %s", got)
	}
}

func TestParseLinuxProbeRetainsIndependentFields(t *testing.T) {
	data := "hostname=host-a\nhostname=host-b\nload1=bad\nuptime_seconds=3\nmemory_total_kib=100\nmemory_available_kib=120\nroot_total_kib=18446744073709551615\nroot_used_kib=1\n"
	snapshot, err := parseLinuxProbe([]byte(data), time.Unix(1, 0))
	if err == nil {
		t.Fatal("expected parse warning")
	}
	if snapshot.Hostname.Available || snapshot.Load1.Available || snapshot.MemoryUsed.Available || snapshot.RootDiskTotal.Available {
		t.Fatalf("invalid fields became available: %#v", snapshot)
	}
	if !snapshot.Uptime.Available || snapshot.Uptime.Value != 3*time.Second {
		t.Fatalf("valid uptime was lost: %#v", snapshot.Uptime)
	}
}

type probeTransport struct {
	mu        sync.Mutex
	active    int
	maxActive int
	requests  []SSHRequest
	delay     time.Duration
	fail      map[string]error
}

func (t *probeTransport) Run(_ context.Context, request SSHRequest) (CommandResult, error) {
	t.mu.Lock()
	t.active++
	if t.active > t.maxActive {
		t.maxActive = t.active
	}
	t.requests = append(t.requests, request)
	t.mu.Unlock()
	time.Sleep(t.delay)
	t.mu.Lock()
	t.active--
	err := t.fail[request.Target]
	t.mu.Unlock()
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Stdout: []byte(completeProbeOutput)}, nil
}

func TestLinuxProbeUsesFixedBoundedRequest(t *testing.T) {
	transport := &probeTransport{}
	clock := time.Unix(100, 0)
	probe := NewLinuxProbe(transport)
	probe.Now = func() time.Time { clock = clock.Add(5 * time.Millisecond); return clock }
	snapshot := probe.ProbeHost(context.Background(), Host{ID: "one", Target: "alias"})
	if snapshot.HostID != "one" || !snapshot.Latency.Available || snapshot.Latency.Value != 5*time.Millisecond || snapshot.Error != "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	request := transport.requests[0]
	if request.Target != "alias" || request.Command.value != linuxProbeScript || request.StdoutLimit != probeOutputLimit || request.StderrLimit != probeOutputLimit {
		t.Fatalf("request = %#v", request)
	}
}

func TestLinuxProbeHostsBoundsConcurrencyPreservesOrderAndIsolatesFailure(t *testing.T) {
	transport := &probeTransport{delay: 10 * time.Millisecond, fail: map[string]error{"bad": errors.New("secret raw ssh error")}}
	probe := NewLinuxProbe(transport)
	probe.MaxConcurrency = 2
	hosts := []Host{{ID: "first", Target: "a"}, {ID: "second", Target: "bad"}, {ID: "third", Target: "c"}, {ID: "fourth", Target: "d"}}
	results := probe.ProbeHosts(context.Background(), hosts)
	if transport.maxActive > 2 {
		t.Fatalf("max concurrency = %d", transport.maxActive)
	}
	for index, result := range results {
		if result.HostID != hosts[index].ID {
			t.Fatalf("result %d id = %q", index, result.HostID)
		}
	}
	if results[1].Error != "host probe failed" || results[1].Hostname.Available {
		t.Fatalf("failed host = %#v", results[1])
	}
	if !results[0].Hostname.Available || !results[2].Hostname.Available || !results[3].Hostname.Available {
		t.Fatalf("healthy hosts were affected: %#v", results)
	}
}
