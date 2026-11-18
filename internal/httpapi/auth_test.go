package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thomaslmaat/homelab-monitor/internal/storage"
)

func TestDashboardRequiresLoginAndForcesInitialPasswordChange(t *testing.T) {
	store, err := storage.Open(t.TempDir() + "/auth-test.db")
	if err != nil { t.Fatal(err) }
	defer store.Close()
	s := New(store, nil)
	handler := s.Handler()

	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if unauth.Code != http.StatusUnauthorized { t.Fatalf("unauthenticated API status = %d, want 401", unauth.Code) }

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"admin"}`)))
	if login.Code != http.StatusOK { t.Fatalf("default login status = %d, body=%s", login.Code, login.Body.String()) }
	cookies := login.Result().Cookies()
	if len(cookies)==0 { t.Fatal("login did not set a session cookie") }
	cookie := cookies[0]

	status := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil); req.AddCookie(cookie)
	handler.ServeHTTP(status, req)
	var state map[string]any
	if err:=json.Unmarshal(status.Body.Bytes(),&state);err!=nil{t.Fatal(err)}
	if state["must_change_password"] != true { t.Fatalf("first login must require password change: %v",state) }

	change := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(`{"current_password":"admin","new_password":"A-secure-test-password-2026"}`)); req.AddCookie(cookie)
	handler.ServeHTTP(change,req)
	if change.Code != http.StatusOK { t.Fatalf("password change status = %d, body=%s",change.Code,change.Body.String()) }

	status = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/status", nil); req.AddCookie(cookie)
	handler.ServeHTTP(status,req)
	state=map[string]any{}
	if err:=json.Unmarshal(status.Body.Bytes(),&state);err!=nil{t.Fatal(err)}
	if state["must_change_password"] != false { t.Fatalf("password change flag should be cleared: %v",state) }

	allowed:=httptest.NewRecorder()
	req=httptest.NewRequest(http.MethodGet,"/api/v1/overview",nil);req.AddCookie(cookie)
	handler.ServeHTTP(allowed,req)
	if allowed.Code==http.StatusUnauthorized { t.Fatal("authenticated dashboard API request was rejected") }
}
