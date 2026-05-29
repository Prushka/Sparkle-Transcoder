package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sparkle-transcoder/internal/config"

	log "github.com/sirupsen/logrus"
)

var ErrScanRunning = errors.New("scan already running")

type Scanner struct {
	cfg    *config.Config
	mu     sync.RWMutex
	index  *Index
	status ScanStatus
}

type scanSession struct {
	dirs  map[string]cachedDir
	files map[string]cachedFile
}

type cachedDir struct {
	entries []os.DirEntry
	err     error
}

type cachedFile struct {
	info os.FileInfo
	err  error
}

func NewScanner(cfg *config.Config) *Scanner {
	return &Scanner{
		cfg:   cfg,
		index: NewIndex(cfg.MediaRoot),
		status: ScanStatus{
			Incremental: cfg.IncrementalScan,
		},
	}
}

func (s *Scanner) Status() ScanStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := s.status
	if s.index != nil {
		status.Items = len(s.index.Items)
	}
	return status
}

func (s *Scanner) Items() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Item, 0, len(s.index.Items))
	for _, item := range s.index.Items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].SortKey < items[j].SortKey
	})
	return items
}

func (s *Scanner) Get(id string) (Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.index.Items[id]
	return item, ok
}

func (s *Scanner) LoadCache() error {
	idx, err := s.readCache()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index = idx
	s.status.Items = len(idx.Items)
	return nil
}

func (s *Scanner) Scan(ctx context.Context, force bool) (*Index, error) {
	start := time.Now().UTC()
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return nil, ErrScanRunning
	}
	s.status.Running = true
	s.status.Incremental = s.cfg.IncrementalScan && !force
	s.status.LastStartedAt = &start
	s.status.Error = ""
	s.mu.Unlock()

	idx, changed, err := s.scan(ctx, force)

	finish := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Running = false
	s.status.LastFinishedAt = &finish
	s.status.Changed = changed
	if err != nil {
		s.status.Error = err.Error()
		return nil, err
	}
	s.index = idx
	s.status.Items = len(idx.Items)
	return cloneIndex(idx), nil
}

func (s *Scanner) scan(ctx context.Context, force bool) (*Index, int, error) {
	session := newScanSession()
	idx, err := s.readCache()
	if err != nil {
		log.Infof("starting without scan cache: %v", err)
		idx = NewIndex(s.cfg.MediaRoot)
	}
	if force || !s.cfg.IncrementalScan || len(idx.Items) == 0 || idx.MediaRoot != s.cfg.MediaRoot {
		next := NewIndex(s.cfg.MediaRoot)
		if err := s.fullScan(ctx, next, session); err != nil {
			return nil, 0, err
		}
		next.GeneratedAt = time.Now().UTC()
		return next, len(next.Items), s.writeCache(next)
	}

	next := cloneIndex(idx)
	changed, err := s.incrementalScan(ctx, next, session)
	if err != nil {
		return nil, changed, err
	}
	next.GeneratedAt = time.Now().UTC()
	return next, changed, s.writeCache(next)
}

func (s *Scanner) fullScan(ctx context.Context, idx *Index, session *scanSession) error {
	roots, err := s.scanRoots()
	if err != nil {
		return err
	}
	for _, root := range roots {
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				log.Warnf("skipping %s: %v", path, err)
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if s.skipPath(path) && path != root {
					return filepath.SkipDir
				}
				return s.recordDir(idx, path)
			}
			if IsVideo(d.Name()) {
				return s.upsertVideo(idx, path, session)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) incrementalScan(ctx context.Context, idx *Index, session *scanSession) (int, error) {
	changed := 0
	for id, item := range idx.Items {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		stat, err := os.Stat(item.Path)
		if err != nil {
			delete(idx.Items, id)
			changed++
			continue
		}
		if stat.Size() != item.Size || !stat.ModTime().UTC().Equal(item.ModTime.UTC()) || s.relatedChanged(item, session) {
			if err := s.upsertVideo(idx, item.Path, session); err != nil {
				log.Warnf("unable to refresh %s: %v", item.Path, err)
			}
			changed++
		}
	}

	dirs := make([]string, 0, len(idx.Dirs))
	for rel := range idx.Dirs {
		dirs = append(dirs, rel)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) < len(dirs[j])
	})
	for _, rel := range dirs {
		if err := ctx.Err(); err != nil {
			return changed, err
		}
		state, ok := idx.Dirs[rel]
		if !ok {
			continue
		}
		abs := filepath.Join(s.cfg.MediaRoot, filepath.FromSlash(rel))
		stat, err := os.Stat(abs)
		if err != nil || !stat.IsDir() {
			s.removeUnder(idx, rel)
			changed++
			continue
		}
		if stat.Size() == state.Size && stat.ModTime().UTC().Equal(state.ModTime.UTC()) {
			continue
		}
		before := len(idx.Items)
		if err := s.scanChangedDir(ctx, idx, abs, session); err != nil {
			return changed, err
		}
		s.refreshItemsUnder(idx, rel, session)
		after := len(idx.Items)
		if after != before {
			changed += absInt(after - before)
		} else {
			changed++
		}
	}
	return changed, nil
}

func (s *Scanner) scanChangedDir(ctx context.Context, idx *Index, dir string, session *scanSession) error {
	if s.skipPath(dir) {
		return nil
	}
	if err := s.recordDir(idx, dir); err != nil {
		return err
	}
	entries, err := session.readDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		abs := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if s.skipPath(abs) {
				continue
			}
			rel := s.rel(abs)
			stat, err := entry.Info()
			if err != nil {
				continue
			}
			if cached, ok := idx.Dirs[rel]; ok && stat.Size() == cached.Size && stat.ModTime().UTC().Equal(cached.ModTime.UTC()) {
				continue
			}
			if err := s.scanChangedDir(ctx, idx, abs, session); err != nil {
				return err
			}
			continue
		}
		if IsVideo(entry.Name()) {
			if err := s.upsertVideo(idx, abs, session); err != nil {
				log.Warnf("unable to scan %s: %v", abs, err)
			}
		}
	}
	return nil
}

func (s *Scanner) upsertVideo(idx *Index, path string, session *scanSession) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	item, err := parseItem(s.cfg.MediaRoot, path, stat.Size(), stat.ModTime().UnixNano())
	if err != nil {
		return err
	}
	related := s.collectRelated(path, session)
	item.Poster = firstRelated(related, RelatedPoster)
	item.Fanart = firstRelated(related, RelatedFanart)
	item.Subtitles = filterRelated(related, RelatedSubtitle)
	item.NFO = filterRelated(related, RelatedNFO)
	item.Attachments = filterRelated(related, RelatedAttachment)
	idx.Items[item.ID] = item
	s.updateScanProgress(len(idx.Items))
	return nil
}

func (s *Scanner) collectRelated(videoPath string, session *scanSession) []RelatedFile {
	out := make([]RelatedFile, 0)
	videoDir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	add := func(kind RelatedKind, path string) {
		if file, ok := session.relatedFile(s, kind, path); ok && !containsRelated(out, file.Path, kind) {
			out = append(out, file)
		}
	}

	entries, err := session.readDir(videoDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			lower := strings.ToLower(name)
			abs := filepath.Join(videoDir, name)
			nameBase := strings.TrimSuffix(name, filepath.Ext(name))
			if isSubtitle(name) && (strings.HasPrefix(nameBase, base+".") || strings.HasPrefix(nameBase, base+" ") || strings.EqualFold(nameBase, base)) {
				add(RelatedSubtitle, abs)
				continue
			}
			if strings.EqualFold(nameBase, base) && strings.EqualFold(filepath.Ext(name), ".nfo") {
				add(RelatedNFO, abs)
				continue
			}
			if isImage(name) && (lower == "poster.jpg" || lower == "folder.jpg" || lower == "cover.jpg" || strings.HasPrefix(lower, strings.ToLower(base)+"-thumb") || strings.HasPrefix(lower, strings.ToLower(base)+"-poster")) {
				add(RelatedPoster, abs)
				continue
			}
			if isImage(name) && (lower == "fanart.jpg" || strings.HasPrefix(lower, strings.ToLower(base)+"-fanart")) {
				add(RelatedFanart, abs)
				continue
			}
		}
	}

	for dir := videoDir; strings.HasPrefix(dir, s.cfg.MediaRoot); dir = filepath.Dir(dir) {
		for _, name := range []string{"poster.jpg", "folder.jpg", "cover.jpg"} {
			add(RelatedPoster, filepath.Join(dir, name))
		}
		add(RelatedFanart, filepath.Join(dir, "fanart.jpg"))
		for _, name := range []string{"movie.nfo", "tvshow.nfo"} {
			add(RelatedNFO, filepath.Join(dir, name))
		}
		if dir == s.cfg.MediaRoot {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].RelPath < out[j].RelPath
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func (session *scanSession) relatedFile(s *Scanner, kind RelatedKind, path string) (RelatedFile, bool) {
	stat, err := session.stat(path)
	if err != nil || stat.IsDir() {
		return RelatedFile{}, false
	}
	return RelatedFile{
		Kind:    kind,
		Path:    path,
		RelPath: s.rel(path),
		Name:    filepath.Base(path),
		Ext:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		Size:    stat.Size(),
		ModTime: stat.ModTime().UTC(),
	}, true
}

func (s *Scanner) relatedChanged(item Item, session *scanSession) bool {
	for _, file := range allRelated(item) {
		stat, err := session.stat(file.Path)
		if err != nil || stat.IsDir() {
			return true
		}
		if stat.Size() != file.Size || !stat.ModTime().UTC().Equal(file.ModTime.UTC()) {
			return true
		}
	}
	return false
}

func allRelated(item Item) []RelatedFile {
	files := make([]RelatedFile, 0, 2+len(item.Subtitles)+len(item.NFO)+len(item.Attachments))
	if item.Poster != nil {
		files = append(files, *item.Poster)
	}
	if item.Fanart != nil {
		files = append(files, *item.Fanart)
	}
	files = append(files, item.Subtitles...)
	files = append(files, item.NFO...)
	files = append(files, item.Attachments...)
	return files
}

func (s *Scanner) refreshItemsUnder(idx *Index, dirRel string, session *scanSession) {
	for _, item := range idx.Items {
		if dirRel == "." || item.RelPath == dirRel || strings.HasPrefix(item.RelPath, strings.TrimSuffix(dirRel, "/")+"/") {
			if err := s.upsertVideo(idx, item.Path, session); err != nil {
				log.Warnf("unable to refresh related files for %s: %v", item.Path, err)
			}
		}
	}
}

func (s *Scanner) removeUnder(idx *Index, dirRel string) {
	for id, item := range idx.Items {
		if item.RelPath == dirRel || strings.HasPrefix(item.RelPath, strings.TrimSuffix(dirRel, "/")+"/") {
			delete(idx.Items, id)
		}
	}
	for rel := range idx.Dirs {
		if rel == dirRel || strings.HasPrefix(rel, strings.TrimSuffix(dirRel, "/")+"/") {
			delete(idx.Dirs, rel)
		}
	}
}

func (s *Scanner) recordDir(idx *Index, path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	idx.Dirs[s.rel(path)] = DirectoryState{
		RelPath: s.rel(path),
		ModTime: stat.ModTime().UTC(),
		Size:    stat.Size(),
	}
	return nil
}

func (s *Scanner) scanRoots() ([]string, error) {
	roots := make([]string, 0)
	for _, library := range s.cfg.MediaLibraries {
		library = strings.TrimSpace(library)
		if library == "" {
			continue
		}
		path := filepath.Join(s.cfg.MediaRoot, filepath.FromSlash(library))
		stat, err := os.Stat(path)
		if err == nil && stat.IsDir() && !s.skipPath(path) {
			roots = append(roots, path)
		}
	}
	if len(roots) > 0 {
		return roots, nil
	}
	entries, err := os.ReadDir(s.cfg.MediaRoot)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(s.cfg.MediaRoot, entry.Name())
		if !s.skipPath(path) {
			roots = append(roots, path)
		}
	}
	return roots, nil
}

func (s *Scanner) skipPath(path string) bool {
	rel := s.rel(path)
	if rel == "." {
		return false
	}
	cleanRel := filepath.ToSlash(filepath.Clean(rel))
	outputRel, err := filepath.Rel(s.cfg.MediaRoot, s.cfg.Output)
	if err == nil {
		outputRel = filepath.ToSlash(filepath.Clean(outputRel))
		if cleanRel == outputRel || strings.HasPrefix(cleanRel, outputRel+"/") {
			return true
		}
	}
	for _, excluded := range s.cfg.MediaExcludeDirs {
		excluded = filepath.ToSlash(filepath.Clean(strings.TrimSpace(excluded)))
		if excluded == "." || excluded == "" {
			continue
		}
		if cleanRel == excluded || strings.HasPrefix(cleanRel, excluded+"/") {
			return true
		}
	}
	return strings.HasPrefix(filepath.Base(path), ".")
}

func (s *Scanner) rel(path string) string {
	rel, err := filepath.Rel(s.cfg.MediaRoot, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (s *Scanner) readCache() (*Index, error) {
	content, err := os.ReadFile(s.cfg.ScanCacheFile)
	if err != nil {
		return nil, err
	}
	idx := NewIndex(s.cfg.MediaRoot)
	if err := json.Unmarshal(content, idx); err != nil {
		return nil, err
	}
	if idx.Items == nil {
		idx.Items = map[string]Item{}
	}
	if idx.Dirs == nil {
		idx.Dirs = map[string]DirectoryState{}
	}
	return idx, nil
}

func (s *Scanner) writeCache(idx *Index) error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.ScanCacheFile), 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.cfg.ScanCacheFile, content, 0644)
}

func firstRelated(files []RelatedFile, kind RelatedKind) *RelatedFile {
	for _, file := range files {
		if file.Kind == kind {
			f := file
			return &f
		}
	}
	return nil
}

func filterRelated(files []RelatedFile, kind RelatedKind) []RelatedFile {
	out := make([]RelatedFile, 0)
	for _, file := range files {
		if file.Kind == kind {
			out = append(out, file)
		}
	}
	return out
}

func containsRelated(files []RelatedFile, path string, kind RelatedKind) bool {
	for _, file := range files {
		if file.Path == path && file.Kind == kind {
			return true
		}
	}
	return false
}

func cloneIndex(idx *Index) *Index {
	if idx == nil {
		return nil
	}
	next := &Index{
		Version:     idx.Version,
		MediaRoot:   idx.MediaRoot,
		GeneratedAt: idx.GeneratedAt,
		Items:       make(map[string]Item, len(idx.Items)),
		Dirs:        make(map[string]DirectoryState, len(idx.Dirs)),
	}
	for key, item := range idx.Items {
		next.Items[key] = item
	}
	for key, dir := range idx.Dirs {
		next.Dirs[key] = dir
	}
	return next
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func newScanSession() *scanSession {
	return &scanSession{
		dirs:  map[string]cachedDir{},
		files: map[string]cachedFile{},
	}
}

func (session *scanSession) readDir(dir string) ([]os.DirEntry, error) {
	if session == nil {
		return os.ReadDir(dir)
	}
	if cached, ok := session.dirs[dir]; ok {
		return cached.entries, cached.err
	}
	entries, err := os.ReadDir(dir)
	session.dirs[dir] = cachedDir{entries: entries, err: err}
	return entries, err
}

func (session *scanSession) stat(path string) (os.FileInfo, error) {
	if session == nil {
		return os.Stat(path)
	}
	if cached, ok := session.files[path]; ok {
		return cached.info, cached.err
	}
	info, err := os.Stat(path)
	session.files[path] = cachedFile{info: info, err: err}
	return info, err
}

func (s *Scanner) updateScanProgress(items int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		s.status.Items = items
	}
}

func ValidateUnderRoot(root, path string) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, fmt.Sprintf("..%c", os.PathSeparator)) {
		return fmt.Errorf("%s is outside %s", path, root)
	}
	return nil
}
