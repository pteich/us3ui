package storage

import (
	"slices"
	"sort"
	"strings"
	"sync"
)

// WalkLister serves the ListObjectsBatch contract for stores that list by
// walking a directory tree (SFTP, FTP): the full walk is cached per prefix,
// fresh listings (startAfter == "") re-walk, pagination (Load More, startAfter
// != "") serves the cached walk. Mutations call Invalidate.
type WalkLister struct {
	mu      sync.Mutex
	prefix  string
	objects []ObjectInfo
}

// Page lists up to batchSize objects sorted by key under prefix, starting
// after startAfter, running walk only when the cache cannot serve the request.
func (w *WalkLister) Page(startAfter, prefix string, batchSize int, walk func() ([]ObjectInfo, error)) ([]ObjectInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if startAfter == "" || w.prefix != prefix || w.objects == nil {
		objects, err := walk()
		if err != nil {
			return nil, err
		}
		slices.SortFunc(objects, func(a, b ObjectInfo) int { return strings.Compare(a.Key, b.Key) })
		w.prefix, w.objects = prefix, objects
	}

	all := w.objects
	start := 0
	if startAfter != "" {
		start = sort.Search(len(all), func(i int) bool { return all[i].Key > startAfter })
	}
	if start >= len(all) {
		return nil, nil
	}
	end := min(start+batchSize, len(all))
	return all[start:end], nil
}

// Invalidate drops the cached walk so the next fresh listing re-walks.
func (w *WalkLister) Invalidate() {
	w.mu.Lock()
	w.objects = nil
	w.mu.Unlock()
}
