package service

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"byteShare/config"
)

func GenerateToken(password string) string {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:])
}

func CheckAuth(r *http.Request) bool {
	// Check cookie first
	cookie, err := r.Cookie("token")
	if err == nil && cookie.Value == GenerateToken(config.App.Auth.Password) {
		return true
	}

	// Check query parameter
	pwd := r.URL.Query().Get("pwd")
	if pwd == config.App.Auth.Password {
		return true
	}

	return false
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !CheckAuth(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func AuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	password := r.FormValue("password")
	if password == "" {
		password = r.URL.Query().Get("password")
	}

	if password != config.App.Auth.Password {
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	token := GenerateToken(password)
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Write([]byte(`{"ok":true}`))
}
