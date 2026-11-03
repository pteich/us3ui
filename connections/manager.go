package connections

import (
	"fmt"

	"github.com/pteich/us3ui/config"
)

type List []config.S3Config

type Manager struct {
	cfg         *config.Config
	connections List
	selected    int
}

func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:         cfg,
		connections: cfg.Settings.Connections,
		selected:    -1,
	}
}

func (m *Manager) SetSelected(index int) {
	m.selected = index
}

func (m *Manager) GetSelected() int {
	return m.selected
}

func (m *Manager) Count() int {
	return len(m.connections)
}

func (m *Manager) Get(index int) config.S3Config {
	return m.connections[index]
}

// Index returns the position of the connection named name, -1 if none.
func (m *Manager) Index(name string) int {
	for i, c := range m.connections {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func (m *Manager) Add(cfg config.S3Config) {
	found := false
	for i, c := range m.connections {
		if c.Name == cfg.Name {
			m.connections[i] = cfg
			found = true
			break
		}
	}

	if !found {
		m.connections = append(m.connections, cfg)
	}
}

func (m *Manager) Remove(index int) {
	if index < 0 || index >= len(m.connections) {
		return
	}
	deleteSecrets(m.connections[index].Name)
	m.connections = append(m.connections[:index], m.connections[index+1:]...)
}

// Rename replaces the connection at index with cfg, which carries the new
// name. Secrets under the old name are dropped; Save stores them anew.
func (m *Manager) Rename(index int, cfg config.S3Config) error {
	if index < 0 || index >= len(m.connections) {
		return fmt.Errorf("no connection at index %d", index)
	}
	for i, c := range m.connections {
		if i != index && c.Name == cfg.Name {
			return fmt.Errorf("a connection named %q already exists", cfg.Name)
		}
	}
	deleteSecrets(m.connections[index].Name)
	m.connections[index] = cfg
	return nil
}

func deleteSecrets(name string) {
	if name == "" || name == config.Transient {
		return
	}
	_ = config.DeleteSecret(name)
	_ = config.DeleteKeyPassphrase(name)
	_ = config.DeleteProxyPassword(name)
}

func (m *Manager) Save() error {
	m.cfg.Settings.Connections = m.connections
	return m.cfg.Save()
}
