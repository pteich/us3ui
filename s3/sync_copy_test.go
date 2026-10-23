package s3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pteich/us3ui/config"
)

func TestCopyOnePreservesSourceContentType(t *testing.T) {
	const content = "<html>test</html>"
	for _, contentType := range []string{"text/html; charset=utf-8", "image/svg+xml"} {
		t.Run(contentType, func(t *testing.T) {
			srcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Content-Length", fmt.Sprint(len(content)))
				w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
				w.Header().Set("ETag", `"abc123"`)
				if r.Method != http.MethodHead {
					fmt.Fprint(w, content)
				}
			}))
			defer srcServer.Close()
			type upload struct{ contentType, body string }
			got := make(chan upload, 1)
			dstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				got <- upload{r.Header.Get("Content-Type"), string(body)}
				w.Header().Set("ETag", `"abc123"`)
			}))
			defer dstServer.Close()
			service := func(server *httptest.Server) *Service {
				t.Helper()
				svc, err := New(config.S3Config{Endpoint: strings.TrimPrefix(server.URL, "http://"), Bucket: "bucket", Region: "us-east-1"})
				if err != nil {
					t.Fatal(err)
				}
				return svc
			}
			a := SyncAction{SrcKey: "x", DstKey: "x", Size: int64(len(content)), ContentType: "application/octet-stream"}
			if err := copyOne(context.Background(), service(srcServer), service(dstServer), a, nil); err != nil {
				t.Fatal(err)
			}
			u := <-got
			if u.contentType != contentType || u.body != content {
				t.Errorf("upload = %q, %q; want %q, %q", u.contentType, u.body, contentType, content)
			}
		})
	}
}
