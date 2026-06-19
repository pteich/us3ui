package windows

import (
	"testing"

	minio "github.com/minio/minio-go/v7"
)

func makeObjects(keys ...string) []minio.ObjectInfo {
	objs := make([]minio.ObjectInfo, len(keys))
	for i, k := range keys {
		objs[i] = minio.ObjectInfo{Key: k}
	}
	return objs
}

func TestFilterObjectsNoFilter(t *testing.T) {
	objs := makeObjects("logs/app.log", "data/report.csv", "README.md")
	fm := &FileManager{
		allObjects:     objs,
		searchTerm:     "",
		selectedPrefix: "all",
	}

	got := fm.filterObjectsLocked()
	if len(got) != len(objs) {
		t.Errorf("filterObjectsLocked() returned %d objects, want %d", len(got), len(objs))
	}
}

func TestFilterObjectsSearchOnly(t *testing.T) {
	objs := makeObjects("logs/app.log", "data/report.csv", "README.md", "LOGS/system.log")
	fm := &FileManager{
		allObjects:     objs,
		searchTerm:     "log", // case-insensitive
		selectedPrefix: "all",
	}

	got := fm.filterObjectsLocked()
	// "logs/app.log" matches ("log" in "logs/app.log")
	// "LOGS/system.log" matches (case-insensitive)
	// "data/report.csv" does not match
	// "README.md" does not match
	if len(got) != 2 {
		t.Errorf("filterObjectsLocked() returned %d objects, want 2; keys: %v", len(got), keysOf(got))
	}
}

func TestFilterObjectsPrefixOnly(t *testing.T) {
	objs := makeObjects("logs/app.log", "logs/system.log", "data/report.csv", "README.md")
	fm := &FileManager{
		allObjects:     objs,
		searchTerm:     "",
		selectedPrefix: "logs",
	}

	got := fm.filterObjectsLocked()
	if len(got) != 2 {
		t.Errorf("filterObjectsLocked() returned %d objects, want 2; keys: %v", len(got), keysOf(got))
	}
	for _, obj := range got {
		if len(obj.Key) < 4 || obj.Key[:4] != "logs" {
			t.Errorf("unexpected key %q — does not start with 'logs'", obj.Key)
		}
	}
}

func keysOf(objs []minio.ObjectInfo) []string {
	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = o.Key
	}
	return keys
}
