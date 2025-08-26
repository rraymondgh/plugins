package cmd

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rraymondgh/plugins"
	"github.com/rraymondgh/plugins/core/metrics"
	"github.com/rraymondgh/plugins/schema"
	"github.com/rraymondgh/plugins/utils"
	"github.com/rraymondgh/plugins/utils/slice"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zapio"
)

const (
	pluginPackageExtension = ".ndp"
	pluginDirPermissions   = 0o700
	pluginFilePermissions  = 0o600
)

// Validation helpers

func validatePluginPackageFile(path string) error {
	if !utils.FileExists(path) {
		return fmt.Errorf("plugin package not found: %s", path)
	}

	if filepath.Ext(path) != pluginPackageExtension {
		return fmt.Errorf(
			"not a valid plugin package: %s (expected %s extension)",
			path,
			pluginPackageExtension,
		)
	}

	return nil
}

func validatePluginDirectory(pluginsDir, pluginName string) (string, error) {
	pluginDir := filepath.Join(pluginsDir, pluginName)
	if !utils.FileExists(pluginDir) {
		return "", fmt.Errorf("plugin not found: %s (path: %s)", pluginName, pluginDir)
	}

	return pluginDir, nil
}

func resolvePluginPath(pluginDir string) (resolvedPath string, isSymlink bool, err error) {
	// Check if it's a directory or a symlink
	lstat, err := os.Lstat(pluginDir)
	if err != nil {
		return "", false, fmt.Errorf("failed to stat plugin: %w", err)
	}

	isSymlink = lstat.Mode()&os.ModeSymlink != 0

	if isSymlink {
		// Resolve the symlink target
		targetDir, err := os.Readlink(pluginDir)
		if err != nil {
			return "", true, fmt.Errorf("failed to resolve symlink: %w", err)
		}

		// If target is a relative path, make it absolute
		if !filepath.IsAbs(targetDir) {
			targetDir = filepath.Join(filepath.Dir(pluginDir), targetDir)
		}

		// Verify the target exists and is a directory
		targetInfo, err := os.Stat(targetDir)
		if err != nil {
			return "", true, fmt.Errorf("failed to access symlink target %s: %w", targetDir, err)
		}

		if !targetInfo.IsDir() {
			return "", true, fmt.Errorf("symlink target is not a directory: %s", targetDir)
		}

		return targetDir, true, nil
	} else if !lstat.IsDir() {
		return "", false, fmt.Errorf("not a valid plugin directory: %s", pluginDir)
	}

	return pluginDir, false, nil
}

// Package handling helpers

func loadAndValidatePackage(ndpPath string) (*plugins.PluginPackage, error) {
	if err := validatePluginPackageFile(ndpPath); err != nil {
		return nil, err
	}

	pkg, err := plugins.LoadPackage(ndpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin package: %w", err)
	}

	return pkg, nil
}

func (p params) extractAndSetupPlugin(ndpPath, targetDir string) error {
	if err := plugins.ExtractPackage(ndpPath, targetDir); err != nil {
		return fmt.Errorf("failed to extract plugin package: %w", err)
	}

	p.ensurePluginDirPermissions(targetDir)

	return nil
}

// Display helpers

func (p params) displayPluginTableRow(w *tabwriter.Writer, discovery plugins.PluginDiscoveryEntry) {
	if discovery.Error != nil {
		// Handle global errors (like directory read failure)
		if discovery.ID == "" {
			p.Logger.Error(
				"Failed to read plugins directory",
				zap.String("folder", p.Config.Folder),
				zap.Error(discovery.Error),
			)

			return
		}
		// Handle individual plugin errors - show them in the table
		fmt.Fprintf(w, "%s\tERROR\tERROR\tERROR\tERROR\t%v\n", discovery.ID, discovery.Error)

		return
	}

	// Mark symlinks with an indicator
	nameDisplay := discovery.Manifest.Name
	if discovery.IsSymlink {
		nameDisplay += " (dev)"
	}

	// Convert capabilities to strings
	capabilities := slice.Map(
		discovery.Manifest.Capabilities,
		func(capElem schema.PluginManifestCapabilitiesElem) string {
			return string(capElem)
		},
	)

	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
		discovery.ID,
		nameDisplay,
		cmp.Or(discovery.Manifest.Author, "-"),
		cmp.Or(discovery.Manifest.Version, "-"),
		strings.Join(capabilities, ", "),
		cmp.Or(discovery.Manifest.Description, "-"))
}

func (p params) displayTypedPermissions(permissions schema.PluginManifestPermissions, indent string) {
	if permissions.Http != nil {
		fmt.Fprintf(p.ioOut(), "%shttp:\n", indent)
		fmt.Fprintf(p.ioOut(), "%s  Reason: %s\n", indent, permissions.Http.Reason)
		fmt.Fprintf(p.ioOut(), "%s  Allow Local Network: %t\n", indent, permissions.Http.AllowLocalNetwork)
		fmt.Fprintf(p.ioOut(), "%s  Allowed URLs:\n", indent)

		for urlPattern, methodEnums := range permissions.Http.AllowedUrls {
			methods := make([]string, len(methodEnums))
			for i, methodEnum := range methodEnums {
				methods[i] = string(methodEnum)
			}

			fmt.Fprintf(p.ioOut(), "%s    %s: [%s]\n", indent, urlPattern, strings.Join(methods, ", "))
		}

		fmt.Fprintln(p.ioOut())
	}

	if permissions.Config != nil {
		fmt.Fprintf(p.ioOut(), "%sconfig:\n", indent)
		fmt.Fprintf(p.ioOut(), "%s  Reason: %s\n", indent, permissions.Config.Reason)
		fmt.Fprintln(p.ioOut())
	}

	if permissions.Websocket != nil {
		fmt.Fprintf(p.ioOut(), "%swebsocket:\n", indent)
		fmt.Fprintf(p.ioOut(), "%s  Reason: %s\n", indent, permissions.Websocket.Reason)
		fmt.Fprintf(p.ioOut(), "%s  Allow Local Network: %t\n", indent, permissions.Websocket.AllowLocalNetwork)
		fmt.Fprintf(p.ioOut(), "%s  Allowed URLs: [%s]\n",
			indent, strings.Join(permissions.Websocket.AllowedUrls, ", "))
		fmt.Fprintln(p.ioOut())
	}
}

func (p params) displayPluginDetails(
	manifest *schema.PluginManifest,
	fileInfo *pluginFileInfo,
	permInfo *pluginPermissionInfo,
) {
	fmt.Fprintln(p.ioOut(), "\nPlugin Information:")
	fmt.Fprintf(p.ioOut(), "  Name:        %s\n", manifest.Name)
	fmt.Fprintf(p.ioOut(), "  Author:      %s\n", manifest.Author)
	fmt.Fprintf(p.ioOut(), "  Version:     %s\n", manifest.Version)
	fmt.Fprintf(p.ioOut(), "  Description: %s\n", manifest.Description)

	capabilities := make([]string, len(manifest.Capabilities))
	for i, cap := range manifest.Capabilities {
		capabilities[i] = string(cap)
	}

	fmt.Fprintf(p.ioOut(), "  Capabilities:    %s\n", strings.Join(capabilities, ", "))

	// Display manifest permissions using the typed permissions
	fmt.Fprintln(p.ioOut(), "  Required Permissions:")
	p.displayTypedPermissions(manifest.Permissions, "    ")

	// Print file information if available
	if fileInfo != nil {
		fmt.Fprintln(p.ioOut(), "Package Information:")
		fmt.Fprintf(p.ioOut(), "  File:        %s\n", fileInfo.path)
		fmt.Fprintf(p.ioOut(), "  Size:        %d bytes (%.2f KB)\n",
			fileInfo.size, float64(fileInfo.size)/1024)
		fmt.Fprintf(p.ioOut(), "  SHA-256:     %s\n", fileInfo.hash)
		fmt.Fprintf(p.ioOut(), "  Modified:    %s\n", fileInfo.modTime.Format(time.RFC3339))
	}

	// Print file permissions information if available
	if permInfo != nil {
		fmt.Fprintln(p.ioOut(), "File Permissions:")
		fmt.Fprintf(p.ioOut(), "  Plugin Directory: %s (%s)\n", permInfo.dirPath, permInfo.dirMode)

		if permInfo.isSymlink {
			fmt.Fprintf(p.ioOut(), "  Symlink Target:   %s (%s)\n",
				permInfo.targetPath, permInfo.targetMode)
		}

		fmt.Fprintf(p.ioOut(), "  Manifest File:    %s\n", permInfo.manifestMode)

		if permInfo.wasmMode != "" {
			fmt.Fprintf(p.ioOut(), "  WASM File:        %s\n", permInfo.wasmMode)
		}
	}
}

type pluginFileInfo struct {
	path    string
	size    int64
	hash    string
	modTime time.Time
}

type pluginPermissionInfo struct {
	dirPath      string
	dirMode      string
	isSymlink    bool
	targetPath   string
	targetMode   string
	manifestMode string
	wasmMode     string
}

func (p params) getFileInfo(path string) *pluginFileInfo {
	fileInfo, err := os.Stat(path)
	if err != nil {
		p.Logger.Error("Failed to get file information", zap.Error(err))
		return nil
	}

	return &pluginFileInfo{
		path:    path,
		size:    fileInfo.Size(),
		hash:    p.calculateSHA256(path),
		modTime: fileInfo.ModTime(),
	}
}

func (p params) getPermissionInfo(pluginDir string) *pluginPermissionInfo {
	// Get plugin directory permissions
	dirInfo, err := os.Lstat(pluginDir)
	if err != nil {
		p.Logger.Error("Failed to get plugin directory permissions", zap.Error(err))
		return nil
	}

	permInfo := &pluginPermissionInfo{
		dirPath: pluginDir,
		dirMode: dirInfo.Mode().String(),
	}

	// Check if it's a symlink
	if dirInfo.Mode()&os.ModeSymlink != 0 {
		permInfo.isSymlink = true

		// Get target path and permissions
		targetPath, err := os.Readlink(pluginDir)
		if err == nil {
			if !filepath.IsAbs(targetPath) {
				targetPath = filepath.Join(filepath.Dir(pluginDir), targetPath)
			}

			permInfo.targetPath = targetPath

			if targetInfo, err := os.Stat(targetPath); err == nil {
				permInfo.targetMode = targetInfo.Mode().String()
			}
		}
	}

	// Get manifest file permissions
	manifestPath := filepath.Join(pluginDir, "manifest.json")
	if manifestInfo, err := os.Stat(manifestPath); err == nil {
		permInfo.manifestMode = manifestInfo.Mode().String()
	}

	// Get WASM file permissions (look for .wasm files)
	entries, err := os.ReadDir(pluginDir)
	if err == nil {
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".wasm" {
				wasmPath := filepath.Join(pluginDir, entry.Name())
				if wasmInfo, err := os.Stat(wasmPath); err == nil {
					permInfo.wasmMode = wasmInfo.Mode().String()
					break // Just show the first WASM file found
				}
			}
		}
	}

	return permInfo
}

// Command implementations

func (p params) pluginList(_ *cli.Context) error {
	discoveries := plugins.DiscoverPlugins(p.Config.Folder)

	w := tabwriter.NewWriter(p.ioOut(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tAUTHOR\tVERSION\tCAPABILITIES\tDESCRIPTION")

	for _, discovery := range discoveries {
		p.displayPluginTableRow(w, discovery)
	}

	w.Flush()

	return nil
}

func (p params) pluginInfo(ctx *cli.Context) error {
	path := ctx.Args().Get(0)
	pluginsDir := p.Config.Folder

	var manifest *schema.PluginManifest

	var fileInfo *pluginFileInfo

	var permInfo *pluginPermissionInfo

	if filepath.Ext(path) == pluginPackageExtension { // It's a package file
		pkg, err := loadAndValidatePackage(path)
		if err != nil {
			p.Logger.Fatal("Failed to load plugin package", zap.Error(err))
		}

		manifest = pkg.Manifest
		fileInfo = p.getFileInfo(path)
	} else { // No permission info for package files
		// It's a plugin name
		pluginDir, err := validatePluginDirectory(pluginsDir, path)
		if err != nil {
			p.Logger.Fatal("Plugin validation failed", zap.Error(err))
		}

		manifest, err = plugins.LoadManifest(pluginDir)
		if err != nil {
			p.Logger.Fatal("Failed to load plugin manifest", zap.Error(err))
		}

		// Get permission info for installed plugins
		permInfo = p.getPermissionInfo(pluginDir)
	}

	p.displayPluginDetails(manifest, fileInfo, permInfo)

	return nil
}

func (p params) pluginInstall(ctx *cli.Context) error {
	ndpPath := ctx.Args().Get(0)
	pluginsDir := p.Config.Folder

	pkg, err := loadAndValidatePackage(ndpPath)
	if err != nil {
		p.Logger.Fatal("Package validation failed", zap.Error(err))
	}

	// Create target directory based on plugin name
	targetDir := filepath.Join(pluginsDir, pkg.Manifest.Name)

	// Check if plugin already exists
	if utils.FileExists(targetDir) {
		p.Logger.Fatal(
			"Plugin already installed",
			zap.String("name", pkg.Manifest.Name),
			zap.String("path", targetDir),
			zap.String("use", "plugin_nav2 plugin update"),
		)
	}

	if err := p.extractAndSetupPlugin(ndpPath, targetDir); err != nil {
		p.Logger.Fatal("Plugin installation failed", zap.Error(err))
	}

	fmt.Fprintf(p.ioOut(), "Plugin '%s' v%s installed successfully\n", pkg.Manifest.Name, pkg.Manifest.Version)

	return nil
}

func (p params) pluginRemove(ctx *cli.Context) error {
	pluginName := ctx.Args().Get(0)
	pluginsDir := p.Config.Folder

	pluginDir, err := validatePluginDirectory(pluginsDir, pluginName)
	if err != nil {
		p.Logger.Fatal("Plugin validation failed", zap.Error(err))
	}

	_, isSymlink, err := resolvePluginPath(pluginDir)
	if err != nil {
		p.Logger.Fatal("Failed to resolve plugin path", zap.Error(err))
	}

	if isSymlink {
		// For symlinked plugins (dev mode), just remove the symlink
		if err := os.Remove(pluginDir); err != nil {
			p.Logger.Fatal(
				"Failed to remove plugin symlink",
				zap.String("name", pluginName),
				zap.Error(err),
			)
		}

		fmt.Fprintf(p.ioOut(),
			"Development plugin symlink '%s' removed successfully (target directory preserved)\n",
			pluginName,
		)
	} else {
		// For regular plugins, remove the entire directory
		if err := os.RemoveAll(pluginDir); err != nil {
			p.Logger.Fatal("Failed to remove plugin directory", zap.String("name", pluginName), zap.Error(err))
		}

		fmt.Fprintf(p.ioOut(), "Plugin '%s' removed successfully\n", pluginName)
	}

	return nil
}

func (p params) pluginUpdate(ctx *cli.Context) error {
	ndpPath := ctx.Args().Get(0)
	pluginsDir := p.Config.Folder

	pkg, err := loadAndValidatePackage(ndpPath)
	if err != nil {
		p.Logger.Fatal("Package validation failed", zap.Error(err))
	}

	// Check if plugin exists
	targetDir := filepath.Join(pluginsDir, pkg.Manifest.Name)
	if !utils.FileExists(targetDir) {
		p.Logger.Fatal("Plugin not found", zap.String("name", pkg.Manifest.Name), zap.String("path", targetDir),
			zap.String("use", "plugin_nav2 plugin install"))
	}

	// Create a backup of the existing plugin
	backupDir := targetDir + ".bak." + time.Now().Format("20060102150405")
	if err := os.Rename(targetDir, backupDir); err != nil {
		p.Logger.Fatal("Failed to backup existing plugin", zap.Error(err))
	}

	// Extract the new package
	if err := p.extractAndSetupPlugin(ndpPath, targetDir); err != nil {
		// Restore backup if extraction failed
		os.RemoveAll(targetDir)
		_ = os.Rename(backupDir, targetDir) // Ignore error as we're already in a fatal path

		p.Logger.Fatal("Plugin update failed", zap.Error(err))
	}

	// Remove the backup
	os.RemoveAll(backupDir)

	fmt.Fprintf(p.ioOut(), "Plugin '%s' updated to v%s successfully\n", pkg.Manifest.Name, pkg.Manifest.Version)

	return nil
}

func (p params) pluginRefresh(ctx *cli.Context) error {
	pluginName := ctx.Args().Get(0)
	pluginsDir := p.Config.Folder

	pluginDir, err := validatePluginDirectory(pluginsDir, pluginName)
	if err != nil {
		p.Logger.Fatal("Plugin validation failed", zap.Error(err))
	}

	resolvedPath, isSymlink, err := resolvePluginPath(pluginDir)
	if err != nil {
		p.Logger.Fatal("Failed to resolve plugin path", zap.Error(err))
	}

	if isSymlink {
		p.Logger.Debug(
			"Processing symlinked plugin",
			zap.String("name",
				pluginName),
			zap.String("link",
				pluginDir),
			zap.String("target",
				resolvedPath),
		)
	}

	fmt.Fprintf(p.ioOut(), "Refreshing plugin '%s'...\n", pluginName)

	// Get the plugin manager and refresh
	mgr := plugins.GetManager(metrics.GetPrometheusInstance(), p.Config)
	p.Logger.Debug("Scanning plugins directory", zap.String("path", pluginsDir))
	mgr.ScanPlugins()

	p.Logger.Info("Waiting for plugin compilation to complete", zap.String("name", pluginName))

	// Wait for compilation to complete
	if err := mgr.EnsureCompiled(pluginName); err != nil {
		p.Logger.Fatal("Failed to compile refreshed plugin", zap.String("name", pluginName), zap.Error(err))
	}

	p.Logger.Info("Plugin compilation completed successfully", zap.String("name", pluginName))
	fmt.Fprintf(p.ioOut(), "Plugin '%s' refreshed successfully\n", pluginName)

	return nil
}

func (p params) pluginDev(ctx *cli.Context) error {
	sourcePath, err := filepath.Abs(ctx.Args().Get(0))
	if err != nil {
		p.Logger.Fatal("Invalid path", zap.String("path", ctx.Args().Get(0)), zap.Error(err))
	}

	pluginsDir := p.Config.Folder

	// Validate source directory and manifest
	if err := validateDevSource(sourcePath); err != nil {
		p.Logger.Fatal("Source validation failed", zap.Error(err))
	}

	// Load manifest to get plugin name
	manifest, err := plugins.LoadManifest(sourcePath)
	if err != nil {
		p.Logger.Fatal(
			"Failed to load plugin manifest",
			zap.String("path",
				filepath.Join(sourcePath, "manifest.json")),
			zap.Error(err),
		)
	}

	pluginName := cmp.Or(manifest.Name, filepath.Base(sourcePath))
	targetPath := filepath.Join(pluginsDir, pluginName)

	// Handle existing target
	if err := p.handleExistingTarget(targetPath, sourcePath); err != nil {
		p.Logger.Fatal("Failed to handle existing target", zap.Error(err))
	}

	// Create target directory if needed
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		p.Logger.Fatal(
			"Failed to create plugins directory",
			zap.String("path", filepath.Dir(targetPath)),
			zap.Error(err),
		)
	}

	// Create the symlink
	if err := os.Symlink(sourcePath, targetPath); err != nil {
		p.Logger.Fatal(
			"Failed to create symlink",
			zap.String("source", sourcePath),
			zap.String("target", targetPath),
			zap.Error(err),
		)
	}

	fmt.Fprintf(p.ioOut(), "Development symlink created: '%s' -> '%s'\n", targetPath, sourcePath)
	fmt.Fprintln(p.ioOut(), "Plugin can be refreshed with: plugin_nav2 plugin refresh", pluginName)

	return nil
}

// Utility functions

func validateDevSource(sourcePath string) error {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source folder not found: %s (%w)", sourcePath, err)
	}

	if !sourceInfo.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", sourcePath)
	}

	manifestPath := filepath.Join(sourcePath, "manifest.json")
	if !utils.FileExists(manifestPath) {
		return fmt.Errorf("source folder missing manifest.json: %s", sourcePath)
	}

	return nil
}

func (p params) handleExistingTarget(targetPath, sourcePath string) error {
	if !utils.FileExists(targetPath) {
		return nil // Nothing to handle
	}

	// Check if it's already a symlink to our source
	existingLink, err := os.Readlink(targetPath)
	if err == nil && existingLink == sourcePath {
		fmt.Fprintf(p.ioOut(), "Symlink already exists and points to the correct source\n")
		return fmt.Errorf("symlink already exists") // This will cause early return in caller
	}

	// Handle case where target exists but is not a symlink to our source
	fmt.Fprintf(p.ioOut(), "Target path '%s' already exists.\n", targetPath)
	fmt.Fprint(p.ioOut(), "Do you want to replace it? (y/N): ")

	var response string

	_, err = fmt.Scanln(&response)
	if err != nil || strings.ToLower(response) != "y" {
		if err != nil {
			p.Logger.Debug("Error reading input, assuming 'no'", zap.Error(err))
		}

		return fmt.Errorf("operation canceled")
	}

	// Remove existing target
	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Errorf("failed to remove existing target %s: %w", targetPath, err)
	}

	return nil
}

func (p params) ensurePluginDirPermissions(dir string) {
	if err := os.Chmod(dir, pluginDirPermissions); err != nil {
		p.Logger.Error("Failed to set plugin directory permissions", zap.String("dir", dir), zap.Error(err))
	}

	// Apply permissions to all files in the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		p.Logger.Error("Failed to read plugin directory", zap.String("dir", dir), zap.Error(err))
		return
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		info, err := os.Stat(path)
		if err != nil {
			p.Logger.Error("Failed to stat file", zap.String("path", path), zap.Error(err))
			continue
		}

		mode := os.FileMode(pluginFilePermissions) // Files
		if info.IsDir() {
			mode = os.FileMode(pluginDirPermissions) // Directories

			p.ensurePluginDirPermissions(path) // Recursive
		}

		if err := os.Chmod(path, mode); err != nil {
			p.Logger.Error("Failed to set file permissions", zap.String("path", path), zap.Error(err))
		}
	}
}

func (p params) calculateSHA256(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		p.Logger.Error("Failed to open file for hashing", zap.Error(err))
		return "N/A"
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		p.Logger.Error("Failed to calculate hash", zap.Error(err))
		return "N/A"
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func (p params) ioOut() io.Writer {
	return &zapio.Writer{
		Log:   p.Logger.Named("plugincmd").WithOptions(zap.WithCaller(false)),
		Level: zapcore.InfoLevel,
	}
}
