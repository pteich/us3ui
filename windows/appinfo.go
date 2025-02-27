package windows

type AppInfo struct {
	Details struct {
		Version string `toml:"version"`
	} `toml:"details"`
}
