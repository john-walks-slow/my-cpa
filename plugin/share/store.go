package share

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const idLength = 10

var ErrNotFound = errors.New("share not found")

type Snapshot struct {
	SchemaVersion int        `json:"schema_version"`
	ID            string     `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	RequireToken  bool       `json:"require_token"`
	TokenSHA256   string     `json:"token_sha256,omitempty"`
	Title         string     `json:"title"`
	Range         any        `json:"range"`
	Metric        string     `json:"metric"`
	Subjects      any        `json:"subjects"`
	Rows          any        `json:"rows"`
}
type Store struct {
	root     string
	maxCount int
	maxBytes int64
	now      func() time.Time
	mu       sync.Mutex
}

func New(root string, maxCount int, maxBytes int64) *Store {
	return &Store{root: filepath.Join(root, "shares"), maxCount: maxCount, maxBytes: maxBytes, now: time.Now}
}

// SetNow overrides the clock used for expiry checks. Intended for tests.
func (s *Store) SetNow(fn func() time.Time) { s.now = fn }
func (s *Store) Available() bool { return s.root != "" }
func (s *Store) Root() string    { return s.root }
func (s *Store) Create(snapshot Snapshot) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Available() {
		return "", fmt.Errorf("share storage is not configured")
	}
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = 1
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = s.now().UTC()
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return "", fmt.Errorf("share mkdir: %w", err)
	}
	for attempt := 0; attempt < 8; attempt++ {
		id, err := newID()
		if err != nil {
			return "", fmt.Errorf("share id: %w", err)
		}
		snapshot.ID = id
		raw, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return "", fmt.Errorf("share marshal: %w", err)
		}
		if s.maxBytes > 0 && int64(len(raw)) > s.maxBytes {
			return "", fmt.Errorf("share snapshot exceeds size limit")
		}
		path := s.path(id)
		tmp, err := os.CreateTemp(s.root, ".share-*.tmp")
		if err != nil {
			return "", fmt.Errorf("share temp: %w", err)
		}
		tmpName := tmp.Name()
		if err = tmp.Chmod(0o600); err == nil {
			_, err = tmp.Write(raw)
		}
		if err == nil {
			err = tmp.Sync()
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(tmpName)
			return "", fmt.Errorf("share write: %w", err)
		}
		if err = os.Rename(tmpName, path); err != nil {
			os.Remove(tmpName)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", fmt.Errorf("share rename: %w", err)
		}
		if err = s.enforceMaxCount(); err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("share enforce max count: %w", err)
		}
		return id, nil
	}
	return "", fmt.Errorf("share id collision limit reached")
}
func (s *Store) Read(id, token string) (Snapshot, error) {
	if !validID(id) {
		return Snapshot{}, ErrNotFound
	}
	raw, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("share read: %w", err)
	}
	if s.maxBytes > 0 && int64(len(raw)) > s.maxBytes {
		return Snapshot{}, ErrNotFound
	}
	var snap Snapshot
	if json.Unmarshal(raw, &snap) != nil || snap.SchemaVersion != 1 || snap.ID != id {
		return Snapshot{}, ErrNotFound
	}
	if snap.ExpiresAt != nil && !snap.ExpiresAt.After(s.now()) {
		return Snapshot{}, ErrNotFound
	}
	if snap.RequireToken {
		digest := sha256.Sum256([]byte(token))
		if len(snap.TokenSHA256) != sha256.Size*2 || subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(snap.TokenSHA256)) != 1 {
			return Snapshot{}, ErrNotFound
		}
	}
	return snap, nil
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(id) {
		return ErrNotFound
	}
	err := os.Remove(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("share delete: %w", err)
	}
	return nil
}
func (s *Store) Cleanup() error { s.mu.Lock(); defer s.mu.Unlock(); return s.cleanupLocked() }
func (s *Store) cleanupLocked() error {
	if !s.Available() {
		return nil
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("share cleanup: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasPrefix(name, ".share-") && strings.HasSuffix(name, ".tmp"):
			// Stray temp file from an interrupted write.
			if rmErr := os.Remove(filepath.Join(s.root, name)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				return fmt.Errorf("share temp cleanup: %w", rmErr)
			}
		case filepath.Ext(name) == ".json":
			id := strings.TrimSuffix(name, ".json")
			if !validID(id) {
				continue
			}
			var snap Snapshot
			raw, readErr := os.ReadFile(s.path(id))
			if readErr == nil && json.Unmarshal(raw, &snap) == nil && snap.ExpiresAt != nil && !snap.ExpiresAt.After(s.now()) {
				_ = os.Remove(s.path(id))
			}
		}
	}
	return s.enforceMaxCount()
}
func (s *Store) path(id string) string { return filepath.Join(s.root, id+".json") }
func (s *Store) enforceMaxCount() error {
	if s.maxCount <= 0 {
		return nil
	}
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("share count: %w", err)
	}
	type item struct {
		path    string
		created time.Time
		id      string
	}
	items := []item{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(s.root, e.Name()))
		if readErr != nil {
			continue
		}
		var snap Snapshot
		if json.Unmarshal(raw, &snap) == nil {
			items = append(items, item{filepath.Join(s.root, e.Name()), snap.CreatedAt, snap.ID})
		}
	}
	if len(items) <= s.maxCount {
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].created.Equal(items[j].created) {
			return items[i].id < items[j].id
		}
		return items[i].created.Before(items[j].created)
	})
	for _, old := range items[:len(items)-s.maxCount] {
		if err := os.Remove(old.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func NewToken() (string, string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buf)
	return token, HashToken(token), nil
}
func HashToken(token string) string {
	d := sha256.Sum256([]byte(token))
	return hex.EncodeToString(d[:])
}
func newID() (string, error) {
	out := make([]byte, idLength)
	limit := byte(256 - (256 % len(alphabet)))
	buf := make([]byte, idLength)
	for written := 0; written < idLength; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out[written] = alphabet[int(b)%len(alphabet)]
			written++
			if written == idLength {
				break
			}
		}
	}
	return string(out), nil
}
func validID(id string) bool {
	if len(id) != idLength {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune(alphabet, c) {
			return false
		}
	}
	return true
}
