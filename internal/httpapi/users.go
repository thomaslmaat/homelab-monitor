package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/thomaslmaat/homelab-monitor/internal/storage"
)

var dashboardUsernamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

type createUserPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Permissions storage.DashboardPermissions `json:"permissions"`
}

type updateUserPayload struct {
	Permissions storage.DashboardPermissions `json:"permissions"`
	Password string `json:"password"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListDashboardUsers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"Gebruikers konden niet worden opgehaald"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users":users})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	var p createUserPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error":"Ongeldig verzoek"})
		return
	}
	p.Username = strings.TrimSpace(p.Username)
	if !dashboardUsernamePattern.MatchString(p.Username) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error":"Gebruikersnaam moet 3–32 tekens bevatten (letters, cijfers, punt, _ of -)"})
		return
	}
	if len(p.Password) < 12 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error":"Het tijdelijke wachtwoord moet minimaal 12 tekens bevatten"})
		return
	}
	salt, err := randomBytes(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"Kon geen wachtwoord beveiligen"})
		return
	}
	err = s.store.CreateDashboardUser(r.Context(), p.Username, base64.RawStdEncoding.EncodeToString(salt), encodeHash(p.Password,salt), p.Permissions)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error":"Deze gebruikersnaam bestaat al"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"status":"created","username":p.Username,"permissions":p.Permissions,"must_change_password":true})
}

func (s *Server) updateUserRole(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PathValue("username"))
	if username == "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error":"Het ingebouwde admin-account kan niet worden aangepast"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	var p updateUserPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error":"Ongeldig verzoek"})
		return
	}
	if p.Password != "" && len(p.Password) < 12 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error":"Een nieuw wachtwoord moet minimaal 12 tekens bevatten"})
		return
	}
	if err := s.store.UpdateDashboardPermissions(r.Context(), username, p.Permissions); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"Gebruikersrol kon niet worden bijgewerkt"})
		return
	}
	if p.Password != "" {
		salt, err := randomBytes(16)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"Kon geen wachtwoord beveiligen"})
			return
		}
		if err = s.store.ResetDashboardPassword(r.Context(), username, base64.RawStdEncoding.EncodeToString(salt), encodeHash(p.Password,salt)); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"Wachtwoord kon niet worden gereset"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status":"updated","username":username,"permissions":p.Permissions,"password_reset":p.Password != ""})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PathValue("username"))
	if username == "admin" || username == s.sessionUser(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error":"Je kunt het admin-account of je eigen account niet verwijderen"})
		return
	}
	if err := s.store.DeleteDashboardUser(r.Context(), username); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"Gebruiker kon niet worden verwijderd"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status":"deleted","username":username})
}
