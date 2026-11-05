package sites

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This file is the architectural fence around the plugin boundary. Shared
// layers may only talk to the interfaces in package sites; plugin ids,
// imports and implementation packages belong to internal/sites/plugins.

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func pluginIDs(t *testing.T, root string) []string {
	t.Helper()
	pluginsDir := filepath.Join(root, "internal", "sites", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("read plugins dir: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	return ids
}

func goFiles(t *testing.T, root string, dirs []string, recurse bool) []string {
	t.Helper()
	var files []string
	for _, dir := range dirs {
		path := filepath.Join(root, dir)
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if !recurse || entry.Name() == "plugins" || entry.Name() == "web" {
					continue
				}
				files = append(files, goFiles(t, path, []string{entry.Name()}, true)...)
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			files = append(files, filepath.Join(path, name))
		}
	}
	return files
}

func parsedImports(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	imports := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", spec.Path.Value, err)
		}
		imports = append(imports, path)
	}
	return imports
}

func TestSharedLayersAreSiteAgnostic(t *testing.T) {
	root := repoRoot(t)
	sharedDirs := []string{
		"cmd",
		"internal/httpapi",
		"internal/download",
		"internal/sync",
		"internal/store",
		"internal/media",
		"internal/vault",
		"internal/netproxy",
		"internal/config",
		"internal/cookieutil",
		"internal/version",
	}
	files := goFiles(t, root, sharedDirs, true)
	files = append(files, goFiles(t, root, []string{"internal/sites"}, false)...)

	forbiddenImports := []string{
		"magpiesync/internal/sites/plugins/",
	}
	ids := pluginIDs(t, root)
	for _, file := range files {
		for _, imp := range parsedImports(t, file) {
			for _, prefix := range forbiddenImports {
				if strings.HasPrefix(imp, prefix) {
					t.Errorf("%s imports %s; shared layers must not depend on a site implementation", rel(root, file), imp)
				}
			}
		}
	}

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, id := range ids {
				if strings.Contains(strings.ToLower(value), strings.ToLower(id)) {
					t.Errorf("%s contains plugin-specific text %q", rel(root, file), id)
				}
			}
			return true
		})
	}
}

func TestPluginsDoNotDependOnEachOther(t *testing.T) {
	root := repoRoot(t)
	pluginsDir := filepath.Join(root, "internal", "sites", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("read plugins dir: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		plugin := entry.Name()
		files := goFiles(t, pluginsDir, []string{plugin}, true)
		for _, file := range files {
			for _, imp := range parsedImports(t, file) {
				ownPrefix := "magpiesync/internal/sites/plugins/" + plugin + "/"
				if strings.HasPrefix(imp, "magpiesync/internal/sites/plugins/") && !strings.HasPrefix(imp, ownPrefix) {
					t.Errorf("%s imports another plugin (%s)", rel(root, file), imp)
				}
				if strings.HasPrefix(imp, "magpiesync/internal/httpapi") || strings.HasPrefix(imp, "magpiesync/internal/download") {
					t.Errorf("%s imports the shared transport layer (%s); move the behavior behind the driver interfaces", rel(root, file), imp)
				}
			}
		}
	}
}

func TestWebClientHasNoSiteIDs(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "internal", "httpapi", "web", "app.js")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	text := string(src)
	for _, id := range pluginIDs(t, root) {
		if strings.Contains(text, `"`+id+`"`) || strings.Contains(text, `'`+id+`'`) {
			t.Errorf("app.js hardcodes the plugin id %q; render the UI from /api/sites metadata instead", id)
		}
	}
}

func TestWebAssetsDoNotContainPluginLogos(t *testing.T) {
	root := repoRoot(t)
	logos := filepath.Join(root, "internal", "httpapi", "web", "logos")
	entries, err := os.ReadDir(logos)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read shared logos: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("shared web assets still contain plugin logos: %v", entries)
	}
}

func TestPluginManifestsMatchDirectories(t *testing.T) {
	root := repoRoot(t)
	pluginsDir := filepath.Join(root, "internal", "sites", "plugins")
	allDir := filepath.Join(root, "internal", "sites", "all")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("read plugins dir: %v", err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		seen[name+".go"] = true
		constraint := pluginBuildConstraint(t, filepath.Join(pluginsDir, name))
		data, err := os.ReadFile(filepath.Join(allDir, name+".go"))
		if err != nil {
			t.Errorf("missing generated manifest internal/sites/all/%s.go; run go generate ./internal/sites/all", name)
			continue
		}
		text := string(data)
		if !strings.Contains(text, "//go:build "+constraint+"\n") {
			t.Errorf("internal/sites/all/%s.go does not copy the plugin build constraint %q", name, constraint)
		}
		if !strings.Contains(text, `"magpiesync/internal/sites/plugins/`+name+`"`) {
			t.Errorf("internal/sites/all/%s.go does not import the plugin package", name)
		}
	}

	allEntries, err := os.ReadDir(allDir)
	if err != nil {
		t.Fatalf("read all dir: %v", err)
	}
	for _, entry := range allEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || seen[entry.Name()] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(allDir, entry.Name()))
		if err != nil {
			t.Fatalf("read all/%s: %v", entry.Name(), err)
		}
		if bytes.HasPrefix(data, []byte("// Code generated by internal/sites/all/gen.go; DO NOT EDIT.")) {
			t.Errorf("stale generated manifest internal/sites/all/%s.go; run go generate ./internal/sites/all", entry.Name())
		}
	}
}

func pluginBuildConstraint(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read plugin dir %s: %v", dir, err)
	}
	constraint := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == "doc.go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		found := ""
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "//go:build ") {
				found = strings.TrimSpace(strings.TrimPrefix(line, "//go:build "))
				break
			}
			if strings.HasPrefix(line, "package ") {
				break
			}
		}
		if found == "" {
			t.Errorf("%s/%s has no //go:build constraint", dir, entry.Name())
			return ""
		}
		if constraint == "" {
			constraint = found
		} else if constraint != found {
			t.Errorf("%s: files disagree on the build constraint: %q vs %q", dir, constraint, found)
			return ""
		}
	}
	if constraint == "" {
		t.Errorf("%s: no constrained Go files", dir)
	}
	return constraint
}

func rel(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}
