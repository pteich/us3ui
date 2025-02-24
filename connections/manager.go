package connections

import (
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

func (m *Manager) Add(cfg config.S3Config) {
	m.connections = append(m.connections, cfg)
}

func (m *Manager) Remove(index int) {
	if index < 0 || index >= len(m.connections) {
		return
	}
	m.connections = append(m.connections[:index], m.connections[index+1:]...)
}

func (m *Manager) Save() error {
	m.cfg.Settings.Connections = m.connections
	return m.cfg.Save()
}
