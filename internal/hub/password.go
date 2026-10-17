package hub

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/yourfriendaaron/meat-mode/internal/api"
	"github.com/yourfriendaaron/meat-mode/internal/auth"
	"github.com/yourfriendaaron/meat-mode/internal/store"
)

// Setting the password in the app (docs/api.md, Password).
const (
	setupCodeTTL  = time.Hour
	setupCodeFile = "setup-code" // in the data directory
	minPassword   = 8            // characters
)

// passwordRoutes registers claiming the hub, changing its password and
// choosing no password.
func (s *Server) passwordRoutes(m *http.ServeMux) {
	m.HandleFunc("POST /api/setup/claim", s.handleClaim)
	m.HandleFunc("PUT /api/password", s.handleChangePassword)
	m.HandleFunc("PUT /api/setup/tailscale-only", s.handleTailscaleOnly)
}

const envPassword = "the password is set by the hub's environment (MEAT_MODE_PASSWORD_HASH); change it there"

// mintSetupCode makes a new setup code while the hub has no password, logs
// it and writes it to the setup-code file in the data directory, readable
// by the hub's user only. With a password it removes a file left from
// before.
func mintSetupCode(cfg Config, st *store.Store) error {
	path := filepath.Join(cfg.DataDir, setupCodeFile)
	h, err := st.Settings()
	if err != nil {
		return err
	}
	if cfg.PasswordHash != "" || h.PasswordHash != "" {
		return removeSetupCode(path)
	}
	code, err := st.NewSetupCode(setupCodeTTL)
	if err != nil {
		return err
	}
	if err := removeSetupCode(path); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("write the setup code: %w", err)
	}
	_, err = f.WriteString(code + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write the setup code: %w", err)
	}
	slog.Warn("this hub has no password: open the app and set one with this setup code, valid for an hour; restart the hub for a new one",
		"code", code, "file", path)
	return nil
}

func removeSetupCode(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the old setup code: %w", err)
	}
	return nil
}

// checkNewPassword returns why password cannot be used, or "".
func checkNewPassword(password string) string {
	if utf8.RuneCountInString(password) < minPassword {
		return fmt.Sprintf("the password must be at least %d characters", minPassword)
	}
	return ""
}

// handleClaim sets the first password with the setup code, and logs this
// browser in. It needs no session: the hub has no login yet.
func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	if l := s.currentLogin(); l != nil {
		msg := "this hub already has a password"
		if l.env {
			msg = envPassword
		}
		writeError(w, http.StatusConflict, msg)
		return
	}
	var body api.SetupClaim
	if !readJSON(w, r, maxLoginBody, &body) {
		return
	}
	if msg := checkNewPassword(body.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	var l *login
	ok, wait, err := s.attempt(&s.codeFails, func() (bool, error) {
		phc, err := auth.HashPassword(body.Password)
		if err != nil {
			return false, err
		}
		err = s.store.ClaimSetupCode(body.Code, phc)
		if errors.Is(err, store.ErrUnauthorized) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		l = newLogin(phc, false)
		s.setLogin(l)
		return true, nil
	})
	if wait > 0 {
		tooMany(w, wait, "too many wrong setup codes; try again in a minute")
		return
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !ok {
		// 403, not 401: the app takes a 401 to mean its session ended.
		writeError(w, http.StatusForbidden, "the setup code is wrong, expired or used; restart the hub for a new one")
		return
	}
	// The hub is claimed and the code spent, so nothing below fails the
	// request. Without a session cookie the app shows its login screen.
	if err := removeSetupCode(filepath.Join(s.cfg.DataDir, setupCodeFile)); err != nil {
		s.log.Warn("claimed the hub", "err", err)
	}
	if err := s.store.SetTailscaleOnly(false); err != nil {
		s.log.Warn("claimed the hub; tailscale-only choice not cleared", "err", err)
	}
	if err := s.startSession(w, l); err != nil {
		s.log.Warn("claimed the hub; log in to continue", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleChangePassword sets a new password given the current one. This
// browser stays logged in; every other one is logged out.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var body api.PasswordChange
	if !readJSON(w, r, maxLoginBody, &body) {
		return
	}
	if msg := checkNewPassword(body.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	sess := sessionOf(r)
	var none, env, out bool
	var ended []string
	ok, wait, err := s.attempt(&s.loginFails, func() (bool, error) {
		l := s.currentLogin()
		none, env = l == nil, l != nil && l.env
		out = l != nil && sess == nil // the hub was claimed after the gate let r in
		if none || env || out {
			return true, nil
		}
		if !l.hash.Verify(body.Current) {
			return false, nil
		}
		phc, err := auth.HashPassword(body.Password)
		if err != nil {
			return false, err
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			return false, err
		}
		nl := newLogin(phc, false)
		if ended, err = s.store.ChangePassword(phc, c.Value, nl.tag); err != nil {
			return false, err
		}
		s.setLogin(nl)
		return true, nil
	})
	switch {
	case wait > 0:
		tooMany(w, wait, "too many wrong passwords; try again in a minute")
		return
	case err != nil:
		s.storeError(w, r, err)
		return
	case none:
		writeError(w, http.StatusConflict, "this hub has no password; set one with the setup code")
		return
	case env:
		writeError(w, http.StatusConflict, envPassword)
		return
	case out:
		writeError(w, http.StatusUnauthorized, "log in first")
		return
	case !ok:
		writeError(w, http.StatusForbidden, "wrong password")
		return
	}
	if s.push != nil {
		for _, id := range ended {
			// The password is already changed; a subscription left behind is
			// pruned once its session is seen to have ended.
			if err := s.push.DropSession(r.Context(), id); err != nil {
				slog.Warn("could not drop the reminders of a logged-out session", "err", err)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTailscaleOnly records whether the user keeps the hub with no
// password on a private network, so setup stops asking for one.
func (s *Server) handleTailscaleOnly(w http.ResponseWriter, r *http.Request) {
	var body api.TailscaleOnly
	if !readJSON(w, r, maxBody, &body) {
		return
	}
	if body.On && s.currentLogin() != nil {
		writeError(w, http.StatusConflict, "this hub has a password")
		return
	}
	if err := s.store.SetTailscaleOnly(body.On); err != nil {
		s.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
