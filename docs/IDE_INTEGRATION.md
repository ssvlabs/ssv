# IDE integrations

The project uses [goimports](https://pkg.go.dev/golang.org/x/tools/cmd/goimports) to format code and imports section.

## Goland

Here are recommended Goland imports settings:

Editor -> Code Style -> Go -> Imports:

- `Use backquotes for imports`: ❌ unchecked
- `Add parentheses for a single import`: ✅ checked
- `Remove redundant import aliases`: ✅ checked
- `Sorting type`: `goimports` (dropdown selection)
- `Move all imports to a single declaration`: ✅ checked
- `Group packages from Go SDK`: ✅ checked
    - `Move all packages to a single group`: ✅ checked
- `Group`: ✅ checked
    - `Current project packages`: 🔘 selected
    - `Imports starting with`: ⚪ not selected (empty)

## VSCode

TODO
