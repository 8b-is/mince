// Package ccr is the reversible Content-Coupled Retrieval store. Raw tool
// output is kept locally so a compressed stub can always be expanded exactly.
package ccr

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	fileMagic    = "MCCR1"
	maxMemBytes  = int64(64 << 20)
	maxMemEntry  = 128
	defaultPerms = 0o700
	filePerms    = 0o600
)

// ErrNotFound is returned when a CCR id is unknown.
var ErrNotFound = errors.New("ccr: id not found")

type memEntry struct {
	id   string
	data []byte
}

// Store keeps a bounded LRU in memory and, when a directory is configured,
// a persistent gzip+CRC32 framed copy on disk.
type Store struct {
	mu         sync.Mutex
	dir        string
	lru        *list.List
	mem        map[string]*list.Element
	curBytes   int64
	hits       uint64
	misses     uint64
	puts       uint64
	diskWrites uint64
}

// Open opens (creating if needed) a disk-backed store in dir.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return NewMemory(), nil
	}
	if err := os.MkdirAll(dir, defaultPerms); err != nil {
		return nil, fmt.Errorf("ccr: mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, defaultPerms); err != nil {
		return nil, fmt.Errorf("ccr: chmod %s: %w", dir, err)
	}
	return &Store{dir: dir, lru: list.New(), mem: make(map[string]*list.Element)}, nil
}

// NewMemory returns a store with no disk persistence.
func NewMemory() *Store {
	return &Store{lru: list.New(), mem: make(map[string]*list.Element)}
}

// Put stores data and returns its ccr_<12hex> id.
func (s *Store) Put(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("ccr: empty content")
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])[:12]
	if existing, err := s.Get(id); err == nil && !bytes.Equal(existing, data) {
		id = hex.EncodeToString(sum[:])[:16]
	}

	s.mu.Lock()
	s.puts++
	s.mu.Unlock()

	if s.dir != "" {
		if err := s.writeDisk(id, data); err != nil {
			return "", err
		}
	}
	s.addMem(id, data)
	return id, nil
}

// Get returns the exact raw bytes for id.
func (s *Store) Get(id string) ([]byte, error) {
	s.mu.Lock()
	if el, ok := s.mem[id]; ok {
		s.lru.MoveToFront(el)
		data := el.Value.(*memEntry).data
		s.hits++
		s.mu.Unlock()
		return data, nil
	}
	s.mu.Unlock()

	data, err := s.readDisk(id)
	if err != nil {
		s.mu.Lock()
		s.misses++
		s.mu.Unlock()
		return nil, err
	}
	s.addMem(id, data)
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	return data, nil
}

// Has reports whether id exists in memory or on disk.
func (s *Store) Has(id string) bool {
	s.mu.Lock()
	_, ok := s.mem[id]
	s.mu.Unlock()
	if ok {
		return true
	}
	if s.dir == "" {
		return false
	}
	_, err := os.Stat(s.path(id))
	return err == nil
}

// Retrieve returns exact lines from id. With a query, only matching lines are
// returned (regexp when valid, literal substring otherwise). offset/limit
// select from the resulting line set; limit <= 0 means all remaining lines.
func (s *Store) Retrieve(id, query string, offset, limit int) (string, error) {
	data, err := s.Get(id)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if query != "" {
		var re *regexp.Regexp
		if r, err := regexp.Compile(query); err == nil {
			re = r
		}
		kept := lines[:0]
		for _, ln := range lines {
			if re != nil {
				if re.MatchString(ln) {
					kept = append(kept, ln)
				}
			} else if strings.Contains(ln, query) {
				kept = append(kept, ln)
			}
		}
		lines = kept
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(lines) {
		return "", nil
	}
	lines = lines[offset:]
	if limit > 0 && limit < len(lines) {
		lines = lines[:limit]
	}
	return strings.Join(lines, "\n"), nil
}

// Stats returns counters and resident entry count.
func (s *Store) Stats() (hits, misses, puts uint64, entries int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits, s.misses, s.puts, len(s.mem)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, "ccr_"+id+".gz")
}

func (s *Store) writeDisk(id string, data []byte) error {
	var buf bytes.Buffer
	buf.WriteString(fileMagic)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(data))
	buf.Write(crc[:])
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return fmt.Errorf("ccr: gzip: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("ccr: gzip close: %w", err)
	}
	tmp := s.path(id) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), filePerms); err != nil {
		return fmt.Errorf("ccr: write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, filePerms); err != nil {
		return fmt.Errorf("ccr: chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path(id)); err != nil {
		return fmt.Errorf("ccr: rename: %w", err)
	}
	s.mu.Lock()
	s.diskWrites++
	s.mu.Unlock()
	return nil
}

func (s *Store) readDisk(id string) ([]byte, error) {
	if s.dir == "" {
		return nil, ErrNotFound
	}
	raw, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("ccr: read: %w", err)
	}
	if len(raw) < len(fileMagic)+4 || string(raw[:len(fileMagic)]) != fileMagic {
		return nil, fmt.Errorf("ccr: bad frame for %s", id)
	}
	want := binary.BigEndian.Uint32(raw[len(fileMagic) : len(fileMagic)+4])
	zr, err := gzip.NewReader(bytes.NewReader(raw[len(fileMagic)+4:]))
	if err != nil {
		return nil, fmt.Errorf("ccr: gzip reader: %w", err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("ccr: gunzip: %w", err)
	}
	if got := crc32.ChecksumIEEE(data); got != want {
		return nil, fmt.Errorf("ccr: crc mismatch for %s", id)
	}
	return data, nil
}

func (s *Store) addMem(id string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.mem[id]; ok {
		s.lru.MoveToFront(el)
		return
	}
	el := s.lru.PushFront(&memEntry{id: id, data: data})
	s.mem[id] = el
	s.curBytes += int64(len(data))
	for s.lru.Len() > maxMemEntry || s.curBytes > maxMemBytes {
		back := s.lru.Back()
		if back == nil {
			break
		}
		ent := back.Value.(*memEntry)
		s.lru.Remove(back)
		delete(s.mem, ent.id)
		s.curBytes -= int64(len(ent.data))
	}
}
