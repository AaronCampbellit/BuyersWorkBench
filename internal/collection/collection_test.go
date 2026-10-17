package collection

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"buyersworkbench/internal/domain"
)

func request() Request {
	return Request{
		Binding: domain.Binding{
			RunID:        "30000000-0000-4000-8000-000000000001",
			ConnectionID: "20000000-0000-4000-8000-000000000001",
			WorkspaceID:  "10000000-0000-4000-8000-000000000001",
			Generation:   1,
			Connector:    domain.Connector{Key: "fictional_supplier", Version: domain.SeedVersion},
			Origin:       "fictional",
			Scope:        domain.FictionalScope(),
		},
		Records:    domain.SeedRecords(),
		ObservedAt: time.Date(2026, 10, 19, 8, 30, 0, 0, time.FixedZone("source", -7*60*60)),
	}
}

type testCollector struct {
	descriptor Descriptor
	collect    func(context.Context, Request) (domain.Batch, error)
	calls      int
}

func (c *testCollector) Describe() Descriptor { return c.descriptor }
func (c *testCollector) Collect(ctx context.Context, req Request) (domain.Batch, error) {
	c.calls++
	if c.collect != nil {
		return c.collect(ctx, req)
	}
	return domain.FictionalBatch(req.Records, req.Binding, req.ObservedAt), nil
}

func TestFictionalCollectionCompatibility(t *testing.T) {
	r := DefaultRegistry()
	for _, scenario := range []string{"baseline", "delay", "partial", "confirm", "repeat", "recovery"} {
		t.Run(scenario, func(t *testing.T) {
			req := request()
			var err error
			req.Records, err = domain.ApplyScenario(req.Records, scenario)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := r.Collect(context.Background(), "fictional_supplier", req)
			if err != nil {
				t.Fatal(err)
			}
			if err := domain.ValidateBatch(batch, req.Binding); err != nil {
				t.Fatalf("scenario no longer satisfies publication contract: %v", err)
			}
			want := domain.FictionalBatch(req.Records, req.Binding, req.ObservedAt)
			if !reflect.DeepEqual(batch, want) {
				t.Fatal("adapter changed the existing fictional observation contract")
			}
			if batch.ObservedAt.Location() != time.UTC || !batch.ObservedAt.Equal(req.ObservedAt) {
				t.Fatal("collection did not preserve the caller's actual observation instant in UTC")
			}
			batch.Records[0].Description = "changed output"
			if req.Records[0].Description == "changed output" {
				t.Fatal("output aliased trusted source records")
			}
		})
	}
}

func TestFictionalIncompleteAndMissingClockNeverReturnBatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Request)
		code   Code
	}{
		{"incomplete", func(r *Request) { r.Incomplete = true }, CodeIncomplete},
		{"missing_clock", func(r *Request) { r.ObservedAt = time.Time{} }, CodeSchema},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := request()
			tc.mutate(&req)
			batch, err := DefaultRegistry().Collect(context.Background(), "fictional_supplier", req)
			if FailureCode(err) != tc.code || Retryable(err) || !reflect.DeepEqual(batch, domain.Batch{}) {
				t.Fatalf("unexpected failure: batch=%+v, error=%v", batch, err)
			}
		})
	}
}

func TestDefaultRegistryDoesNotEnableRealCollection(t *testing.T) {
	r := DefaultRegistry()
	d, err := r.Describe("fictional_supplier")
	if err != nil || d.Method != MethodFictional || d.Mode != ModeFictional || d.Auth != AuthNone {
		t.Fatalf("wrong built-in descriptor: %+v, %v", d, err)
	}
	for _, key := range []string{"lenovo", "webhook", "api_polling", "browser", "public_scrape", "email", "", "fictional_supplier/alternate"} {
		batch, err := r.Collect(context.Background(), key, request())
		if FailureCode(err) != CodeUnsupported || Retryable(err) || !reflect.DeepEqual(batch, domain.Batch{}) {
			t.Fatalf("unregistered connector %q did not fail closed: %v", key, err)
		}
	}
	var absent *Registry
	if _, err := absent.Describe("fictional_supplier"); FailureCode(err) != CodeUnsupported {
		t.Fatal("nil registry unexpectedly enabled collection")
	}
}

func TestRegistryModelsRealMethodsOnlyWhenExplicitlyRegistered(t *testing.T) {
	for _, method := range []Method{MethodWebhook, MethodAPIPolling, MethodBrowser, MethodPublicScrape, MethodEmail} {
		t.Run(string(method), func(t *testing.T) {
			d := Descriptor{Key: "explicit_supplier", Version: "1.0", Method: method, Auth: AuthNone, Mode: ModeLivePrivate}
			r, err := NewRegistry(&testCollector{descriptor: d})
			if err != nil {
				t.Fatalf("method not represented: %v", err)
			}
			got, err := r.Describe(d.Key)
			if err != nil || got != d {
				t.Fatalf("wrong descriptor: %+v, %v", got, err)
			}
		})
	}
}

func TestRegistryRejectsInvalidAndDuplicateDescriptors(t *testing.T) {
	base := (Fictional{}).Describe()
	for _, tc := range []struct {
		name   string
		mutate func(*Descriptor)
	}{
		{"empty_key", func(d *Descriptor) { d.Key = "" }},
		{"unsafe_key", func(d *Descriptor) { d.Key = "supplier\nsecret" }},
		{"empty_version", func(d *Descriptor) { d.Version = "" }},
		{"unknown_method", func(d *Descriptor) { d.Method = "future" }},
		{"unknown_authentication", func(d *Descriptor) { d.Auth = "inline_password" }},
		{"unknown_mode", func(d *Descriptor) { d.Mode = "public" }},
		{"fictional_as_live", func(d *Descriptor) { d.Mode = ModeLivePrivate }},
		{"live_as_fictional", func(d *Descriptor) { d.Method = MethodBrowser }},
		{"fictional_credentials", func(d *Descriptor) { d.Auth = AuthCredentialReference }},
		{"authenticated_public_scrape", func(d *Descriptor) {
			d.Method, d.Mode, d.Auth = MethodPublicScrape, ModeLivePrivate, AuthSessionReference
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := base
			tc.mutate(&d)
			if _, err := NewRegistry(&testCollector{descriptor: d}); err == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
	if _, err := NewRegistry(nil); err == nil {
		t.Fatal("nil adapter accepted")
	}
	otherVersion := base
	otherVersion.Version = "2.0"
	if _, err := NewRegistry(Fictional{}, &testCollector{descriptor: otherVersion}); err == nil {
		t.Fatal("duplicate key accepted with a different version")
	}
}

func TestRegistrySnapshotsIdentityAndRejectsUntrustedBinding(t *testing.T) {
	c := &testCollector{descriptor: (Fictional{}).Describe()}
	r, err := NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	c.descriptor.Key, c.descriptor.Version, c.descriptor.Mode = "changed", "2.0", ModeLivePrivate
	got, err := r.Describe("fictional_supplier")
	if err != nil || got != (Fictional{}).Describe() {
		t.Fatal("adapter changed its registered authority after construction")
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Binding.Connector.Key = "changed" },
		func(r *Request) { r.Binding.Connector.Version = "2.0" },
		func(r *Request) { r.Binding.Origin = "live_private" },
	} {
		req := request()
		mutate(&req)
		if _, err := r.Collect(context.Background(), "fictional_supplier", req); FailureCode(err) != CodeSchema {
			t.Fatalf("untrusted binding accepted: %v", err)
		}
	}
	if c.calls != 0 {
		t.Fatal("adapter called with an invalid trusted identity")
	}
}

func TestRegistryRejectsAdapterIdentityOverrides(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domain.Batch)
	}{
		{"run", func(b *domain.Batch) { b.RunID = "other" }},
		{"connection", func(b *domain.Batch) { b.ConnectionID = "other" }},
		{"workspace", func(b *domain.Batch) { b.WorkspaceID = "other" }},
		{"generation", func(b *domain.Batch) { b.Generation++ }},
		{"connector_key", func(b *domain.Batch) { b.Connector.Key = "other" }},
		{"connector_version", func(b *domain.Batch) { b.Connector.Version = "2.0" }},
		{"origin", func(b *domain.Batch) { b.Origin = "live_private" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &testCollector{descriptor: (Fictional{}).Describe(), collect: func(_ context.Context, req Request) (domain.Batch, error) {
				batch := domain.FictionalBatch(req.Records, req.Binding, req.ObservedAt)
				tc.mutate(&batch)
				return batch, nil
			}}
			r, err := NewRegistry(c)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := r.Collect(context.Background(), "fictional_supplier", request())
			if FailureCode(err) != CodeSchema || !reflect.DeepEqual(batch, domain.Batch{}) {
				t.Fatalf("adapter identity override accepted: %v", err)
			}
		})
	}
}

func TestRegistryProtectsCallerOwnedSourceAndScope(t *testing.T) {
	req := request()
	n := len(req.Records)
	req.Binding.Scope.ExpectedRecords = &n
	before := domain.CloneRecords(req.Records)
	statuses := append([]string(nil), req.Binding.Scope.Statuses...)
	c := &testCollector{descriptor: (Fictional{}).Describe(), collect: func(_ context.Context, req Request) (domain.Batch, error) {
		req.Binding.Scope.Statuses[0] = "cancelled"
		*req.Binding.Scope.ExpectedRecords = 9000
		req.Records[0].Description = "mutated"
		if req.Records[0].PlacedAt != nil {
			*req.Records[0].PlacedAt = time.Time{}
		}
		return domain.Batch{}, NewFailure(CodeSchema)
	}}
	r, err := NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.Collect(context.Background(), "fictional_supplier", req)
	if !reflect.DeepEqual(req.Records, before) || !reflect.DeepEqual(req.Binding.Scope.Statuses, statuses) || *req.Binding.Scope.ExpectedRecords != len(req.Records) {
		t.Fatal("adapter mutated trusted caller state")
	}
}

func TestSafeFailureClassificationAndRetryPolicy(t *testing.T) {
	for _, code := range []Code{CodeAuthentication, CodeChallenge, CodePermission, CodeSchema, CodeIncomplete, CodeNetwork, CodeRateLimit, CodeUnavailable, CodeUnsupported, CodeCancelled} {
		t.Run(string(code), func(t *testing.T) {
			private := "supplier response with password and session cookie"
			c := &testCollector{descriptor: (Fictional{}).Describe(), collect: func(context.Context, Request) (domain.Batch, error) {
				return domain.Batch{}, fmt.Errorf("%s: %w", private, NewFailure(code))
			}}
			r, err := NewRegistry(c)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Collect(context.Background(), "fictional_supplier", request())
			wantRetry := code == CodeNetwork || code == CodeRateLimit || code == CodeUnavailable
			if FailureCode(err) != code || Retryable(err) != wantRetry || strings.Contains(err.Error(), private) || errors.Unwrap(err) != nil {
				t.Fatalf("unsafe or incorrect failure classification: %v", err)
			}
		})
	}
	c := &testCollector{descriptor: (Fictional{}).Describe(), collect: func(context.Context, Request) (domain.Batch, error) {
		return domain.Batch{}, errors.New("raw supplier secret")
	}}
	r, err := NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Collect(context.Background(), "fictional_supplier", request())
	if FailureCode(err) != CodeUnavailable || err.Error() != "collection: source_unavailable" || !Retryable(err) {
		t.Fatalf("raw failure leaked or was misclassified: %v", err)
	}
	if FailureCode(nil) != "" || Retryable(nil) || NewFailure("raw secret").Error() != "collection: source_schema_invalid" {
		t.Fatal("invalid failure codes were not normalized")
	}
}

func TestCancelledCollectionDoesNotCallAdapter(t *testing.T) {
	c := &testCollector{descriptor: (Fictional{}).Describe()}
	r, err := NewRegistry(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Collect(ctx, "fictional_supplier", request())
	if FailureCode(err) != CodeCancelled || Retryable(err) || c.calls != 0 {
		t.Fatalf("cancelled request reached adapter: %v", err)
	}
	if FailureCode(context.DeadlineExceeded) != CodeNetwork || !Retryable(context.DeadlineExceeded) {
		t.Fatal("deadline did not classify as a retryable network failure")
	}
}
