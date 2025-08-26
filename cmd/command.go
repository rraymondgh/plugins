package cmd

import (
	"github.com/rraymondgh/plugins"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

type params struct {
	Logger *zap.Logger
	Config *plugins.Config
}

func CLI(log *zap.Logger, config plugins.Config) *cli.Command {
	p := params{
		Logger: log,
		Config: &config,
	}

	undo := zap.ReplaceGlobals(p.Logger.Named("plugincmd"))
	defer undo()

	return &cli.Command{
		Name: "plugin",
		Subcommands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "List installed plugins",
				UsageText: "List all installed plugins with their metadata",
				Action:    p.pluginList,
			},
			{
				Name:      "info",
				Usage:     "info [pluginPackage|pluginName]",
				UsageText: "Show detailed information about a plugin package (.ndp file) or an installed plugin",
				Args:      true,
				Action:    p.pluginInfo,
			},
			{
				Name:      "install",
				Usage:     "install [pluginPackage]",
				UsageText: "Install a Plugin Package (.ndp) file",
				Args:      true,
				Action:    p.pluginInstall,
			},
			{
				Name:      "remove",
				Usage:     "remove [pluginName]",
				UsageText: "Remove a plugin by name",
				Args:      true,
				Action:    p.pluginRemove,
			},
			{
				Name:      "update",
				Usage:     "update [pluginPackage]",
				UsageText: "Update an installed plugin with a new version from a .ndp file",
				Args:      true,
				Action:    p.pluginUpdate,
			},
			{
				Name:      "refresh",
				Usage:     "refresh [pluginName]",
				UsageText: "Reload and recompile a plugin without needing to restart",
				Args:      true,
				Action:    p.pluginRefresh,
			},
			{
				Name:  "dev",
				Usage: "dev [folder_path]",
				UsageText: "Create a symlink from a plugin development folder " +
					"to the plugins directory for easier development",
				Args:   true,
				Action: p.pluginDev,
			},
		},
	}
}
