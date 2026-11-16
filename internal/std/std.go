// Package std embeds Plain's standard library modules.
package std

import (
	"embed"
	"sort"
	"strings"
)

//go:embed lib/*.pln
var files embed.FS

// Source returns the code of a standard module.
func Source(name string) (string, bool) {
	b, err := files.ReadFile("lib/" + name + ".pln")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Names lists the modules that can be used without an import (core excluded).
func Names() []string {
	entries, _ := files.ReadDir("lib")
	var out []string
	for _, e := range entries {
		n := strings.TrimSuffix(e.Name(), ".pln")
		if n != "core" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
