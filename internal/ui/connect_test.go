package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/pteich/us3ui/config"
)

func TestConnectFormSFTPFields(t *testing.T) {
	a := testApp(t)
	a.conn.open = true
	a.conn.fill(config.S3Config{Type: config.TypeSFTP, Name: "srv", Endpoint: "example.com", AccessKey: "peter"})
	tt := ui.NewTester(a.view, 1000, 600)

	for _, want := range []string{"Username", "Password", "Key File", "Key Passphrase", "Start Directory", "Host"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	for _, hidden := range []string{"Manage Buckets", "Region", "Bucket", "Access Key"} {
		if tt.HasText(hidden) {
			t.Errorf("%q should be hidden for SFTP connections: %q", hidden, tt.Texts())
		}
	}
}

func TestConnectFormDefaultsToS3(t *testing.T) {
	a := testApp(t)
	a.conn.open = true
	a.conn.clear()
	tt := ui.NewTester(a.view, 1000, 600)
	for _, want := range []string{"Endpoint", "Access Key", "Secret Key", "Manage Buckets"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestFilesViewHidesLinkWithoutPresigner(t *testing.T) {
	a := testApp(t)
	a.linker = nil
	tt := ui.NewTester(a.view, 1000, 600)
	if !tt.HasText("Download") {
		t.Fatal("files view did not render")
	}
	if tt.HasText("Link") {
		t.Error("Link button shown for a store without presign support")
	}
}

func TestConnTypeHelpers(t *testing.T) {
	if got := (config.S3Config{}).ConnType(); got != config.TypeS3 {
		t.Errorf("empty type = %q, want s3", got)
	}
	if got := (config.S3Config{Type: config.TypeSFTP}).ConnType(); got != config.TypeSFTP {
		t.Errorf("explicit type = %q, want sftp", got)
	}
	for _, ct := range connTypes {
		if connTypeValue(connTypeLabel(ct.value)) != ct.value {
			t.Errorf("label round trip failed for %q", ct.value)
		}
	}
}
