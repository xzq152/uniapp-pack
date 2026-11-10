package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 可选访问口令：设置环境变量 ACCESS_PASSWORD 后生效；不设则和以前一样免登录。
// 鉴权信息放在 HttpOnly Cookie 里（口令的 SHA-256 派生值，不存明文）。
// ---------------------------------------------------------------------------

const authCookie = "ups_auth"

func (a *API) authToken() string {
	sum := sha256.Sum256([]byte("uniapp-pack::" + a.cfg.Password))
	return hex.EncodeToString(sum[:])
}

func (a *API) isAuthed(r *http.Request) bool {
	if a.cfg.Password == "" {
		return true
	}
	c, err := r.Cookie(authCookie)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.authToken())) == 1
}

func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.Password == "" || a.isAuthed(r) {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if p == "/login" || p == "/api/login" || p == "/favicon.ico" ||
			strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"未登录或登录已过期"}`))
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Password == "" {
		jsonOK(w, map[string]any{"ok": true})
		return
	}
	_ = r.ParseForm()
	pass := r.FormValue("password")
	if subtle.ConstantTimeCompare([]byte(pass), []byte(a.cfg.Password)) != 1 {
		http.Error(w, "口令不正确", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    a.authToken(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
	jsonOK(w, map[string]any{"ok": true})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: authCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	jsonOK(w, map[string]any{"ok": true})
}
