package ui

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
	"github.com/pteich/us3ui/s3"
	"github.com/pteich/us3ui/storage"
)

func TestDirTreePreservesUnusualPathsWithoutCycles(t *testing.T) {
	tr := newDirTree()
	for _, dir := range []string{"", "/logs/old", "a//b"} {
		tr.add(dir)
	}
	if got := tr.children(""); !slices.Equal(got, []string{"/logs", "a"}) {
		t.Fatalf("root children = %q, want /logs and a without a root cycle", got)
	}
	if got := tr.children("/logs"); !slices.Equal(got, []string{"/logs/old"}) {
		t.Errorf("leading slash lost: %q", got)
	}
	if got := tr.children("a/"); !slices.Equal(got, []string{"a//b"}) {
		t.Errorf("repeated slash lost: %q", got)
	}
	all := objs("/logs/old/x", "logs/old/x", "a//b/x")
	if got := keys(filterObjects(all, "", "/logs")); !slices.Equal(got, []string{"/logs/old/x"}) {
		t.Errorf("folder does not match original keys: %q", got)
	}
}

func TestSyncViewRejectsUnboundPlan(t *testing.T) {
	a := testApp(t)
	a.sync.open = true
	a.sync.plan = []storage.SyncAction{{SrcKey: "x", DstKey: "x"}}
	ui.NewTester(a.view, 1000, 600)
	if len(a.sync.plan) != 0 {
		t.Fatal("Start Copy retains a plan not bound to the selected connections")
	}
}

func TestSyncRejectsPlanAfterInputsChange(t *testing.T) {
	changes := map[string]func(*syncState){
		"source":        func(sy *syncState) { sy.src = "other" },
		"target":        func(sy *syncState) { sy.dst = "other" },
		"source prefix": func(sy *syncState) { sy.srcPrefix = "other/" },
		"target prefix": func(sy *syncState) { sy.dstPrefix = "other/" },
		"saved connection": func(sy *syncState) {
			cn := sy.conns[sy.dst]
			cn.Bucket = "another-bucket"
			sy.conns[sy.dst] = cn
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			sy := &a.sync
			sy.src, sy.dst = "source", "target"
			sy.conns = map[string]config.S3Config{
				"source": {Name: "source", Endpoint: "localhost:9000", Bucket: "source"},
				"target": {Name: "target", Endpoint: "localhost:9000", Bucket: "target"},
				"other":  {Name: "other", Endpoint: "localhost:9001", Bucket: "other"},
			}
			sy.plan = []storage.SyncAction{{SrcKey: "x", DstKey: "x"}}
			inputs := sy.inputs()
			sy.planInputs = &inputs
			sy.invalidatePlan()
			if len(sy.plan) != 1 {
				t.Fatal("unchanged dry run was discarded")
			}
			change(sy)
			a.syncStart()
			if len(sy.plan) != 0 || sy.copying || sy.cancel != nil {
				t.Fatal("copy started after its dry run inputs changed")
			}
		})
	}
}

func TestBucketsClearWhenServiceChanges(t *testing.T) {
	a := testApp(t)
	b := &a.buckets
	b.buckets = []storage.BucketInfo{{Name: "old-bucket"}}
	b.row, b.askDelete, b.addOpen = 0, true, true
	b.reset(a.svc.(storage.BucketManager))
	if len(b.buckets) != 0 || b.row != -1 || b.askDelete || b.addOpen {
		t.Fatal("old buckets or pending dialogs survived a service change")
	}
}

func TestBucketsIgnoreOldServiceResults(t *testing.T) {
	a := testApp(t)
	old := a.svc.(storage.BucketManager)
	current, err := s3.New(config.S3Config{Endpoint: "localhost:9001"})
	if err != nil {
		t.Fatal(err)
	}
	a.buckets.svc = current
	a.buckets.buckets = []storage.BucketInfo{{Name: "current"}}
	a.applyBuckets(old, []storage.BucketInfo{{Name: "old"}}, nil)
	a.applyBuckets(old, nil, errors.New("old request failed"))
	if got := a.buckets.buckets; len(got) != 1 || got[0].Name != "current" || a.msgOpen {
		t.Fatal("old service response changed current buckets or displayed an error")
	}
	a.applyBuckets(current, []storage.BucketInfo{{Name: "new"}}, nil)
	if got := a.buckets.buckets; len(got) != 1 || got[0].Name != "new" {
		t.Fatal("current service response was discarded")
	}
	a.buckets.reset(current)
	a.applyBuckets(current, nil, errors.New("current request failed"))
	if len(a.buckets.buckets) != 0 || !a.msgOpen {
		t.Fatal("failed refresh restored stale buckets or suppressed the current error")
	}
}
