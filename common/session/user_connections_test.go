package session

import (
	"errors"
	"sync/atomic"
	"testing"
)

type testCloser struct {
	closeCount atomic.Int64
}

func (c *testCloser) Close() error {
	c.closeCount.Add(1)
	return nil
}

type errorCloser struct{}

func (c *errorCloser) Close() error {
	return errors.New("close failed")
}

func TestUserConnectionTrackerCloseUserConnections(t *testing.T) {
	tracker := NewUserConnectionTracker()

	userAConn1 := &testCloser{}
	userAConn2 := &testCloser{}
	userBConn := &testCloser{}

	releaseA1 := tracker.Track("user-a", "inbound-a", userAConn1)
	t.Cleanup(releaseA1)
	releaseA2 := tracker.Track("user-a", "inbound-b", userAConn2)
	t.Cleanup(releaseA2)
	releaseB := tracker.Track("user-b", "inbound-a", userBConn)
	t.Cleanup(releaseB)

	result := tracker.CloseUserConnections("user-a", CloseUserConnectionsOptions{AllInbounds: true})
	if result.KilledCount != 2 {
		t.Fatalf("unexpected killed count: got %d want %d", result.KilledCount, 2)
	}
	if len(result.MatchedIdentities) != 1 || result.MatchedIdentities[0] != "user-a" {
		t.Fatalf("unexpected matched identities: %#v", result.MatchedIdentities)
	}
	if userAConn1.closeCount.Load() != 1 {
		t.Fatalf("unexpected close count for user A conn 1: got %d want %d", userAConn1.closeCount.Load(), 1)
	}
	if userAConn2.closeCount.Load() != 1 {
		t.Fatalf("unexpected close count for user A conn 2: got %d want %d", userAConn2.closeCount.Load(), 1)
	}
	if userBConn.closeCount.Load() != 0 {
		t.Fatalf("unexpected close count for user B conn: got %d want %d", userBConn.closeCount.Load(), 0)
	}

	duplicate := tracker.CloseUserConnections("user-a", CloseUserConnectionsOptions{AllInbounds: true})
	if duplicate.KilledCount != 0 {
		t.Fatalf("unexpected duplicate killed count: got %d want 0", duplicate.KilledCount)
	}
	if userAConn1.closeCount.Load() != 1 || userAConn2.closeCount.Load() != 1 {
		t.Fatalf("duplicate close attempted unexpectedly: got %d and %d", userAConn1.closeCount.Load(), userAConn2.closeCount.Load())
	}
}

func TestUserConnectionTrackerCloseScopedAliases(t *testing.T) {
	tracker := NewUserConnectionTracker()

	scopedInboundA := "pgscope:dXNlci1h:aW5ib3VuZC1h"
	scopedInboundB := "pgscope:dXNlci1h:aW5ib3VuZC1i"

	connA := &testCloser{}
	connB := &testCloser{}

	releaseA := tracker.Track(scopedInboundA, "inbound-a", connA)
	t.Cleanup(releaseA)
	releaseB := tracker.Track(scopedInboundB, "inbound-b", connB)
	t.Cleanup(releaseB)

	result := tracker.CloseUserConnections("user-a", CloseUserConnectionsOptions{
		InboundTag:           "inbound-a",
		IncludeScopedAliases: true,
	})
	if result.KilledCount != 1 {
		t.Fatalf("unexpected scoped killed count: got %d want 1", result.KilledCount)
	}
	if connA.closeCount.Load() != 1 {
		t.Fatalf("unexpected close count for inbound A: got %d want %d", connA.closeCount.Load(), 1)
	}
	if connB.closeCount.Load() != 0 {
		t.Fatalf("unexpected close count for inbound B: got %d want %d", connB.closeCount.Load(), 0)
	}
}

func TestUserConnectionTrackerReleasePreventsLeaks(t *testing.T) {
	tracker := NewUserConnectionTracker()

	conn := &errorCloser{}
	release := tracker.Track("user-a", "inbound-a", conn)
	release()

	result := tracker.CloseUserConnections("user-a", CloseUserConnectionsOptions{AllInbounds: true})
	if result.KilledCount != 0 {
		t.Fatalf("unexpected killed count after release: got %d want 0", result.KilledCount)
	}
}
