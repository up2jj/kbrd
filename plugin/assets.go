package plugin

// AssetSource identifies verified content owned by one locked plugin. Path may
// name a single file or a directory tree and must be treated as read-only.
type AssetSource struct {
	ID   string
	Path string
}

// Empty reports whether the plugin declares no static content.
func (a PluginAssets) Empty() bool {
	return a == PluginAssets{}
}

type pluginAssetDeclaration struct {
	name       string
	relative   string
	requireDir bool
	extensions []string
}

func (a PluginAssets) declarations() []pluginAssetDeclaration {
	return []pluginAssetDeclaration{
		{name: "cardTemplates", relative: a.CardTemplates, extensions: []string{".md"}},
		{name: "themes", relative: a.Themes, extensions: []string{".toml"}},
		{name: "frontmatterPresets", relative: a.FrontmatterPresets, extensions: []string{".toml"}},
		{name: "customCommands", relative: a.CustomCommands, extensions: []string{".yml", ".yaml"}},
		{name: "boardStarters", relative: a.BoardStarters, requireDir: true},
	}
}

func (a PluginAssets) resolve(id, root string) (RuntimeAssets, error) {
	assets := RuntimeAssets{ID: id, Root: root}
	paths := []struct {
		relative string
		resolved *string
	}{
		{a.CardTemplates, &assets.CardTemplates},
		{a.Themes, &assets.Themes},
		{a.FrontmatterPresets, &assets.FrontmatterPresets},
		{a.CustomCommands, &assets.CustomCommands},
		{a.BoardStarters, &assets.BoardStarters},
	}
	for _, path := range paths {
		if path.relative == "" {
			continue
		}
		resolved, err := safeRelativePath(root, path.relative)
		if err != nil {
			return RuntimeAssets{}, err
		}
		*path.resolved = resolved
	}
	return assets, nil
}
