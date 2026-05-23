package engine

import (
	"fmt"

	"Gogoxel/internal/game/generators"
	"Gogoxel/internal/world"
)

type GeneratorCatalog struct {
	order  []string
	byName map[string]generators.Generator
}

func NewGeneratorCatalog(items []generators.Generator) *GeneratorCatalog {
	catalog := &GeneratorCatalog{byName: make(map[string]generators.Generator, len(items))}
	for _, item := range items {
		if item == nil {
			continue
		}
		name := item.Name()
		if name == "" {
			continue
		}
		if _, exists := catalog.byName[name]; exists {
			continue
		}
		catalog.byName[name] = item
		catalog.order = append(catalog.order, name)
	}
	return catalog
}

func DefaultGeneratorCatalog() *GeneratorCatalog {
	return NewGeneratorCatalog(generators.DefaultGenerators())
}

func (c *GeneratorCatalog) Names() []string {
	if c == nil {
		return nil
	}
	names := make([]string, len(c.order))
	copy(names, c.order)
	return names
}

func (c *GeneratorCatalog) DefaultName() string {
	if c == nil || len(c.order) == 0 {
		return ""
	}
	return c.order[0]
}

func (c *GeneratorCatalog) NextName(current string) string {
	if c == nil || len(c.order) == 0 {
		return ""
	}
	if current == "" {
		return c.order[0]
	}
	for index, name := range c.order {
		if name == current {
			return c.order[(index+1)%len(c.order)]
		}
	}
	return c.order[0]
}

func (c *GeneratorCatalog) Lookup(name string) (generators.Generator, bool) {
	if c == nil {
		return nil, false
	}
	item, ok := c.byName[name]
	return item, ok
}

func (c *GeneratorCatalog) Build(name string, request generators.BuildRequest) (*world.SVO, uint, error) {
	if c == nil {
		return nil, 0, fmt.Errorf("generator catalog is not initialized")
	}
	item, ok := c.byName[name]
	if !ok {
		return nil, 0, fmt.Errorf("unknown generator %q", name)
	}
	svo := world.NewSVO()
	if err := item.BuildSVO(svo, request); err != nil {
		return nil, 0, fmt.Errorf("building generator %q: %w", name, err)
	}
	return svo, item.ChunkSize(), nil
}