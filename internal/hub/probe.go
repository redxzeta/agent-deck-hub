package hub

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultProbeConcurrency = 4
	probeOutputLimit        = 16 * 1024
)

// linuxProbeScript is immutable and contains no inventory-derived text. Values
// are emitted one per line for a parser that never evaluates remote output.
const linuxProbeScript = `set -eu; printf 'hostname=%s\n' "$(hostname)"; printf 'timestamp=%s\n' "$(date +%s)"; awk '{printf "uptime_seconds=%s\n",$1}' /proc/uptime; awk '{printf "load1=%s\n",$1}' /proc/loadavg; awk '/^MemTotal:/{t=$2}/^MemAvailable:/{a=$2}END{printf "memory_total_kib=%s\nmemory_available_kib=%s\n",t,a}' /proc/meminfo; df -Pk / | awk 'NR==2{printf "root_total_kib=%s\nroot_used_kib=%s\n",$2,$3}'`

var linuxProbeCommand = fixedRemoteCommand(linuxProbeScript)

type SSHExecutor interface {
	Run(context.Context, SSHRequest) (CommandResult, error)
}

type LinuxProbe struct {
	Transport      SSHExecutor
	Now            func() time.Time
	Timeout        time.Duration
	MaxConcurrency int
}

func NewLinuxProbe(transport SSHExecutor) *LinuxProbe {
	return &LinuxProbe{Transport: transport, Now: time.Now, Timeout: DefaultSSHTimeout, MaxConcurrency: DefaultProbeConcurrency}
}

func (p *LinuxProbe) ProbeHost(ctx context.Context, host Host) HostSnapshot {
	now := time.Now
	if p != nil && p.Now != nil {
		now = p.Now
	}
	snapshot := HostSnapshot{HostID: host.ID, CollectedAt: now()}
	if p == nil || p.Transport == nil {
		snapshot.Error = "host probe transport is unavailable"
		return snapshot
	}
	started := now()
	result, err := p.Transport.Run(ctx, SSHRequest{
		Target: host.Target, Command: linuxProbeCommand, Timeout: p.Timeout,
		StdoutLimit: probeOutputLimit, StderrLimit: probeOutputLimit,
	})
	elapsed := now().Sub(started)
	if err != nil {
		snapshot.Error = "host probe failed"
		return snapshot
	}
	if elapsed >= 0 {
		snapshot.Latency = Available[time.Duration]{Value: elapsed, Available: true}
	}
	parsed, parseErr := parseLinuxProbe(result.Stdout, snapshot.CollectedAt)
	parsed.HostID = host.ID
	parsed.Latency = snapshot.Latency
	if parseErr != nil {
		parsed.Error = parseErr.Error()
	}
	return parsed
}

func (p *LinuxProbe) ProbeHosts(ctx context.Context, hosts []Host) []HostSnapshot {
	results := make([]HostSnapshot, len(hosts))
	if len(hosts) == 0 {
		return results
	}
	limit := DefaultProbeConcurrency
	if p != nil && p.MaxConcurrency > 0 {
		limit = p.MaxConcurrency
	}
	if limit > len(hosts) {
		limit = len(hosts)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range limit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = p.ProbeHost(ctx, hosts[index])
			}
		}()
	}
	for index := range hosts {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func parseLinuxProbe(data []byte, fallback time.Time) (HostSnapshot, error) {
	snapshot := HostSnapshot{CollectedAt: fallback}
	values := make(map[string]string)
	invalid := make(map[string]bool)
	var problems []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1024), probeOutputLimit)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			problems = append(problems, "malformed probe line")
			continue
		}
		if _, exists := values[key]; exists {
			invalid[key] = true
			problems = append(problems, "duplicate "+key)
			continue
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, "probe output exceeds parser limit")
	}

	if value, ok := validValue(values, invalid, "hostname"); ok && value != "" && !strings.ContainsAny(value, "\r\n\x00") {
		snapshot.Hostname = Available[string]{Value: value, Available: true}
	} else {
		problems = append(problems, "invalid hostname")
	}
	if value, ok := validValue(values, invalid, "timestamp"); ok {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
			snapshot.CollectedAt = time.Unix(seconds, 0).UTC()
		} else {
			problems = append(problems, "invalid timestamp")
		}
	}
	if value, ok := validValue(values, invalid, "uptime_seconds"); ok {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 && seconds <= float64(math.MaxInt64)/float64(time.Second) {
			snapshot.Uptime = Available[time.Duration]{Value: time.Duration(seconds * float64(time.Second)), Available: true}
		} else {
			problems = append(problems, "invalid uptime")
		}
	}
	if value, ok := validValue(values, invalid, "load1"); ok {
		if load, err := strconv.ParseFloat(value, 64); err == nil && load >= 0 && !math.IsInf(load, 0) && !math.IsNaN(load) {
			snapshot.Load1 = Available[float64]{Value: load, Available: true}
		} else {
			problems = append(problems, "invalid load")
		}
	}
	memoryTotal, totalOK := parseKiB(values, invalid, "memory_total_kib")
	memoryAvailable, availableOK := parseKiB(values, invalid, "memory_available_kib")
	if totalOK {
		snapshot.MemoryTotal = Available[uint64]{Value: memoryTotal, Available: true}
	}
	if totalOK && availableOK && memoryAvailable <= memoryTotal {
		snapshot.MemoryUsed = Available[uint64]{Value: memoryTotal - memoryAvailable, Available: true}
	} else if _, present := values["memory_total_kib"]; present || availableOK {
		problems = append(problems, "invalid memory")
	}
	rootTotal, rootTotalOK := parseKiB(values, invalid, "root_total_kib")
	rootUsed, rootUsedOK := parseKiB(values, invalid, "root_used_kib")
	if rootTotalOK {
		snapshot.RootDiskTotal = Available[uint64]{Value: rootTotal, Available: true}
	}
	if rootUsedOK && rootTotalOK && rootUsed <= rootTotal {
		snapshot.RootDiskUsed = Available[uint64]{Value: rootUsed, Available: true}
	} else if _, present := values["root_total_kib"]; present || rootUsedOK {
		problems = append(problems, "invalid root disk")
	}
	if len(problems) != 0 {
		return snapshot, fmt.Errorf("parse host probe: %s", strings.Join(uniqueStrings(problems), "; "))
	}
	return snapshot, nil
}

func validValue(values map[string]string, invalid map[string]bool, key string) (string, bool) {
	value, ok := values[key]
	return value, ok && !invalid[key]
}

func parseKiB(values map[string]string, invalid map[string]bool, key string) (uint64, bool) {
	value, ok := validValue(values, invalid, key)
	if !ok {
		return 0, false
	}
	kib, err := strconv.ParseUint(value, 10, 64)
	if err != nil || kib > math.MaxUint64/1024 {
		return 0, false
	}
	return kib * 1024, true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
