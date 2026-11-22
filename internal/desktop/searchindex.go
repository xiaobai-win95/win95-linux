package desktop

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const maxIndexEntries = 300000

type searchEntry struct {
	path  string
	lname string
	dir   bool
	size  int64
}

type spotResult struct {
	path    string
	name    string
	dir     bool
	size    int64
	score   int
	content bool
}

type SearchIndex struct {
	mu      sync.RWMutex
	entries []searchEntry
	roots   []string
	ready   bool
	started bool
	start   sync.Once
}

func defaultSearchRoots() []string {
	candidates := []string{"/root", "/home", "/opt", "/srv", "/mnt", "/media", "/etc", "/var", "/usr", "/tmp"}
	roots := make([]string, 0, len(candidates))
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			roots = append(roots, p)
		}
	}
	return roots
}

func newSearchIndex() *SearchIndex {
	return &SearchIndex{roots: defaultSearchRoots()}
}

func newSearchIndexWithRoots(roots []string) *SearchIndex {
	return &SearchIndex{roots: roots}
}

func (s *SearchIndex) ensureStarted() {
	s.start.Do(func() {
		s.mu.Lock()
		s.started = true
		s.mu.Unlock()
		go s.build()
	})
}

func skipIndexPath(path string, name string, isDir bool) bool {
	if !isDir {
		return false
	}
	switch path {
	case "/proc", "/sys", "/dev", "/run", "/var/cache", "/var/lib/apk":
		return true
	}
	switch name {
	case ".git", ".cache", "node_modules", "__pycache__":
		return true
	}
	return false
}

func (s *SearchIndex) build() {
	for _, root := range s.roots {
		_ = filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
			if err != nil {
				if de != nil && de.IsDir() && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if path == root {
				return nil
			}
			name := de.Name()
			if skipIndexPath(path, name, de.IsDir()) {
				return filepath.SkipDir
			}
			var size int64
			if !de.IsDir() {
				if info, err := de.Info(); err == nil {
					size = info.Size()
				}
			}
			s.mu.Lock()
			if len(s.entries) >= maxIndexEntries {
				s.mu.Unlock()
				return filepath.SkipAll
			}
			s.entries = append(s.entries, searchEntry{
				path:  path,
				lname: strings.ToLower(name),
				dir:   de.IsDir(),
				size:  size,
			})
			s.mu.Unlock()
			return nil
		})
	}
	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()
}

func (s *SearchIndex) Stats() (count int, ready bool, started bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries), s.ready, s.started
}

func entryMatch(lname, pattern string, wild bool) int {
	if wild {
		if ok, _ := filepath.Match(pattern, lname); ok {
			return 2
		}
		return -1
	}
	if lname == pattern {
		return 0
	}
	if strings.HasPrefix(lname, pattern) {
		return 1
	}
	if strings.Contains(lname, pattern) {
		return 2
	}
	return -1
}

func sortSpotResults(out []spotResult) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.score != b.score {
			return a.score < b.score
		}
		if a.dir != b.dir {
			return a.dir
		}
		da, db := strings.Count(a.path, "/"), strings.Count(b.path, "/")
		if da != db {
			return da < db
		}
		if len(a.path) != len(b.path) {
			return len(a.path) < len(b.path)
		}
		return a.path < b.path
	})
}

// Search returns filename matches, best first. It is safe to call while the
// index is still being built; the caller simply sees a partial view.
func (s *SearchIndex) Search(pattern string, limit int) []spotResult {
	s.ensureStarted()
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" || limit <= 0 {
		return nil
	}
	wild := strings.ContainsAny(pattern, "*?")
	s.mu.RLock()
	entries := s.entries
	var out []spotResult
	for i := range entries {
		e := &entries[i]
		score := entryMatch(e.lname, pattern, wild)
		if score < 0 {
			continue
		}
		out = append(out, spotResult{
			path:  e.path,
			name:  filepath.Base(e.path),
			dir:   e.dir,
			size:  e.size,
			score: score,
		})
	}
	s.mu.RUnlock()
	sortSpotResults(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *SearchIndex) Covers(scope string) bool {
	scope = filepath.Clean(scope)
	for _, root := range s.roots {
		if scope == root || strings.HasPrefix(scope, root+"/") {
			return true
		}
	}
	return false
}

func relativeHidden(rel string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// SearchIn restricts filename matches to a directory, honoring the Find
// dialog's recursive and hidden-file options.
func (s *SearchIndex) SearchIn(pattern, scope string, recursive, hidden, filesOnly bool, limit int) []spotResult {
	s.ensureStarted()
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	scope = filepath.Clean(scope)
	if pattern == "" || limit <= 0 {
		return nil
	}
	wild := strings.ContainsAny(pattern, "*?")
	s.mu.RLock()
	entries := s.entries
	var out []spotResult
	for i := range entries {
		e := &entries[i]
		if filesOnly && e.dir {
			continue
		}
		rel, err := filepath.Rel(scope, e.path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if !recursive && strings.Contains(rel, string(filepath.Separator)) {
			continue
		}
		if !hidden && relativeHidden(rel) {
			continue
		}
		score := entryMatch(e.lname, pattern, wild)
		if score < 0 {
			continue
		}
		out = append(out, spotResult{
			path:  e.path,
			name:  filepath.Base(e.path),
			dir:   e.dir,
			size:  e.size,
			score: score,
		})
	}
	s.mu.RUnlock()
	sortSpotResults(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func fileContainsText(path, lowerQuery string, maxSize int64) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() <= 0 || st.Size() > maxSize {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return false
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	return bytes.Contains(bytes.ToLower(b), []byte(lowerQuery))
}

func textSearchable(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return searchTextExt[ext] || ext == ""
}

var searchTextExt = map[string]bool{
	".txt": true, ".md": true, ".conf": true, ".cfg": true, ".ini": true,
	".log": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".sh": true, ".go": true, ".c": true, ".h": true, ".py": true, ".js": true,
	".html": true, ".htm": true, ".css": true, ".xml": true, ".csv": true,
	".desktop": true, ".service": true, ".env": true, ".properties": true,
}

// ContentSearch greps small text files from the filename index. It is meant
// to run in a goroutine with a deadline while results stream to the UI.
func (s *SearchIndex) ContentSearch(ctx context.Context, query string, limit int) []spotResult {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || limit <= 0 {
		return nil
	}
	s.mu.RLock()
	var candidates []searchEntry
	for _, e := range s.entries {
		if e.dir || e.size <= 0 || e.size > 1<<20 {
			continue
		}
		if !searchTextExt[strings.ToLower(filepath.Ext(e.lname))] {
			continue
		}
		candidates = append(candidates, e)
	}
	s.mu.RUnlock()

	var out []spotResult
	scanned := 0
	for _, e := range candidates {
		if scanned >= 4000 || len(out) >= limit {
			break
		}
		select {
		case <-ctx.Done():
			return out
		default:
		}
		scanned++
		if !fileContainsText(e.path, query, 1<<20) {
			continue
		}
		out = append(out, spotResult{
			path:    e.path,
			name:    filepath.Base(e.path),
			size:    e.size,
			score:   10,
			content: true,
		})
	}
	return out
}

func (d *Desktop) searchIndex() *SearchIndex {
	if d.search == nil {
		d.search = newSearchIndex()
	}
	return d.search
}
