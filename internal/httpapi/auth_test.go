package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if state["default_password_available"] != true { t.Fatalf("default password notice should be available before setup: %v",state) }

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

	publicStatus:=httptest.NewRecorder()
	handler.ServeHTTP(publicStatus,httptest.NewRequest(http.MethodGet,"/api/v1/auth/status",nil))
	var publicState map[string]any
	if err:=json.Unmarshal(publicStatus.Body.Bytes(),&publicState);err!=nil{t.Fatal(err)}
	if publicState["default_password_available"] != false { t.Fatalf("default password notice should disappear after setup: %v",publicState) }

	allowed:=httptest.NewRecorder()
	req=httptest.NewRequest(http.MethodGet,"/api/v1/overview",nil);req.AddCookie(cookie)
	handler.ServeHTTP(allowed,req)
	if allowed.Code==http.StatusUnauthorized { t.Fatal("authenticated dashboard API request was rejected") }
}

func TestRoleAccess(t *testing.T) {
	tests := []struct {
		role string
		monitor, servers, admin bool
	}{
		{"admin",true,true,true},
		{"monitor",true,false,false},
		{"servers",false,true,false},
		{"both",true,true,false},
		{"unknown",false,false,false},
	}
	for _,tt := range tests {
		monitor,servers,admin:=roleAccess(tt.role)
		if monitor!=tt.monitor || servers!=tt.servers || admin!=tt.admin {
			t.Errorf("roleAccess(%q)=(%t,%t,%t), want (%t,%t,%t)",tt.role,monitor,servers,admin,tt.monitor,tt.servers,tt.admin)
		}
	}
}

func TestMonitorRoleCannotUseServerManagementOrUserAdminAPI(t *testing.T) {
	store,err:=storage.Open(t.TempDir()+"/roles-test.db")
	if err!=nil { t.Fatal(err) }
	defer store.Close()
	s:=New(store,nil)
	salt,err:=randomBytes(16)
	if err!=nil { t.Fatal(err) }
	if err=store.CreateDashboardUser(t.Context(),"monitor-user", "c2FsdA", encodeHash("temporary-password",[]byte("salt")), "monitor");err!=nil { t.Fatal(err) }
	s.sessions["monitor-session"]=authSession{Username:"monitor-user",Expires:time.Now().Add(time.Hour)}
	handler:=s.Handler()
	request:=func(path string) int {
		rec:=httptest.NewRecorder()
		req:=httptest.NewRequest(http.MethodGet,path,nil)
		req.AddCookie(&http.Cookie{Name:sessionCookie,Value:"monitor-session"})
		handler.ServeHTTP(rec,req)
		return rec.Code
	}
	if got:=request("/api/v1/overview");got!=http.StatusOK { t.Errorf("monitor role overview status=%d, want 200",got) }
	if got:=request("/api/v1/game-servers");got!=http.StatusForbidden { t.Errorf("monitor role game server status=%d, want 403",got) }
	if got:=request("/api/v1/users");got!=http.StatusForbidden { t.Errorf("monitor role user admin status=%d, want 403",got) }
	_ = salt
}
