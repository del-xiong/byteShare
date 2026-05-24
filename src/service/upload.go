package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"byteShare/config"
	"byteShare/model"

	"github.com/google/uuid"
)

type UploadService struct {
	store *model.Store
}

type UploadResult struct {
	OK        bool   `json:"ok"`
	Token     string `json:"token"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Mime      string `json:"mime"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func NewUploadService(store *model.Store) *UploadService {
	return &UploadService{store: store}
}

func (s *UploadService) HandleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, config.App.Upload.MaxSize*1024*1024)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, fmt.Sprintf("File too large or invalid: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	expiryDays := config.App.Upload.DefaultExpiry
	if expiryParam := r.FormValue("expiry"); expiryParam != "" {
		if d, err := strconv.Atoi(expiryParam); err == nil {
			if d < 1 {
				d = 1
			}
			if d > config.App.Upload.MaxExpiry {
				d = config.App.Upload.MaxExpiry
			}
			expiryDays = d
		}
	}

	os.MkdirAll(config.App.Upload.Dir, 0755)

	ext := filepath.Ext(header.Filename)
	token := uuid.New().String()
	savedName := token + ext
	savedPath := filepath.Join(config.App.Upload.Dir, savedName)

	dst, err := os.Create(savedPath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(savedPath)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	record := model.FileRecord{
		OriginalName: header.Filename,
		SavedPath:    savedPath,
		Size:         written,
		Token:        token,
		MimeType:     mimeType,
		ExpiresAt:    time.Now().AddDate(0, 0, expiryDays),
	}

	if err := s.store.Create(&record); err != nil {
		os.Remove(savedPath)
		http.Error(w, "Failed to save record", http.StatusInternalServerError)
		return
	}

	dlLink := "/dl/" + token
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{
		"ok":true,
		"token":"%s",
		"name":"%s",
		"size":%d,
		"mime":"%s",
		"url":"%s",
		"expires_at":"%s"
	}`,
		token, header.Filename, written, mimeType, dlLink,
		record.ExpiresAt.Format(time.RFC3339),
	)))

	log.Printf("[Upload] File saved: %s (%d bytes, expires %s)", header.Filename, written, record.ExpiresAt.Format(time.RFC3339))
}

func (s *UploadService) HandleDownload(w http.ResponseWriter, r *http.Request) {
	token := filepath.Base(r.URL.Path)
	if token == "" || token == "dl" || token == "." {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	file := s.store.GetByToken(token)
	if file == nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if time.Now().After(file.ExpiresAt) {
		http.Error(w, "File expired", http.StatusGone)
		return
	}

	f, err := os.Open(file.SavedPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "File error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.OriginalName))
	http.ServeContent(w, r, file.OriginalName, stat.ModTime(), f)
}

func (s *UploadService) CleanupExpired() {
	expired := s.store.GetExpired()
	for _, f := range expired {
		if err := os.Remove(f.SavedPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[Cleanup] Failed to remove file %s: %v", f.SavedPath, err)
		}
		if err := s.store.Delete(f.ID); err != nil {
			log.Printf("[Cleanup] Failed to delete record %d: %v", f.ID, err)
		}
		log.Printf("[Cleanup] Deleted expired file: %s (token: %s)", f.OriginalName, f.Token)
	}
}

// ---- Chunked upload support ----

type chunkMeta struct {
	Name     string
	Mime     string
	Expiry   int
	Total    int
	Dir      string
	Received map[int]bool
	Updated  time.Time
}

var (
	chunkMu     sync.Mutex
	chunkUploads = map[string]*chunkMeta{}
)

func (s *UploadService) HandleUploadChunk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, fmt.Sprintf("Invalid upload: %v", err), http.StatusBadRequest)
		return
	}

	fileID := r.FormValue("file_id")
	indexStr := r.FormValue("index")
	totalStr := r.FormValue("total")
	name := r.FormValue("name")
	mime := r.FormValue("mime")
	expiryStr := r.FormValue("expiry")

	if fileID == "" || indexStr == "" || totalStr == "" || name == "" {
		http.Error(w, "Missing fields", http.StatusBadRequest)
		return
	}

	index, err := strconv.Atoi(indexStr)
	if err != nil || index < 0 {
		http.Error(w, "Invalid index", http.StatusBadRequest)
		return
	}
	total, err := strconv.Atoi(totalStr)
	if err != nil || total < 1 || index >= total {
		http.Error(w, "Invalid total", http.StatusBadRequest)
		return
	}

	expiry := config.App.Upload.DefaultExpiry
	if d, err := strconv.Atoi(expiryStr); err == nil && d >= 1 && d <= config.App.Upload.MaxExpiry {
		expiry = d
	}

	chunkData, _, err := r.FormFile("data")
	if err != nil {
		http.Error(w, "Missing chunk data", http.StatusBadRequest)
		return
	}
	defer chunkData.Close()

	chunkMu.Lock()
	meta, exists := chunkUploads[fileID]
	if !exists {
		chunkDir := filepath.Join(config.App.Upload.Dir, ".chunks", fileID)
		if err := os.MkdirAll(chunkDir, 0755); err != nil {
			chunkMu.Unlock()
			http.Error(w, "Failed to create temp dir", http.StatusInternalServerError)
			return
		}
		meta = &chunkMeta{
			Name:     name,
			Mime:     mime,
			Expiry:   expiry,
			Total:    total,
			Dir:      chunkDir,
			Received: map[int]bool{},
		}
		chunkUploads[fileID] = meta
	}

	// Idempotent: skip if chunk already received
	if !meta.Received[index] {
		chunkPath := filepath.Join(meta.Dir, fmt.Sprintf("%d", index))
		dst, err := os.Create(chunkPath)
		if err != nil {
			chunkMu.Unlock()
			http.Error(w, "Failed to save chunk", http.StatusInternalServerError)
			return
		}
		if _, err := io.Copy(dst, chunkData); err != nil {
			dst.Close()
			chunkMu.Unlock()
			http.Error(w, "Failed to write chunk", http.StatusInternalServerError)
			return
		}
		dst.Close()
		meta.Received[index] = true
	}
	meta.Updated = time.Now()
	done := len(meta.Received) >= meta.Total
	chunkMu.Unlock()

	if !done {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"done":false}`))
		return
	}

	// All chunks received — reassemble
	record, err := s.reassembleChunks(fileID, meta)
	chunkMu.Lock()
	delete(chunkUploads, fileID)
	chunkMu.Unlock()

	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to reassemble: %v", err), http.StatusInternalServerError)
		return
	}

	dlLink := "/dl/" + record.Token
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{
		"ok":true,
		"done":true,
		"token":"%s",
		"name":"%s",
		"size":%d,
		"mime":"%s",
		"url":"%s",
		"expires_at":"%s"
	}`, record.Token, record.OriginalName, record.Size, record.MimeType, dlLink,
		record.ExpiresAt.Format(time.RFC3339))))

	log.Printf("[Upload] Chunked file saved: %s (%d bytes, expires %s)", record.OriginalName, record.Size, record.ExpiresAt.Format(time.RFC3339))
}

func (s *UploadService) reassembleChunks(fileID string, meta *chunkMeta) (*model.FileRecord, error) {
	// Build sorted list of chunk indices
	var indices []int
	for i := 0; i < meta.Total; i++ {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	ext := filepath.Ext(meta.Name)
	token := uuid.New().String()
	savedName := token + ext
	savedPath := filepath.Join(config.App.Upload.Dir, savedName)

	dst, err := os.Create(savedPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	var totalSize int64
	for _, idx := range indices {
		chunkPath := filepath.Join(meta.Dir, fmt.Sprintf("%d", idx))
		src, err := os.Open(chunkPath)
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(dst, src)
		src.Close()
		if err != nil {
			return nil, err
		}
		totalSize += n
	}

	// Clean up chunk dir
	os.RemoveAll(meta.Dir)

	mimeType := meta.Mime
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	record := &model.FileRecord{
		OriginalName: meta.Name,
		SavedPath:    savedPath,
		Size:         totalSize,
		Token:        token,
		MimeType:     mimeType,
		ExpiresAt:    time.Now().AddDate(0, 0, meta.Expiry),
	}

	if err := s.store.Create(record); err != nil {
		os.Remove(savedPath)
		return nil, err
	}

	return record, nil
}

func (s *UploadService) CleanupStaleChunks() {
	chunkMu.Lock()
	now := time.Now()
	for id, meta := range chunkUploads {
		if now.Sub(meta.Updated) > 1*time.Hour {
			os.RemoveAll(meta.Dir)
			delete(chunkUploads, id)
			log.Printf("[Cleanup] Removed stale chunk upload: %s", id)
		}
	}
	chunkMu.Unlock()
}
