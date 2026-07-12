package main

import (
	"bytes"
	"strings"
	"testing"
)

func withBuildIdentity(t *testing.T, flavor, hub, upstream string) {
	t.Helper()
	oldFlavor, oldHub, oldUpstream := BuildFlavor, HubVersion, UpstreamVersion
	BuildFlavor, HubVersion, UpstreamVersion = flavor, hub, upstream
	t.Cleanup(func() {
		BuildFlavor, HubVersion, UpstreamVersion = oldFlavor, oldHub, oldUpstream
	})
}

func TestHubVersionOutputShowsBothVersions(t *testing.T) {
	withBuildIdentity(t, "hub", "0.1.0", "1.10.9")
	var buf bytes.Buffer
	writeVersionOutput(&buf, Version)
	want := "Agent Deck Hub v0.1.0 (upstream-compatible Agent Deck v1.10.9)\n"
	if got := buf.String(); got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestHubBuildRefusesLocalSelfUpdate(t *testing.T) {
	withBuildIdentity(t, "hub", "dev", "1.10.9")
	err := localSelfUpdateError()
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("localSelfUpdateError() = %v, want disabled error", err)
	}
}

func TestUpstreamBuildKeepsLocalSelfUpdate(t *testing.T) {
	withBuildIdentity(t, "upstream", "dev", "1.10.9")
	if err := localSelfUpdateError(); err != nil {
		t.Fatalf("upstream localSelfUpdateError() = %v", err)
	}
}

func TestHubCompatibilityVersionUsesUpstreamVersion(t *testing.T) {
	withBuildIdentity(t, "hub", "0.1.0", "1.10.9")
	if got := compatibleAgentDeckVersion(); got != "1.10.9" {
		t.Fatalf("compatibleAgentDeckVersion() = %q", got)
	}
}
