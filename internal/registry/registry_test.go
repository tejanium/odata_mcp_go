// Copyright (c) 2024 OData MCP Contributors
// SPDX-License-Identifier: MIT

package registry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zmcp/odata-mcp/internal/transport"
)

type fakeBridge struct {
	creds  Credentials
	closed bool
}

func (f *fakeBridge) HandleMessage(_ context.Context, msg *transport.Message) (*transport.Message, error) {
	return msg, nil
}

func (f *fakeBridge) Close() error {
	f.closed = true
	return nil
}

func testCreds(id string) Credentials {
	return Credentials{
		ServiceURL:   "https://svc.example.com/odata/",
		ClientID:     id,
		ClientSecret: "secret-" + id,
		TokenURL:     "https://svc.example.com/token",
		Scope:        "read",
	}
}

func countingFactory(calls *int64) Factory {
	return func(_ context.Context, creds Credentials) (Bridge, error) {
		atomic.AddInt64(calls, 1)
		return &fakeBridge{creds: creds}, nil
	}
}

func TestForBuildsOncePerCredential(t *testing.T) {
	var calls int64
	r := New(countingFactory(&calls))
	ctx := context.Background()

	first, err := r.For(ctx, testCreds("a"))
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}

	second, err := r.For(ctx, testCreds("a"))
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}

	if first != second {
		t.Error("For() returned a different bridge for identical credentials")
	}
	if calls != 1 {
		t.Errorf("factory calls = %d, want 1", calls)
	}
}

func TestForKeepsCredentialsApart(t *testing.T) {
	var calls int64
	r := New(countingFactory(&calls))
	ctx := context.Background()

	first, _ := r.For(ctx, testCreds("a"))
	second, _ := r.For(ctx, testCreds("b"))

	if first == second {
		t.Error("two different credentials shared one bridge, so one caller could use the other's access")
	}
	if calls != 2 {
		t.Errorf("factory calls = %d, want 2", calls)
	}
	if r.Len() != 2 {
		t.Errorf("Len() = %d, want 2", r.Len())
	}
}

func TestForSharesOneBuildAcrossConcurrentCallers(t *testing.T) {
	var calls int64

	factory := func(_ context.Context, creds Credentials) (Bridge, error) {
		atomic.AddInt64(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return &fakeBridge{creds: creds}, nil
	}

	r := New(factory)
	ctx := context.Background()

	var wg sync.WaitGroup
	bridges := make([]Bridge, 25)

	for i := range bridges {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b, err := r.For(ctx, testCreds("a"))
			if err != nil {
				t.Errorf("For() error = %v", err)
				return
			}
			bridges[i] = b
		}(i)
	}
	wg.Wait()

	if calls != 1 {
		t.Errorf("factory calls = %d, want 1 build shared by every caller", calls)
	}
	for i, b := range bridges {
		if b != bridges[0] {
			t.Fatalf("caller %d got a different bridge", i)
		}
	}
}

func TestForDoesNotCacheAFailedBuild(t *testing.T) {
	var calls int64
	wanted := errors.New("metadata fetch returned HTTP 401")

	factory := func(_ context.Context, creds Credentials) (Bridge, error) {
		if atomic.AddInt64(&calls, 1) == 1 {
			return nil, wanted
		}
		return &fakeBridge{creds: creds}, nil
	}

	r := New(factory)
	ctx := context.Background()

	if _, err := r.For(ctx, testCreds("a")); !errors.Is(err, wanted) {
		t.Fatalf("For() error = %v, want %v", err, wanted)
	}
	if r.Len() != 0 {
		t.Errorf("Len() = %d, want the failed entry dropped", r.Len())
	}

	if _, err := r.For(ctx, testCreds("a")); err != nil {
		t.Fatalf("For() after a failure error = %v, want a retry to succeed", err)
	}
	if calls != 2 {
		t.Errorf("factory calls = %d, want the second call to retry", calls)
	}
}

func TestForRejectsIncompleteCredentialsWithoutBuilding(t *testing.T) {
	var calls int64
	r := New(countingFactory(&calls))

	if _, err := r.For(context.Background(), Credentials{ClientID: "a"}); err == nil {
		t.Fatal("For() with no service URL returned no error")
	}
	if calls != 0 {
		t.Errorf("factory calls = %d, want the factory never reached", calls)
	}
}

func TestForEvictsAfterTheTTL(t *testing.T) {
	var calls int64
	now := time.Now()
	clock := func() time.Time { return now }

	r := New(countingFactory(&calls), WithTTL(time.Minute), WithClock(clock))
	ctx := context.Background()

	if _, err := r.For(ctx, testCreds("a")); err != nil {
		t.Fatalf("For() error = %v", err)
	}

	now = now.Add(2 * time.Minute)

	if _, err := r.For(ctx, testCreds("b")); err != nil {
		t.Fatalf("For() error = %v", err)
	}

	if r.Len() != 1 {
		t.Errorf("Len() = %d, want the expired entry dropped", r.Len())
	}

	if _, err := r.For(ctx, testCreds("a")); err != nil {
		t.Fatalf("For() error = %v", err)
	}
	if calls != 3 {
		t.Errorf("factory calls = %d, want the expired credential rebuilt", calls)
	}
}

func TestForEvictsTheLeastRecentlyUsedWhenFull(t *testing.T) {
	var calls int64
	now := time.Now()
	clock := func() time.Time { return now }

	r := New(countingFactory(&calls), WithMaxEntries(2), WithTTL(time.Hour), WithClock(clock))
	ctx := context.Background()

	oldest, _ := r.For(ctx, testCreds("a"))
	now = now.Add(time.Second)
	if _, err := r.For(ctx, testCreds("b")); err != nil {
		t.Fatalf("For() error = %v", err)
	}

	now = now.Add(time.Second)
	if _, err := r.For(ctx, testCreds("c")); err != nil {
		t.Fatalf("For() error = %v", err)
	}

	if r.Len() != 2 {
		t.Errorf("Len() = %d, want the cache capped at 2", r.Len())
	}
	if closeable, ok := oldest.(*fakeBridge); !ok || !closeable.closed {
		t.Error("the evicted bridge was not closed")
	}
}

func TestForReportsNoCapacityWhileEveryEntryIsBuilding(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	factory := func(_ context.Context, creds Credentials) (Bridge, error) {
		close(started)
		<-release
		return &fakeBridge{creds: creds}, nil
	}

	r := New(factory, WithMaxEntries(1))
	ctx := context.Background()

	go func() { _, _ = r.For(ctx, testCreds("a")) }()
	<-started

	_, err := r.For(ctx, testCreds("b"))
	close(release)

	if !errors.Is(err, ErrNoCapacity) {
		t.Errorf("For() error = %v, want ErrNoCapacity", err)
	}
}

func TestEvictClosesTheBridge(t *testing.T) {
	var calls int64
	r := New(countingFactory(&calls))
	ctx := context.Background()

	b, _ := r.For(ctx, testCreds("a"))
	r.Evict(testCreds("a"))

	if r.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after Evict", r.Len())
	}
	if closeable, ok := b.(*fakeBridge); !ok || !closeable.closed {
		t.Error("Evict() left the bridge open")
	}
}

func TestKeySeparatesFieldsUnambiguously(t *testing.T) {
	base := Credentials{ServiceURL: "https://svc.example.com/", TokenURL: "https://svc.example.com/token"}

	shifted := base
	shifted.ClientID, shifted.ClientSecret = "ab", "c"

	other := base
	other.ClientID, other.ClientSecret = "a", "bc"

	if shifted.Key() == other.Key() {
		t.Error("Key() collided across a field boundary")
	}

	same := shifted
	if shifted.Key() != same.Key() {
		t.Error("Key() is not stable for identical credentials")
	}

	scoped := shifted
	scoped.Scope = "write"
	if shifted.Key() == scoped.Key() {
		t.Error("Key() ignored the scope, so a narrower caller would reuse a wider bridge")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		creds   Credentials
		wantErr bool
	}{
		{"client credentials", testCreds("a"), false},
		{"bearer token only", Credentials{ServiceURL: "https://svc.example.com/", BearerToken: "t"}, false},
		{"no service url", Credentials{BearerToken: "t"}, true},
		{"no credential at all", Credentials{ServiceURL: "https://svc.example.com/"}, true},
		{"client id without secret", Credentials{ServiceURL: "https://svc.example.com/", ClientID: "a", TokenURL: "https://t/"}, true},
		{"client credentials without a token url", Credentials{ServiceURL: "https://svc.example.com/", ClientID: "a", ClientSecret: "b"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.creds.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestRedactedHidesSecrets(t *testing.T) {
	redacted := Credentials{
		ServiceURL:   "https://svc.example.com/",
		ClientID:     "public-id",
		ClientSecret: "very-secret",
		BearerToken:  "also-secret",
	}.Redacted()

	if redacted.ClientSecret == "very-secret" || redacted.BearerToken == "also-secret" {
		t.Errorf("Redacted() kept a secret: %+v", redacted)
	}
	if redacted.ClientID != "public-id" {
		t.Errorf("ClientID = %q, want it left readable", redacted.ClientID)
	}
}
