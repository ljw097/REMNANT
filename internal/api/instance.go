package api

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ljw097/remnant/internal/auth"
	"github.com/ljw097/remnant/internal/store"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

func checkUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return badRequest("username must be 2–64 lowercase letters, digits, . _ -")
	}
	return nil
}

// personal reports whether one person may decide their own requests.
func (s *Server) personal(ctx context.Context) bool {
	mode, err := s.store.DeploymentMode(ctx)
	return err == nil && mode == store.ModePersonal
}

// getSetup is public: the web UI asks it before anyone can sign in.
func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.UserCount(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"required": n == 0})
}

// postSetup creates the first administrator. It needs the one-time code
// remnantd printed to its log, and works only while there are no users.
func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code         string `json:"code"`
		Mode         string `json:"mode"`
		Username     string `json:"username"`
		InstanceName string `json:"instance_name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validate(w, oneOf("mode", in.Mode, store.ModeTeam, store.ModePersonal), checkUsername(in.Username),
		required("code", in.Code, 40), maxLen("instance_name", in.InstanceName, 60)) {
		return
	}
	u, token, err := s.store.Setup(r.Context(), store.SetupInput{Mode: in.Mode, Username: in.Username, Code: in.Code})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.InstanceName != "" {
		_ = s.store.SetSetting(r.Context(), brandingKey, Branding{Name: in.InstanceName}, u.ID)
	}
	s.log.Info("first-run setup completed", "mode", in.Mode, "admin", u.Username)
	writeJSON(w, http.StatusCreated, map[string]any{"user": u, "token": token, "mode": in.Mode})
}

func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	mode, err := s.store.DeploymentMode(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"mode": mode})
}

func (s *Server) setInstance(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, &in) || !validate(w, oneOf("mode", in.Mode, store.ModeTeam, store.ModePersonal)) {
		return
	}
	if err := s.store.SetDeploymentMode(r.Context(), in.Mode, principal(r).UserID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("deployment mode changed", "mode", in.Mode, "by", principal(r).Username)
	writeJSON(w, http.StatusOK, map[string]string{"mode": in.Mode})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string    `json:"username"`
		Role     auth.Role `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	var roleErr error
	if !in.Role.Valid() {
		roleErr = badRequest("role must be admin, researcher or reviewer")
	}
	if !validate(w, checkUsername(in.Username), roleErr) {
		return
	}
	u, err := s.store.CreateUser(r.Context(), in.Username, in.Role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	token, err := s.store.CreateToken(r.Context(), u.ID, "initial", 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": u, "token": token})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Role     *auth.Role `json:"role"`
		Disabled *bool      `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role != nil && !in.Role.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be admin, researcher or reviewer")
		return
	}
	u, err := s.store.UpdateUser(r.Context(), id, store.UserPatch{Role: in.Role, Disabled: in.Disabled})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// issueToken gives a user a new bearer token, shown once.
func (s *Server) issueToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name     string `json:"name"`
		TTLHours int    `json:"ttl_hours"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" {
		in.Name = "web"
	}
	if in.TTLHours < 0 || in.TTLHours > 24*366 {
		writeError(w, http.StatusBadRequest, "bad_request", "ttl_hours must be 0 (no expiry) to "+strconv.Itoa(24*366))
		return
	}
	if !validate(w, maxLen("name", in.Name, 60)) {
		return
	}
	token, err := s.store.CreateToken(r.Context(), id, in.Name, time.Duration(in.TTLHours)*time.Hour)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": token})
}
