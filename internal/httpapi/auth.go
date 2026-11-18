package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookie = "hm_session"
	sessionTTL = 12 * time.Hour
	passwordIterations = 210000
)

type authSession struct { Username string; Expires time.Time }
type loginAttempt struct { Count int; LockedUntil time.Time }

type loginPayload struct { Username string `json:"username"`; Password string `json:"password"` }
type passwordPayload struct { CurrentPassword string `json:"current_password"`; NewPassword string `json:"new_password"` }

func randomBytes(n int) ([]byte,error) { b:=make([]byte,n); _,err:=rand.Read(b); return b,err }
func passwordKey(password string, salt []byte) []byte {
	// PBKDF2-HMAC-SHA256. The stored value is a salted, deliberately expensive hash, never plaintext.
	mac:=hmac.New(sha256.New,[]byte(password))
	mac.Write(salt); mac.Write([]byte{0,0,0,1})
	u:=mac.Sum(nil); out:=append([]byte(nil),u...)
	for i:=1;i<passwordIterations;i++ { mac=hmac.New(sha256.New,[]byte(password)); mac.Write(u); u=mac.Sum(nil); for j:=range out { out[j]^=u[j] } }
	return out
}
func encodeHash(password string, salt []byte) string { return base64.RawStdEncoding.EncodeToString(passwordKey(password,salt)) }
func secureEqual(a,b string) bool { return subtle.ConstantTimeCompare([]byte(a),[]byte(b))==1 }

func (s *Server) bootstrapAdmin() error {
	salt,err:=randomBytes(16); if err!=nil{return err}
	return s.store.EnsureDashboardAdmin(nilContext{},base64.RawStdEncoding.EncodeToString(salt),encodeHash("admin",salt))
}

type nilContext struct{}
func (nilContext) Deadline()(time.Time,bool){return time.Time{},false}
func (nilContext) Done() <-chan struct{}{return nil}
func (nilContext) Err() error{return nil}
func (nilContext) Value(any) any{return nil}

func (s *Server) authStatus(w http.ResponseWriter,r *http.Request) {
	username,ok:=s.sessionUsername(r)
	if !ok { writeJSON(w,http.StatusOK,map[string]any{"authenticated":false}); return }
	_,_,mustChange,err:=s.store.DashboardUser(r.Context(),username)
	if err!=nil { writeJSON(w,500,map[string]string{"error":"could not read account"});return }
	writeJSON(w,200,map[string]any{"authenticated":true,"username":username,"must_change_password":mustChange})
}
func (s *Server) login(w http.ResponseWriter,r *http.Request) {
	ip:=clientIP(r); now:=time.Now()
	s.authMu.Lock(); attempt:=s.loginAttempts[ip]
	if now.Before(attempt.LockedUntil) { s.authMu.Unlock(); writeJSON(w,429,map[string]string{"error":"Too many attempts. Try again later."});return }
	s.authMu.Unlock()
	r.Body=http.MaxBytesReader(w,r.Body,4096); defer r.Body.Close()
	var p loginPayload
	if err:=json.NewDecoder(r.Body).Decode(&p);err!=nil { writeJSON(w,400,map[string]string{"error":"Invalid request"});return }
	salt,stored,_,err:=s.store.DashboardUser(r.Context(),p.Username)
	valid:=false
	if err==nil { saltBytes,e:=base64.RawStdEncoding.DecodeString(salt); if e==nil { valid=secureEqual(encodeHash(p.Password,saltBytes),stored) } }
	if !valid {
		s.authMu.Lock(); a:=s.loginAttempts[ip]; a.Count++; if a.Count>=5 { a.LockedUntil=now.Add(15*time.Minute); a.Count=0 }; s.loginAttempts[ip]=a; s.authMu.Unlock()
		writeJSON(w,401,map[string]string{"error":"Gebruikersnaam of wachtwoord is onjuist"});return
	}
	s.authMu.Lock(); delete(s.loginAttempts,ip); s.authMu.Unlock()
	raw,err:=randomBytes(32); if err!=nil { writeJSON(w,500,map[string]string{"error":"Could not create session"});return }
	id:=base64.RawURLEncoding.EncodeToString(raw)
	s.authMu.Lock(); s.sessions[id]=authSession{Username:p.Username,Expires:now.Add(sessionTTL)}; s.authMu.Unlock()
	http.SetCookie(w,&http.Cookie{Name:sessionCookie,Value:id,Path:"/",HttpOnly:true,Secure:isHTTPS(r),SameSite:http.SameSiteStrictMode,MaxAge:int(sessionTTL.Seconds())})
	writeJSON(w,200,map[string]bool{"ok":true})
}
func (s *Server) changePassword(w http.ResponseWriter,r *http.Request) {
	username,ok:=s.sessionUsername(r); if !ok { writeJSON(w,401,map[string]string{"error":"Authentication required"});return }
	r.Body=http.MaxBytesReader(w,r.Body,4096); defer r.Body.Close()
	var p passwordPayload
	if err:=json.NewDecoder(r.Body).Decode(&p);err!=nil { writeJSON(w,400,map[string]string{"error":"Invalid request"});return }
	if len(p.NewPassword)<12 { writeJSON(w,400,map[string]string{"error":"Gebruik minimaal 12 tekens voor je nieuwe wachtwoord"});return }
	salt,stored,_,err:=s.store.DashboardUser(r.Context(),username); if err!=nil { writeJSON(w,500,map[string]string{"error":"Could not read account"});return }
	saltBytes,err:=base64.RawStdEncoding.DecodeString(salt); if err!=nil || !secureEqual(encodeHash(p.CurrentPassword,saltBytes),stored) { writeJSON(w,401,map[string]string{"error":"Huidig wachtwoord is onjuist"});return }
	newSalt,err:=randomBytes(16); if err!=nil { writeJSON(w,500,map[string]string{"error":"Could not update password"});return }
	if err=s.store.SetDashboardPassword(r.Context(),username,base64.RawStdEncoding.EncodeToString(newSalt),encodeHash(p.NewPassword,newSalt));err!=nil { writeJSON(w,500,map[string]string{"error":"Could not save password"});return }
	writeJSON(w,200,map[string]bool{"ok":true})
}
func (s *Server) logout(w http.ResponseWriter,r *http.Request) {
	if c,err:=r.Cookie(sessionCookie);err==nil { s.authMu.Lock(); delete(s.sessions,c.Value); s.authMu.Unlock() }
	http.SetCookie(w,&http.Cookie{Name:sessionCookie,Value:"",Path:"/",HttpOnly:true,Secure:isHTTPS(r),SameSite:http.SameSiteStrictMode,MaxAge:-1})
	writeJSON(w,200,map[string]bool{"ok":true})
}
func (s *Server) sessionUsername(r *http.Request)(string,bool) {
	c,err:=r.Cookie(sessionCookie); if err!=nil{return "",false}
	now:=time.Now(); s.authMu.Lock(); defer s.authMu.Unlock()
	session,ok:=s.sessions[c.Value]; if !ok{return "",false}
	if now.After(session.Expires){delete(s.sessions,c.Value);return "",false}
	return session.Username,true
}
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
		path:=r.URL.Path
		if path=="/healthz" || path=="/api/v1/auth/status" || path=="/api/v1/auth/login" || path=="/api/v1/auth/logout" || path=="/api/v1/metrics" || !strings.HasPrefix(path,"/api/") { next.ServeHTTP(w,r);return }
		username,ok:=s.sessionUsername(r)
		if !ok { writeJSON(w,401,map[string]string{"error":"authentication_required"});return }
		_,_,mustChange,err:=s.store.DashboardUser(r.Context(),username)
		if err!=nil { writeJSON(w,500,map[string]string{"error":"could not read account"});return }
		if mustChange && path!="/api/v1/auth/password" { writeJSON(w,403,map[string]string{"error":"password_change_required"});return }
		next.ServeHTTP(w,r)
	})
}
func clientIP(r *http.Request) string { host,_,err:=net.SplitHostPort(r.RemoteAddr); if err==nil{return host}; return r.RemoteAddr }
func isHTTPS(r *http.Request) bool { return r.TLS!=nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"),"https") }
