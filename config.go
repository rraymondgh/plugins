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
		Enabled:      true,
		Folder:       "examples",
		PluginConfig: nil,
	}
}
