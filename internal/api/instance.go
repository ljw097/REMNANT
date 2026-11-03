package api

import (
	"errors"
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

func checkPassword(username, password string) error {
	if err := auth.CheckPassword(username, password); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

// policy returns the instance policy, or the team preset if it cannot be
// read: when in doubt, keep the two-person rules.
func (s *Server) policy(r *http.Request) store.Policy {
	p, err := s.store.Policy(r.Context())
	if err != nil {
		s.log.Error("reading instance policy", "err", err)
		return store.Presets[store.ProfileTeam]
	}
	return p
}

// authStatus is public: the web app asks it on load who is signed in (by
// the session cookie, if any), and whether to show first-run setup or the
// account request form.
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.UserCount(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var user *auth.Principal
	if token, ok := auth.SessionTokenFrom(r); ok {
		if p, err := s.store.LookupSession(r.Context(), auth.Digest(token)); err == nil {
			user = &p
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":             user,
		"setup_required":   n == 0,
		"required":         n == 0, // GET /setup compatibility
		"account_requests": n > 0 && s.policy(r).AccountRequests,
		"presets":          store.Presets,
	})
}

// postSetup creates the first administrator, records the instance policy
// and signs the administrator in. It needs the one-time code remnantd
// printed to its log, and works only while there are no users.
func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code         string       `json:"code"`
		Username     string       `json:"username"`
		Password     string       `json:"password"`
		InstanceName string       `json:"instance_name"`
		Policy       store.Policy `json:"policy"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validate(w, checkUsername(in.Username), checkPassword(in.Username, in.Password),
		required("code", in.Code, 40), maxLen("instance_name", in.InstanceName, 60)) {
		return
	}
	if !s.limiter.allow(clientIP(r)) {
		tooManyAttempts(w)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.store.Setup(r.Context(), store.SetupInput{Policy: in.Policy, Username: in.Username, PasswordHash: hash, Code: in.Code})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.InstanceName != "" {
		_ = s.store.SetSetting(r.Context(), brandingKey, Branding{Name: in.InstanceName}, u.ID)
	}
	pol, _ := s.store.Policy(r.Context())
	s.log.Info("first-run setup completed", "profile", pol.Profile, "admin", u.Username)
	p, ok := s.startSession(w, r, in.Username, in.Password, true)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": p, "policy": pol})
}

func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	pol, err := s.store.Policy(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": pol, "presets": store.Presets})
}

func (s *Server) setInstance(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Policy store.Policy `json:"policy"`
	}
	if !decode(w, r, &in) {
		return
	}
	pol, err := s.store.SetPolicy(r.Context(), in.Policy, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("instance policy changed", "profile", pol.Profile, "self_approval", pol.SelfApproval,
		"self_review", pol.SelfReview, "admin_lab_approval", pol.AdminLabApproval, "by", principal(r).Username)
	writeJSON(w, http.StatusOK, map[string]any{"policy": pol, "presets": store.Presets})
}

// createUser adds a user. Without a password one is generated and
// returned once; the user must change it at the first sign-in.
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string    `json:"username"`
		Role     auth.Role `json:"role"`
		Password string    `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	var roleErr, pwErr error
	if !in.Role.Valid() {
		roleErr = badRequest("role must be admin, researcher or reviewer")
	}
	if in.Password != "" {
		pwErr = checkPassword(in.Username, in.Password)
	}
	if !validate(w, checkUsername(in.Username), roleErr, pwErr) {
		return
	}
	password, generated := in.Password, in.Password == ""
	if generated {
		password = auth.NewTemporaryPassword()
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.store.CreateUserWithPassword(r.Context(), in.Username, in.Role, hash, generated)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := map[string]any{"user": u}
	if generated {
		out["temporary_password"] = password
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Role     *auth.Role `json:"role"`
		Disabled *bool      `json:"disabled"`
		Approve  bool       `json:"approve"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role != nil && !in.Role.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be admin, researcher or reviewer")
		return
	}
	u, err := s.store.UpdateUser(r.Context(), id, store.UserPatch{Role: in.Role, Disabled: in.Disabled, Approve: in.Approve})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Approve {
		s.log.Info("account request approved", "user", u.Username, "by", principal(r).Username)
	}
	writeJSON(w, http.StatusOK, u)
}

// deleteUser declines an account request. Accounts that were ever active
// are disabled instead, so their history keeps a name.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAccountRequest(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusConflict, "conflict", "only pending account requests can be deleted; disable the account instead")
			return
		}
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetPassword gives a user a new temporary password, shown once, and
// signs out their sessions.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	password := auth.NewTemporaryPassword()
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), id, hash, true, 0); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("password reset", "user_id", id, "by", principal(r).Username)
	writeJSON(w, http.StatusOK, map[string]string{"temporary_password": password})
}

// issueToken gives a user a new API token for scripts, shown once.
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
		in.Name = "api"
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
