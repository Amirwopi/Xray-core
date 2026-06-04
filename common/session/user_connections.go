package session

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"sync/atomic"
)

const scopedUserIdentityPrefix = "pgscope:"

type CloseUserConnectionsOptions struct {
	InboundTag           string
	IncludeScopedAliases bool
	AllInbounds          bool
}

type CloseUserConnectionsResult struct {
	KilledCount       int
	MatchedIdentities []string
}

type trackedUserConnection struct {
	id         uint64
	identity   string
	inboundTag string
	closer     io.Closer
}

type UserConnectionTracker struct {
	nextID atomic.Uint64

	mu         sync.Mutex
	byIdentity map[string]map[uint64]*trackedUserConnection
}

func NewUserConnectionTracker() *UserConnectionTracker {
	return &UserConnectionTracker{
		byIdentity: make(map[string]map[uint64]*trackedUserConnection),
	}
}

var defaultUserConnectionTracker = NewUserConnectionTracker()

func DefaultUserConnectionTracker() *UserConnectionTracker {
	return defaultUserConnectionTracker
}

func TrackUserConnection(ctx context.Context, closer io.Closer) func() {
	if closer == nil {
		return func() {}
	}

	inbound := InboundFromContext(ctx)
	if inbound == nil || inbound.User == nil || inbound.User.Email == "" {
		return func() {}
	}

	return DefaultUserConnectionTracker().Track(inbound.User.Email, inbound.Tag, closer)
}

func (t *UserConnectionTracker) Track(identity, inboundTag string, closer io.Closer) func() {
	if t == nil || closer == nil || identity == "" {
		return func() {}
	}

	tracked := &trackedUserConnection{
		id:         t.nextID.Add(1),
		identity:   identity,
		inboundTag: inboundTag,
		closer:     closer,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	identityConnections := t.byIdentity[identity]
	if identityConnections == nil {
		identityConnections = make(map[uint64]*trackedUserConnection)
		t.byIdentity[identity] = identityConnections
	}
	identityConnections[tracked.id] = tracked

	return func() {
		t.removeTrackedConnection(tracked)
	}
}

func (t *UserConnectionTracker) CloseUserConnections(identity string, opts CloseUserConnectionsOptions) CloseUserConnectionsResult {
	if t == nil || identity == "" {
		return CloseUserConnectionsResult{}
	}

	targets, matchedIdentities := t.collectTargets(identity, opts)
	for _, target := range targets {
		_ = target.closer.Close()
	}

	return CloseUserConnectionsResult{
		KilledCount:       len(targets),
		MatchedIdentities: matchedIdentities,
	}
}

func (t *UserConnectionTracker) collectTargets(identity string, opts CloseUserConnectionsOptions) ([]*trackedUserConnection, []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	targetsByID := make(map[uint64]*trackedUserConnection)
	matchedIdentitySet := make(map[string]struct{})

	addTarget := func(tracked *trackedUserConnection) {
		if tracked == nil {
			return
		}
		if !matchesInboundScope(tracked.inboundTag, opts) {
			return
		}
		targetsByID[tracked.id] = tracked
		matchedIdentitySet[tracked.identity] = struct{}{}
	}

	if identityConnections := t.byIdentity[identity]; identityConnections != nil {
		for _, tracked := range identityConnections {
			addTarget(tracked)
		}
	}

	if opts.IncludeScopedAliases {
		for rawIdentity, identityConnections := range t.byIdentity {
			logicalIdentity, _, ok := parseScopedUserIdentity(rawIdentity)
			if !ok || logicalIdentity != identity {
				continue
			}
			for _, tracked := range identityConnections {
				addTarget(tracked)
			}
		}
	}

	targets := make([]*trackedUserConnection, 0, len(targetsByID))
	for _, tracked := range targetsByID {
		targets = append(targets, tracked)
		t.removeTrackedConnectionLocked(tracked)
	}

	matchedIdentities := make([]string, 0, len(matchedIdentitySet))
	for matchedIdentity := range matchedIdentitySet {
		matchedIdentities = append(matchedIdentities, matchedIdentity)
	}

	return targets, matchedIdentities
}

func matchesInboundScope(trackedInboundTag string, opts CloseUserConnectionsOptions) bool {
	if opts.AllInbounds || opts.InboundTag == "" {
		return true
	}
	return trackedInboundTag == opts.InboundTag
}

func (t *UserConnectionTracker) removeTrackedConnection(tracked *trackedUserConnection) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removeTrackedConnectionLocked(tracked)
}

func (t *UserConnectionTracker) removeTrackedConnectionLocked(tracked *trackedUserConnection) {
	if tracked == nil {
		return
	}

	identityConnections := t.byIdentity[tracked.identity]
	if identityConnections == nil {
		return
	}

	delete(identityConnections, tracked.id)
	if len(identityConnections) == 0 {
		delete(t.byIdentity, tracked.identity)
	}
}

func parseScopedUserIdentity(raw string) (identity string, inboundTag string, ok bool) {
	if !strings.HasPrefix(raw, scopedUserIdentityPrefix) {
		return "", "", false
	}

	encoded := strings.TrimPrefix(raw, scopedUserIdentityPrefix)
	parts := strings.SplitN(encoded, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}

	identityBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", false
	}
	inboundTagBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}

	return string(identityBytes), string(inboundTagBytes), true
}
