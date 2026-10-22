package commands

import (
	"testing"
	"time"
)

func TestStatsMonitorClaimAndRelease(t *testing.T) {
	ctr := &Container{}

	if !ctr.StartStatsMonitor() {
		t.Fatal("expected the first claim to succeed")
	}

	// A second claim must be rejected while a stream is already attached,
	// otherwise we'd open a duplicate stream for the same container.
	if ctr.StartStatsMonitor() {
		t.Fatal("expected a second claim to be rejected while a stream is attached")
	}

	if !ctr.StatsMonitorRunning() {
		t.Fatal("expected the monitor to report itself as running")
	}

	ctr.StopStatsMonitor(0)

	if ctr.StatsMonitorRunning() {
		t.Fatal("expected the monitor to be released")
	}

	if !ctr.StartStatsMonitor() {
		t.Fatal("expected an immediate reclaim when no backoff was requested")
	}
}

func TestStatsMonitorBackoffAfterFailure(t *testing.T) {
	ctr := &Container{}

	if !ctr.StartStatsMonitor() {
		t.Fatal("expected the first claim to succeed")
	}

	ctr.StopStatsMonitor(50 * time.Millisecond)

	// This is the behaviour that stopped a container whose stats can't be read
	// (e.g. a stopped one) from being retried once per second forever.
	if ctr.StartStatsMonitor() {
		t.Fatal("expected a claim inside the backoff window to be rejected")
	}

	time.Sleep(60 * time.Millisecond)

	if !ctr.StartStatsMonitor() {
		t.Fatal("expected a claim after the backoff window to succeed")
	}
}
