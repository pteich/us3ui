package ui

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/minio/minio-go/v7"
)

// filterObjects returns the objects under dir ("" for all of them) whose key
// contains term, ignoring case.
func filterObjects(all []minio.ObjectInfo, term, dir string) []minio.ObjectInfo {
	if term == "" && dir == "" {
		return all
	}
	term = strings.ToLower(term)
	out := make([]minio.ObjectInfo, 0, len(all)/2)
	for _, o := range all {
		if dir != "" && !strings.HasPrefix(o.Key, dir+"/") {
			continue
		}
		if term != "" && !strings.Contains(strings.ToLower(o.Key), term) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// dirTree holds the "directories" implied by object keys.
type dirTree struct {
	kids map[string][]string // parent dir -> child dirs, unsorted
	seen map[string]struct{}
}

func newDirTree() *dirTree {
	return &dirTree{kids: map[string][]string{}, seen: map[string]struct{}{}}
}

// add registers dir ("a/b/c") and all its parents.
func (t *dirTree) add(dir string) {
	parent := ""
	end := 0
	for _, part := range strings.Split(dir, "/") {
		end += len(part)
		child := dir[:end]
		end++ // include the separator in the next path
		if child == "" {
			continue // the root cannot be its own child
		}
		if _, ok := t.seen[child]; !ok {
			t.seen[child] = struct{}{}
			t.kids[parent] = append(t.kids[parent], child)
		}
		parent = child
	}
}

// children returns the sorted child dirs of dir, nil for a leaf.
func (t *dirTree) children(dir string) []string {
	k := t.kids[dir]
	if len(k) == 0 {
		return nil
	}
	s := slices.Clone(k)
	slices.Sort(s)
	return s
}

// uploadObjectName puts a local file name under the selected dir.
func uploadObjectName(dir, localPath string) string {
	name := filepath.Base(localPath)
	if dir != "" {
		return dir + "/" + name
	}
	return name
}
