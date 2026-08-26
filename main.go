package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"byteShare/config"
	"byteShare/model"
	"byteShare/src/service"
	"byteShare/utils"

	"github.com/gorilla/websocket"
)

//go:embed web/login.html web/index.html web/js/app.js web/js/qrcode.min.js
var webContent embed.FS

const Version = "1.0.4"

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	mode := flag.String("mode", "", "run mode (web)")
	flag.Parse()

	if err := config.Load(*configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *mode != "" {
		config.App.Server.Mode = *mode
	}

	switch config.App.Server.Mode {
	case "web":
		runWeb()
	default:
		log.Fatalf("Unknown mode: %s. Supported: web", config.App.Server.Mode)
	}
}

func runWeb() {
	store, err := model.NewStore(config.App.Database.Path)
	if err != nil {
		log.Fatalf("Failed to init store: %v", err)
	}
	log.Printf("Store initialized: %s", config.App.Database.Path)

	uploadSrv := service.NewUploadService(store)
	hub := service.NewHub()

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			uploadSrv.CleanupExpired()
			uploadSrv.CleanupStaleChunks()
		}
	}()

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/auth", service.AuthHandler)

	mux.Handle("/api/upload", service.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		uploadSrv.HandleUpload(w, r)
	})))
	mux.Handle("/api/upload/chunk", service.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		uploadSrv.HandleUploadChunk(w, r)
	})))

	mux.HandleFunc("/dl/", uploadSrv.HandleDownload)

	// WebSocket
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		pwd := r.URL.Query().Get("pwd")
		if pwd == "" {
			if cookie, err := r.Cookie("token"); err == nil {
				expected := service.GenerateToken(config.App.Auth.Password)
				if cookie.Value == expected {
					pwd = config.App.Auth.Password
				}
			}
		}
		if pwd != config.App.Auth.Password {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}

		client := service.NewClient(conn, hub)
		go client.ReadPump()
		go client.WritePump()
	})

	// API to generate a random username
	mux.HandleFunc("/api/username", func(w http.ResponseWriter, r *http.Request) {
		name := utils.GenerateUserName()
		color := utils.GetUserColor(name)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"%s","color":"%s"}`, name, color)
	})

	// API version
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":"%s"}`, Version)
	})

	// API config (public URL for frontend)
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		baseURL := config.App.Server.PublicURL
		if baseURL == "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"public_url":"%s"}`, baseURL)
	})

	// Static web files
	webFS, _ := fs.Sub(webContent, "web")
	fileServer := http.FileServer(http.FS(webFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")

		// Login page
		if p == "" || p == "login" {
			data, err := webContent.ReadFile("web/login.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		// Room route: single path segment, no extension
		if !strings.Contains(p, "/") && !strings.Contains(p, ".") {
			if !service.CheckAuth(r) {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			data, err := webContent.ReadFile("web/index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		// Try serving embedded file
		if _, err := webFS.Open(p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	addr := fmt.Sprintf("%s:%d", config.App.Server.Host, config.App.Server.Port)

	// Ensure uploads directory exists
	os.MkdirAll(config.App.Upload.Dir, 0755)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Server starting on http://%s", addr)
		log.Printf("Open http://localhost:%d in your browser", config.App.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	srv.Close()
	log.Println("Server stopped")
}
