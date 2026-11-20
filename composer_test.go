package framework_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The Laravel package is installed from this repository's root, because
// Composer only reads a composer.json at the root of a repository. The package
// itself lives in laravel/ and keeps its own composer.json for its tests. Two
// files that say the same thing drift, so this holds them together: the root
// file must name the same package, ask for the same things and register the
// same provider, with its code found under laravel/.
func TestRootComposerMatchesTheLaravelPackage(t *testing.T) {
	type manifest struct {
		Name     string            `json:"name"`
		License  string            `json:"license"`
		Require  map[string]string `json:"require"`
		Autoload struct {
			PSR4 map[string]string `json:"psr-4"`
		} `json:"autoload"`
		Extra struct {
			Laravel struct {
				Providers []string `json:"providers"`
			} `json:"laravel"`
		} `json:"extra"`
	}
	read := func(path string) manifest {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var m manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return m
	}
	root, pkg := read("composer.json"), read("laravel/composer.json")

	if root.Name != pkg.Name {
		t.Errorf("name: root %q, laravel %q", root.Name, pkg.Name)
	}
	if root.License != pkg.License {
		t.Errorf("license: root %q, laravel %q", root.License, pkg.License)
	}
	if !reflect.DeepEqual(root.Require, pkg.Require) {
		t.Errorf("require: root %v, laravel %v", root.Require, pkg.Require)
	}
	if !reflect.DeepEqual(root.Extra.Laravel.Providers, pkg.Extra.Laravel.Providers) {
		t.Errorf("providers: root %v, laravel %v", root.Extra.Laravel.Providers, pkg.Extra.Laravel.Providers)
	}
	want := map[string]string{}
	for namespace, dir := range pkg.Autoload.PSR4 {
		want[namespace] = "laravel/" + dir
	}
	if !reflect.DeepEqual(root.Autoload.PSR4, want) {
		t.Errorf("autoload: root %v, want %v", root.Autoload.PSR4, want)
	}
	for _, dir := range root.Autoload.PSR4 {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("autoload folder %q: %v", dir, err)
		}
	}
}

// A Composer install downloads an archive of the tag. Everything a Laravel app
// has no use for is left out of it by .gitattributes, so the package stays the
// size of its PHP: the Go source, the fixtures and the docs are named there.
func TestComposerArchiveLeavesOutWhatLaravelDoesNotNeed(t *testing.T) {
	raw, err := os.ReadFile(".gitattributes")
	if err != nil {
		t.Fatalf(".gitattributes: %v", err)
	}
	ignored := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "export-ignore" {
			ignored[strings.Trim(fields[0], "/")] = true
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	// What a Laravel app does need from the root.
	kept := map[string]bool{"composer.json": true, "laravel": true, "LICENSE": true, "README.md": true}
	for _, e := range entries {
		name := e.Name()
		if kept[name] || name == ".git" || name == ".gitattributes" || name == ".gitignore" {
			continue
		}
		if isUntracked(name) {
			continue
		}
		if !ignored[name] {
			t.Errorf("%s is neither kept for Composer nor marked export-ignore in .gitattributes", name)
		}
	}
	for name := range kept {
		if ignored[name] {
			t.Errorf("%s is needed by Composer but marked export-ignore", name)
		}
	}
}

// isUntracked reports whether a root entry is something git never archives
// anyway: the built binaries and local tool folders named in .gitignore.
func isUntracked(name string) bool {
	raw, err := os.ReadFile(".gitignore")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Trim(strings.TrimSpace(line), "/") == name {
			return true
		}
	}
	return false
}
