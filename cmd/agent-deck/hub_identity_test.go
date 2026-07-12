package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/update"
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

func TestHubCompatibilityVersionFallsBackToBakedInVersion(t *testing.T) {
	oldVersion := Version
	Version = "1.10.9"
	t.Cleanup(func() { Version = oldVersion })
	withBuildIdentity(t, "hub", "dev", "")

	if got := compatibleAgentDeckVersion(); got != "1.10.9" {
		t.Fatalf("compatibleAgentDeckVersion() = %q, want baked-in version", got)
	}
}

func TestRemoteReleaseUsesHubCompatibleVersion(t *testing.T) {
	withBuildIdentity(t, "hub", "0.1.0", "1.10.9")
	oldFetch := fetchReleaseByTagForRemote
	t.Cleanup(func() { fetchReleaseByTagForRemote = oldFetch })

	wantErr := errors.New("stop after selection")
	var requestedTag string
	fetchReleaseByTagForRemote = func(tag string) (*update.Release, error) {
		requestedTag = tag
		return nil, wantErr
	}

	_, err := fetchCompatibleRemoteRelease()
	if !errors.Is(err, wantErr) {
		t.Fatalf("fetchReleaseForRemote() error = %v, want %v", err, wantErr)
	}
	if requestedTag != "1.10.9" {
		t.Fatalf("requested release = %q, want compatible release 1.10.9", requestedTag)
	}
}

func TestRemoteReleaseKeepsLatestSelectionForUpstreamBuild(t *testing.T) {
	withBuildIdentity(t, "upstream", "dev", "1.10.9")
	oldFetch := fetchLatestReleaseForRemote
	t.Cleanup(func() { fetchLatestReleaseForRemote = oldFetch })

	want := &update.Release{TagName: "v1.11.0"}
	fetchLatestReleaseForRemote = func() (*update.Release, error) { return want, nil }

	got, err := fetchCompatibleRemoteRelease()
	if err != nil {
		t.Fatalf("fetchCompatibleRemoteRelease() error = %v", err)
	}
	if got != want {
		t.Fatalf("fetchCompatibleRemoteRelease() = %#v, want latest release", got)
	}
}
