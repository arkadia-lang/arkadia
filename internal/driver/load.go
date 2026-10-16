// Package driver loads Arkadia modules, type checks them and builds
// executables.
package driver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/verniyyy/arkadia/internal/diag"
	"github.com/verniyyy/arkadia/internal/syntax"
	"github.com/verniyyy/arkadia/internal/types"
	"github.com/verniyyy/arkadia/lib"
)

// Program is a loaded and type checked set of modules.
type Program struct {
	Modules []*syntax.Module // dependency order (Prelude first)
	Global  *types.Global
	Main    string // name of the entry module
	Sources *diag.Sources
	Natives []string // Go source files to include in the build
}

// Loader finds and parses modules.
type Loader struct {
	SearchPaths []string // directories containing module sources
	Sources     *diag.Sources
	// Overlay maps absolute file paths to unsaved editor contents.
	Overlay map[string]string
	// CollectInfo records identifier information for editor tooling.
	CollectInfo bool
	// Global is set after checking, even when checking fails.
	Global   *types.Global
	mods     map[string]*syntax.Module
	order    []*syntax.Module
	visiting map[string]bool
}

func NewLoader(paths []string) *Loader {
	return &Loader{SearchPaths: paths, Sources: diag.NewSources(), mods: map[string]*syntax.Module{}, visiting: map[string]bool{}}
}

func modulePath(name string) string {
	return filepath.Join(strings.Split(name, ".")...) + ".ark"
}

func (l *Loader) readFile(path string) ([]byte, error) {
	if l.Overlay != nil {
		if abs, err := filepath.Abs(path); err == nil {
			if src, ok := l.Overlay[abs]; ok {
				return []byte(src), nil
			}
		}
	}
	return os.ReadFile(path)
}

func (l *Loader) readModule(name string) (file, src string, err error) {
	rel := modulePath(name)
	for _, dir := range l.SearchPaths {
		p := filepath.Join(dir, rel)
		if data, err := l.readFile(p); err == nil {
			return p, string(data), nil
		}
	}
	if data, err := fs.ReadFile(lib.FS, filepath.ToSlash(rel)); err == nil {
		return "<std>/" + filepath.ToSlash(rel), string(data), nil
	}
	return "", "", fmt.Errorf("module %s not found (looked for %s)", name, rel)
}

func (l *Loader) parse(file, src, expectName string) (*syntax.Module, error) {
	l.Sources.Add(file, src)
	m, err := syntax.ParseModule(file, src)
	if err != nil {
		return nil, err
	}
	if m.Name == "" {
		m.Name = expectName
	}
	if expectName != "" && m.Name != expectName {
		return nil, diag.Errorf(diag.Pos{File: file, Line: 1, Col: 1}, "module is declared as %s but was imported as %s", m.Name, expectName)
	}
	return m, nil
}

// load parses a module and (recursively) its imports.
func (l *Loader) load(m *syntax.Module) error {
	if l.visiting[m.Name] {
		return diag.Errorf(diag.Pos{File: m.File, Line: 1, Col: 1}, "import cycle involving module %s", m.Name)
	}
	l.visiting[m.Name] = true
	l.mods[m.Name] = m
	deps := []string{}
	if m.Name != types.PreludeMod {
		deps = append(deps, types.PreludeMod)
	}
	for _, imp := range m.Imports {
		deps = append(deps, imp.Path)
	}
	for _, d := range deps {
		if _, ok := l.mods[d]; ok {
			if l.visiting[d] {
				return diag.Errorf(diag.Pos{File: m.File, Line: 1, Col: 1}, "import cycle between %s and %s", m.Name, d)
			}
			continue
		}
		file, src, err := l.readModule(d)
		if err != nil {
			pos := diag.Pos{File: m.File, Line: 1, Col: 1}
			for _, imp := range m.Imports {
				if imp.Path == d {
					pos = imp.P
				}
			}
			return diag.Errorf(pos, "%v", err)
		}
		dm, err := l.parse(file, src, d)
		if err != nil {
			return err
		}
		if err := l.load(dm); err != nil {
			return err
		}
	}
	l.visiting[m.Name] = false
	l.order = append(l.order, m)
	return nil
}

// LoadFile loads an entry module from a file plus everything it imports.
func (l *Loader) LoadFile(path string, extra ...string) (*Program, error) {
	data, err := l.readFile(path)
	if err != nil {
		return nil, err
	}
	m, err := l.parse(path, string(data), "")
	if err != nil {
		return nil, err
	}
	if m.Name == "" {
		m.Name = "Main"
	}
	if err := l.load(m); err != nil {
		return nil, err
	}
	for _, name := range extra {
		if _, ok := l.mods[name]; ok {
			continue
		}
		file, src, err := l.readModule(name)
		if err != nil {
			return nil, err
		}
		em, err := l.parse(file, src, name)
		if err != nil {
			return nil, err
		}
		if err := l.load(em); err != nil {
			return nil, err
		}
	}
	return l.check(m.Name)
}

// LoadModules loads the named modules (and their imports) without an entry
// module; used for tests and documentation.
func (l *Loader) LoadModules(names []string) (*Program, error) {
	for _, name := range names {
		if _, ok := l.mods[name]; ok {
			continue
		}
		file, src, err := l.readModule(name)
		if err != nil {
			return nil, err
		}
		m, err := l.parse(file, src, name)
		if err != nil {
			return nil, err
		}
		if err := l.load(m); err != nil {
			return nil, err
		}
	}
	return l.check("")
}

func (l *Loader) check(main string) (*Program, error) {
	fx := syntax.DefaultFixities()
	var errs diag.List
	for _, m := range l.order {
		if err := fx.Collect(m); err != nil {
			errs = append(errs, err.(diag.List)...)
		}
	}
	for _, m := range l.order {
		if err := syntax.ResolveFixity(m, fx); err != nil {
			errs = append(errs, err.(diag.List)...)
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	g := types.NewGlobal()
	g.CollectInfo = l.CollectInfo
	l.Global = g
	for _, m := range l.order {
		if err := types.CheckModule(g, m); err != nil {
			return nil, err
		}
	}
	return &Program{Modules: l.order, Global: g, Main: main, Sources: l.Sources}, nil
}
