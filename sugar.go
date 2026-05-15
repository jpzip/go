package jpzip

import (
	"context"
	"sync"
)

// Package-level shortcuts wrap a lazily-initialized default Client. They
// share L1 state but cannot be configured with an L2 cache — use New() to
// get a configurable instance for that.

var (
	defaultOnce   sync.Once
	defaultClient *Client
)

func dflt() *Client {
	defaultOnce.Do(func() { defaultClient = New() })
	return defaultClient
}

// Lookup is a shortcut for New().Lookup.
func Lookup(ctx context.Context, zip string) (*ZipcodeEntry, error) {
	return dflt().Lookup(ctx, zip)
}

// LookupGroup is a shortcut for New().LookupGroup.
func LookupGroup(ctx context.Context, prefix string) (ZipcodeDict, error) {
	return dflt().LookupGroup(ctx, prefix)
}

// LookupAll is a shortcut for New().LookupAll.
func LookupAll(ctx context.Context) (ZipcodeDict, error) {
	return dflt().LookupAll(ctx)
}

// GetMeta is a shortcut for New().GetMeta.
func GetMeta(ctx context.Context) (*Meta, error) {
	return dflt().GetMeta(ctx)
}

// Preload is a shortcut for New().Preload.
func Preload(ctx context.Context, scope string) error {
	return dflt().Preload(ctx, scope)
}
