package s3

import (
	"testing"

	"github.com/minio/minio-go/v7"
)

func obj(key string, size int64) minio.ObjectInfo {
	return minio.ObjectInfo{Key: key, Size: size}
}

func TestDiffObjects(t *testing.T) {
	tests := []struct {
		name               string
		src, dst           []minio.ObjectInfo
		srcPrefix, dstPref string
		want               []SyncAction // SrcKey/DstKey/Reason checked
	}{
		{
			name: "missing and size-differs copied, identical skipped",
			src:  []minio.ObjectInfo{obj("a.txt", 10), obj("b.txt", 20), obj("c.txt", 30)},
			dst:  []minio.ObjectInfo{obj("b.txt", 99), obj("c.txt", 30)},
			want: []SyncAction{
				{SrcKey: "a.txt", DstKey: "a.txt", Reason: "missing"},
				{SrcKey: "b.txt", DstKey: "b.txt", Reason: "size differs"},
			},
		},
		{
			name:      "prefix stripped from source and applied to target",
			src:       []minio.ObjectInfo{obj("logs/2024/x.log", 5)},
			dst:       nil,
			srcPrefix: "logs/2024",
			dstPref:   "backup",
			want: []SyncAction{
				{SrcKey: "logs/2024/x.log", DstKey: "backup/x.log", Reason: "missing"},
			},
		},
		{
			name:      "trailing slashes on prefixes are normalized",
			src:       []minio.ObjectInfo{obj("src/x.log", 5)},
			dst:       nil,
			srcPrefix: "src/",
			dstPref:   "/dst/",
			want: []SyncAction{
				{SrcKey: "src/x.log", DstKey: "dst/x.log", Reason: "missing"},
			},
		},
		{
			name:      "empty target prefix keeps relative key at root",
			src:       []minio.ObjectInfo{obj("src/x.log", 5)},
			dst:       nil,
			srcPrefix: "src",
			dstPref:   "",
			want: []SyncAction{
				{SrcKey: "src/x.log", DstKey: "x.log", Reason: "missing"},
			},
		},
		{
			name: "prefix placeholder object itself is skipped",
			src:  []minio.ObjectInfo{obj("src/", 0), obj("src/x.log", 5)},
			dst:  nil, srcPrefix: "src",
			want: []SyncAction{
				{SrcKey: "src/x.log", DstKey: "x.log", Reason: "missing"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffObjects(tt.src, tt.dst, tt.srcPrefix, tt.dstPref)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d actions, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				if got[i].SrcKey != w.SrcKey || got[i].DstKey != w.DstKey || got[i].Reason != w.Reason {
					t.Errorf("action %d = {Src:%q Dst:%q Reason:%q}, want {Src:%q Dst:%q Reason:%q}",
						i, got[i].SrcKey, got[i].DstKey, got[i].Reason, w.SrcKey, w.DstKey, w.Reason)
				}
			}
		})
	}
}
