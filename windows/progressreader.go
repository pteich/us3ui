package windows

import "io"

// ProgressReader wraps an io.Reader to track progress
type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	BytesRead  int64
	OnProgress func(int64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.BytesRead += int64(n)
	if pr.OnProgress != nil {
		pr.OnProgress(pr.BytesRead)
	}
	return n, err
}
