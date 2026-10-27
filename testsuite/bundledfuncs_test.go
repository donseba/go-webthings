package testsuite

import (
	"maps"
	"slices"
	"testing"

	"github.com/donseba/go-webthings/internal/bundled"
)

// TestBundledFuncs checks that the template check at Start knows every
// template function of every bundled module, and no other, with the module
// it comes from, for its hint to add a module to app.Use.
func TestBundledFuncs(t *testing.T) {
	t.Parallel()

	offered := map[string]string{}

	for _, module := range allModules() {
		for name := range module.Funcs {
			offered[name] = module.Type
		}
	}

	for _, name := range slices.Sorted(maps.Keys(offered)) {
		if module := bundled.ModuleOf(name); module != offered[name] {
			t.Errorf("function %s of module %s: the check says %q", name, offered[name], module)
		}
	}

	for module, funcs := range bundled.Funcs() {
		for _, name := range funcs {
			if offered[name] != module {
				t.Errorf("function %s of %s is no function of that module", name, module)
			}
		}
	}
}
