// Package assets is the foundation of the asset import/build pipeline.
// See issue #25.
//
// The first slice defines the Importer interface and a registry so
// concrete loaders (.vox, .gltf, .png, audio) can register themselves
// in init(). Build-time caching and dependency tracking come in a
// follow-up.
package assets

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

// Descriptor identifies one asset by its logical path and discovered
// type.
type Descriptor struct {
	Path string
	Kind string
}

// Importer parses raw bytes for one asset kind.
type Importer interface {
	// Extensions returns the lowercase file extensions (including the
	// leading dot) this importer handles.
	Extensions() []string
	// Import decodes one asset from r. The returned value is opaque to
	// the registry.
	Import(r io.Reader) (any, error)
}

// ErrNoImporter is returned by Find when no importer handles the
// requested extension.
var ErrNoImporter = errors.New("assets: no importer for extension")

var (
	regMu     sync.RWMutex
	importers = make(map[string]Importer)
)

// Register associates imp with all its extensions. Subsequent registers
// for the same extension overwrite.
func Register(imp Importer) {
	regMu.Lock()
	defer regMu.Unlock()
	for _, ext := range imp.Extensions() {
		importers[strings.ToLower(ext)] = imp
	}
}

// Find returns the importer registered for path's extension, or
// ErrNoImporter.
func Find(path string) (Importer, error) {
	ext := strings.ToLower(filepath.Ext(path))
	regMu.RLock()
	defer regMu.RUnlock()
	imp, ok := importers[ext]
	if !ok {
		return nil, ErrNoImporter
	}
	return imp, nil
}
