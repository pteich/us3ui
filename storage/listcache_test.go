package storage

import (
	"errors"
	"testing"
)

func TestWalkListerPagesSortedWalk(t *testing.T) {
	var w WalkLister
	walks := 0
	walk := func() ([]ObjectInfo, error) {
		walks++
		return []ObjectInfo{
			{Key: "z.txt"}, {Key: "a.txt"}, {Key: "m/x.log"},
		}, nil
	}

	page := func(startAfter string, n int) []string {
		objs, err := w.Page(startAfter, "", n, walk)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(objs))
		for i, o := range objs {
			out[i] = o.Key
		}
		return out
	}

	if got := page("", 2); len(got) != 2 || got[0] != "a.txt" || got[1] != "m/x.log" {
		t.Errorf("page 1 = %v, want sorted [a.txt m/x.log]", got)
	}
	if got := page("m/x.log", 2); len(got) != 1 || got[0] != "z.txt" {
		t.Errorf("page 2 = %v, want [z.txt]", got)
	}
	if got := page("z.txt", 2); len(got) != 0 {
		t.Errorf("page 3 = %v, want empty", got)
	}
	if walks != 1 {
		t.Errorf("walk ran %d times, want 1 (pagination serves the cache)", walks)
	}
}

func TestWalkListerFreshListingRewalks(t *testing.T) {
	var w WalkLister
	walks := 0
	walk := func() ([]ObjectInfo, error) {
		walks++
		return []ObjectInfo{{Key: "a.txt"}}, nil
	}
	for range 3 {
		if _, err := w.Page("", "", 10, walk); err != nil {
			t.Fatal(err)
		}
	}
	if walks != 3 {
		t.Errorf("fresh listings walked %d times, want 3", walks)
	}
}

func TestWalkListerPrefixChangeAndInvalidate(t *testing.T) {
	var w WalkLister
	listed := ""
	walk := func() ([]ObjectInfo, error) {
		listed = "called"
		return []ObjectInfo{{Key: "x"}}, nil
	}
	if _, err := w.Page("", "a", 10, walk); err != nil {
		t.Fatal(err)
	}
	listed = ""
	// Same prefix, pagination: no walk.
	if _, err := w.Page("x", "a", 10, walk); err != nil {
		t.Fatal(err)
	}
	if listed != "" {
		t.Error("pagination should serve the cache")
	}
	// Different prefix: walk.
	if _, err := w.Page("", "b", 10, walk); err != nil {
		t.Fatal(err)
	}
	if listed != "called" {
		t.Error("prefix change should re-walk")
	}
	// Same prefix after mutation: walk.
	listed = ""
	w.Invalidate()
	if _, err := w.Page("", "b", 10, walk); err != nil {
		t.Fatal(err)
	}
	if listed != "called" {
		t.Error("invalidate should force a re-walk")
	}
}

func TestWalkListerWalkErrorPropagates(t *testing.T) {
	var w WalkLister
	want := errors.New("boom")
	if _, err := w.Page("", "", 10, func() ([]ObjectInfo, error) { return nil, want }); err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
}
