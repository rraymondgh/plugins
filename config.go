package plugins

import "time"

func TestConfig() *Config {
	return &Config{
		Enabled:      true,
		Folder:       "testdata",
		PluginConfig: nil,
	}
}

type Config struct {
	Enabled                     bool
	Folder                      string
	CacheSize                   string
	PluginConfig                map[string]map[string]string
	CacheFolder                 string
	DevPluginCompilationTimeout time.Duration
}

func NewDefaultConfig() Config {
	return Config{
		Enabled:      false,
		Folder:       "/plugins/plugins",
		CacheFolder:  "/plugins/cache",
		PluginConfig: nil,
	}
}
