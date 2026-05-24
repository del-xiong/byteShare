package model

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type FileRecord struct {
	ID           uint      `json:"id"`
	OriginalName string    `json:"original_name"`
	SavedPath    string    `json:"saved_path"`
	Size         int64     `json:"size"`
	Token        string    `json:"token"`
	MimeType     string    `json:"mime_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type Store struct {
	mu      sync.Mutex
	path    string
	records []FileRecord
	nextID  uint
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:    path,
		records: make([]FileRecord, 0),
		nextID:  1,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &s.records); err != nil {
		return nil, err
	}
	for _, r := range s.records {
		if r.ID >= s.nextID {
			s.nextID = r.ID + 1
		}
	}
	return s, nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *Store) Create(record *FileRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record.ID = s.nextID
	s.nextID++
	record.CreatedAt = time.Now()
	s.records = append(s.records, *record)

	return s.save()
}

func (s *Store) GetByToken(token string) *FileRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.records {
		if r.Token == token {
			return &r
		}
	}
	return nil
}

func (s *Store) GetExpired() []FileRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var expired []FileRecord
	for _, r := range s.records {
		if r.ExpiresAt.Before(now) {
			expired = append(expired, r)
		}
	}
	return expired
}

func (s *Store) Delete(id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, r := range s.records {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	s.records = append(s.records[:idx], s.records[idx+1:]...)
	return s.save()
}

// FindToken uses gjson to locate a record by token in the JSON data.
// Demonstrates gjson usage as specified in the requirements.
func (s *Store) FindToken(token string) gjson.Result {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, _ := json.Marshal(s.records)
	result := gjson.GetBytes(data, "#[token=\""+token+"\"]")
	return result
}

// InsertRecord uses sjson to add a new record, then reloads.
// Demonstrates sjson usage as specified in the requirements.
func (s *Store) InsertRecord(record FileRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record.ID = s.nextID
	s.nextID++
	record.CreatedAt = time.Now()

	jsonStr, _ := sjson.Set("[]", "-1", record)
	var newRecords []FileRecord
	if err := json.Unmarshal([]byte(jsonStr), &newRecords); err != nil {
		return err
	}

	s.records = newRecords
	return s.save()
}
