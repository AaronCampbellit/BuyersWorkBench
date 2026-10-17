// Package collection defines source adapters independently of persistence,
// scheduling, and the purchasing interface. Only the fictional adapter is
// installed by default; declaring a collection method does not enable it.
package collection

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"time"

	"buyersworkbench/internal/domain"
)

type Method string

const (
	MethodWebhook      Method = "webhook"
	MethodAPIPolling   Method = "api_polling"
	MethodBrowser      Method = "browser"
	MethodPublicScrape Method = "public_scrape"
	MethodEmail        Method = "email"
	MethodFictional    Method = "fictional"
)

// Authentication describes the server-owned reference an eventual adapter
// requires. Descriptors and requests never contain a password or browser state.
type Authentication string

const (
	AuthNone                Authentication = "none"
	AuthCredentialReference Authentication = "credential_reference"
	AuthSessionReference    Authentication = "session_reference"
	AuthSignatureReference  Authentication = "signature_reference"
)

type Mode string

const (
	ModeFictional   Mode = "fictional"
	ModeLivePrivate Mode = "live_private"
)

// Descriptor is fixed when an adapter enters a Registry. Source observations
// cannot choose its identity, authentication requirements, or origin mode.
type Descriptor struct {
	Key     string         `json:"key"`
	Version string         `json:"version"`
	Method  Method         `json:"method"`
	Auth    Authentication `json:"auth"`
	Mode    Mode           `json:"mode"`
}

// Request is constructed from trusted connection/run state. Records and
// Incomplete are the server-owned fictional source, not an ingress contract for
// remote suppliers. ObservedAt is supplied by the caller's clock.
type Request struct {
	Binding    domain.Binding
	Records    []domain.Record
	Incomplete bool
	ObservedAt time.Time
}

type Collector interface {
	Describe() Descriptor
	Collect(context.Context, Request) (domain.Batch, error)
}

type registered struct {
	descriptor Descriptor
	collector  Collector
}

// Registry is immutable after construction and safe for concurrent reads.
// Collectors must independently support concurrent calls.
type Registry struct {
	entries map[string]registered
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validDescriptor(d Descriptor) bool {
	if !keyPattern.MatchString(d.Key) || !versionPattern.MatchString(d.Version) ||
		!slices.Contains([]Method{MethodWebhook, MethodAPIPolling, MethodBrowser, MethodPublicScrape, MethodEmail, MethodFictional}, d.Method) ||
		!slices.Contains([]Authentication{AuthNone, AuthCredentialReference, AuthSessionReference, AuthSignatureReference}, d.Auth) ||
		!slices.Contains([]Mode{ModeFictional, ModeLivePrivate}, d.Mode) {
		return false
	}
	if d.Method == MethodFictional || d.Mode == ModeFictional {
		return d.Method == MethodFictional && d.Mode == ModeFictional && d.Auth == AuthNone
	}
	return d.Method != MethodPublicScrape || d.Auth == AuthNone
}

func NewRegistry(collectors ...Collector) (*Registry, error) {
	r := &Registry{entries: make(map[string]registered, len(collectors))}
	for _, c := range collectors {
		if c == nil {
			return nil, errors.New("collection adapter is required")
		}
		d := c.Describe()
		if !validDescriptor(d) {
			return nil, errors.New("invalid collection descriptor")
		}
		if _, exists := r.entries[d.Key]; exists {
			return nil, errors.New("duplicate collection adapter key")
		}
		r.entries[d.Key] = registered{descriptor: d, collector: c}
	}
	return r, nil
}

// DefaultRegistry enables no supplier network access.
func DefaultRegistry() *Registry {
	r, err := NewRegistry(Fictional{})
	if err != nil {
		panic("invalid built-in collection descriptor")
	}
	return r
}

func (r *Registry) Describe(key string) (Descriptor, error) {
	if r != nil {
		if entry, ok := r.entries[key]; ok {
			return entry.descriptor, nil
		}
	}
	return Descriptor{}, NewFailure(CodeUnsupported)
}

// Collect enforces the server-issued identity before and after calling an
// adapter. Publication must still call domain.ValidateBatch for bounded schema
// and completeness checks and recheck its durable authorization/lease fence.
func (r *Registry) Collect(ctx context.Context, key string, req Request) (domain.Batch, error) {
	d, err := r.Describe(key)
	if err != nil {
		return domain.Batch{}, err
	}
	if req.Binding.Connector != (domain.Connector{Key: d.Key, Version: d.Version}) || req.Binding.Origin != string(d.Mode) {
		return domain.Batch{}, NewFailure(CodeSchema)
	}
	if err := ctx.Err(); err != nil {
		return domain.Batch{}, safeFailure(err)
	}
	// Protect the caller's trusted scope and source facts from adapter mutation.
	bound := req.Binding
	req.Binding.Scope.Statuses = slices.Clone(bound.Scope.Statuses)
	req.Binding.Scope.Warnings = slices.Clone(bound.Scope.Warnings)
	if bound.Scope.ExpectedRecords != nil {
		n := *bound.Scope.ExpectedRecords
		req.Binding.Scope.ExpectedRecords = &n
	}
	req.Records = domain.CloneRecords(req.Records)
	batch, err := r.entries[key].collector.Collect(ctx, req)
	if err != nil {
		return domain.Batch{}, safeFailure(err)
	}
	if batch.RunID != bound.RunID || batch.ConnectionID != bound.ConnectionID || batch.WorkspaceID != bound.WorkspaceID ||
		batch.Generation != bound.Generation || batch.Connector != bound.Connector || batch.Origin != bound.Origin {
		return domain.Batch{}, NewFailure(CodeSchema)
	}
	return batch, nil
}
