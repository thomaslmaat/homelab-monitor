package httpapi

import (
	"encoding/json"
	"github.com/thomaslmaat/homelab-monitor/internal/model"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHostViewsHeartbeat(t *testing.T) {
	now := time.Now()
	rows := []model.HostMetrics{{Hostname: "fresh", CollectedAt: now.Add(-10 * time.Second)}, {Hostname: "stale", CollectedAt: now.Add(-2 * time.Minute)}}
	views := hostViews(rows)
	if len(views) != 2 {
		t.Fatalf("got %d views", len(views))
	}
	if !views[0].Online {
		t.Fatal("fresh host should be online")
	}
	if views[1].Online {
		t.Fatal("stale host should be offline")
	}
}
func TestHealthEndpoint(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/healthz", nil))
	if rr.Code != 200 {
		t.Fatalf("status=%d", rr.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Fatalf("unexpected health response: %v", got)
	}
}
