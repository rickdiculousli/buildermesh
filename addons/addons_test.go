package addons

import (
	"slices"
	"testing"

	"github.com/rickdiculousli/buildermesh/internal/config"
)

// The names config.toml accepts are the add-ons there are.
func TestRegistryIsConfigsNames(t *testing.T) {
	var names []string
	for n := range Registry {
		names = append(names, n)
	}
	slices.Sort(names)
	if want := slices.Sorted(slices.Values(config.AddonNames)); !slices.Equal(names, want) {
		t.Fatalf("registry %v, config.AddonNames %v", names, want)
	}
}
