package connections

import (
	"testing"

	"github.com/pteich/us3ui/config"
)

func makeManager(conns []config.S3Config) *Manager {
	cfg := &config.Config{
		Settings: config.Settings{Connections: conns},
	}
	return NewManager(cfg)
}

func TestManagerAddAppends(t *testing.T) {
	m := makeManager([]config.S3Config{
		{Name: "existing", Endpoint: "a.example.com"},
	})

	initial := m.Count()
	m.Add(config.S3Config{Name: "new", Endpoint: "b.example.com"})

	if m.Count() != initial+1 {
		t.Errorf("Count() = %d, want %d", m.Count(), initial+1)
	}
	got := m.Get(m.Count() - 1)
	if got.Name != "new" {
		t.Errorf("last connection Name = %q, want %q", got.Name, "new")
	}
}

func TestManagerAddUpdatesInPlace(t *testing.T) {
	m := makeManager([]config.S3Config{
		{Name: "prod", Endpoint: "old.example.com"},
	})

	initial := m.Count()
	m.Add(config.S3Config{Name: "prod", Endpoint: "new.example.com"})

	if m.Count() != initial {
		t.Errorf("Count() = %d after update, want %d (no append)", m.Count(), initial)
	}
	got := m.Get(0)
	if got.Endpoint != "new.example.com" {
		t.Errorf("Get(0).Endpoint = %q, want %q", got.Endpoint, "new.example.com")
	}
}

func TestManagerRemoveValid(t *testing.T) {
	m := makeManager([]config.S3Config{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	})

	m.Remove(1) // remove "b"

	if m.Count() != 2 {
		t.Errorf("Count() = %d after Remove(1), want 2", m.Count())
	}
	if m.Get(0).Name != "a" || m.Get(1).Name != "c" {
		t.Errorf("unexpected connections after Remove: %v, %v", m.Get(0).Name, m.Get(1).Name)
	}
}

func TestManagerRemoveOutOfRange(t *testing.T) {
	m := makeManager([]config.S3Config{
		{Name: "a"},
		{Name: "b"},
	})

	// Both should be no-ops
	m.Remove(-1)
	m.Remove(m.Count() + 5)

	if m.Count() != 2 {
		t.Errorf("Count() = %d after out-of-range Remove, want 2", m.Count())
	}
}
