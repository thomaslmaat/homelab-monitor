package integrations

import (
	"context"
	"testing"
)

type testAdapter struct { id string }
func (a testAdapter) ID() string { return a.id }
func (testAdapter) Name() string { return "Test" }
func (testAdapter) Kind() string { return "test" }
func (testAdapter) Version() string { return "1" }
func (testAdapter) Capabilities() []Capability { return []Capability{{ID:"resources.read"}} }
func (testAdapter) Check(context.Context) Health { return Health{Status:"connected"} }

func TestRegisterAndList(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(testAdapter{id:"test"}); err != nil { t.Fatal(err) }
	got := r.List(context.Background())
	if len(got) != 1 || got[0].ID != "test" || got[0].Health.Status != "connected" { t.Fatalf("unexpected snapshots: %#v", got) }
}
func TestRejectDuplicateAndEmptyID(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(testAdapter{id:"test"}); err != nil { t.Fatal(err) }
	if err := r.Register(testAdapter{id:"test"}); err == nil { t.Fatal("expected duplicate ID error") }
	if err := r.Register(testAdapter{id:" "}); err == nil { t.Fatal("expected empty ID error") }
}


type countingAdapter struct {
	testAdapter
	calls *int
}

func (a countingAdapter) Check(context.Context) Health {
	*a.calls++
	return Health{Status: "connected"}
}

func TestListCachesHealthChecks(t *testing.T) {
	r := NewRegistry()
	calls := 0
	if err := r.Register(countingAdapter{testAdapter: testAdapter{id: "cached"}, calls: &calls}); err != nil {
		t.Fatal(err)
	}
	r.List(context.Background())
	r.List(context.Background())
	if calls != 1 {
		t.Fatalf("expected one health check within cache window, got %d", calls)
	}
}
