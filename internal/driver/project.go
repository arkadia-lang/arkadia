package driver

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ManifestName is the file name of a package manifest.
const ManifestName = "ark.toml"

// LockName is the file name of the dependency lock file.
const LockName = "ark.lock"

// Dependency describes one entry of [dependencies].
type Dependency struct {
	Name string
	Path string // local path (relative to the manifest)
	Git  string // git repository URL
	Rev  string // tag, branch or commit for git dependencies
}

// Manifest is a parsed ark.toml.
type Manifest struct {
	Dir          string
	Name         string
	Version      string
	Description  string
	Entry        string // entry module file (default src/Main.ark)
	Dependencies []Dependency
}

// Project is a manifest with its resolved dependency directories.
type Project struct {
	Manifest *Manifest
	// source roots: the package's src dir followed by dependencies' src dirs
	SrcDirs []string
	Natives []string
}

// ---------------------------------------------------------------- TOML subset

// parseTOML parses the subset of TOML used by manifests: tables, string,
// integer and boolean values, arrays of strings and inline tables.
func parseTOML(src string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{"": {}}
	table := ""
	sc := bufio.NewScanner(strings.NewReader(src))
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(stripComment(sc.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: malformed table header", n)
			}
			table = strings.TrimSpace(line[1 : len(line)-1])
			if out[table] == nil {
				out[table] = map[string]any{}
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", n)
		}
		v, rest, err := parseTOMLValue(strings.TrimSpace(val))
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		if strings.TrimSpace(rest) != "" {
			return nil, fmt.Errorf("line %d: unexpected %q", n, rest)
		}
		out[table][unquoteKey(strings.TrimSpace(key))] = v
	}
	return out, nil
}

func unquoteKey(k string) string {
	if s, err := strconv.Unquote(k); err == nil {
		return s
	}
	return k
}

func stripComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if inStr {
				i++
			}
		case '"':
			inStr = !inStr
		case '#':
			if !inStr {
				return s[:i]
			}
		}
	}
	return s
}

func parseTOMLValue(s string) (any, string, error) {
	switch {
	case strings.HasPrefix(s, "\""):
		end := 1
		for end < len(s) && s[end] != '"' {
			if s[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(s) {
			return nil, "", fmt.Errorf("unterminated string")
		}
		str, err := strconv.Unquote(s[:end+1])
		return str, s[end+1:], err
	case strings.HasPrefix(s, "{"):
		m := map[string]any{}
		rest := strings.TrimSpace(s[1:])
		for !strings.HasPrefix(rest, "}") {
			key, after, ok := strings.Cut(rest, "=")
			if !ok {
				return nil, "", fmt.Errorf("malformed inline table")
			}
			v, r, err := parseTOMLValue(strings.TrimSpace(after))
			if err != nil {
				return nil, "", err
			}
			m[unquoteKey(strings.TrimSpace(key))] = v
			rest = strings.TrimSpace(r)
			rest = strings.TrimPrefix(rest, ",")
			rest = strings.TrimSpace(rest)
			if rest == "" {
				return nil, "", fmt.Errorf("unterminated inline table")
			}
		}
		return m, rest[1:], nil
	case strings.HasPrefix(s, "["):
		var arr []any
		rest := strings.TrimSpace(s[1:])
		for !strings.HasPrefix(rest, "]") {
			v, r, err := parseTOMLValue(rest)
			if err != nil {
				return nil, "", err
			}
			arr = append(arr, v)
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r), ","))
			if rest == "" {
				return nil, "", fmt.Errorf("unterminated array")
			}
		}
		return arr, rest[1:], nil
	case strings.HasPrefix(s, "true"):
		return true, s[4:], nil
	case strings.HasPrefix(s, "false"):
		return false, s[5:], nil
	}
	end := 0
	for end < len(s) && (s[end] == '-' || s[end] >= '0' && s[end] <= '9') {
		end++
	}
	if end == 0 {
		return nil, "", fmt.Errorf("invalid value %q", s)
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	return n, s[end:], err
}

// ---------------------------------------------------------------- manifest

// FindManifest looks for ark.toml in dir and its parents.
func FindManifest(dir string) (string, bool) {
	dir, _ = filepath.Abs(dir)
	for {
		p := filepath.Join(dir, ManifestName)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// LoadManifest parses an ark.toml file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := parseTOML(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	m := &Manifest{Dir: filepath.Dir(path), Entry: "src/Main.ark"}
	pkg := t["package"]
	if pkg == nil {
		return nil, fmt.Errorf("%s: missing [package] table", path)
	}
	str := func(k string) string {
		if v, ok := pkg[k].(string); ok {
			return v
		}
		return ""
	}
	m.Name, m.Version, m.Description = str("name"), str("version"), str("description")
	if e := str("entry"); e != "" {
		m.Entry = e
	}
	if m.Name == "" {
		return nil, fmt.Errorf("%s: package name is required", path)
	}
	var names []string
	for name := range t["dependencies"] {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		v := t["dependencies"][name]
		d := Dependency{Name: name}
		switch v := v.(type) {
		case string:
			d.Path = v
		case map[string]any:
			d.Path, _ = v["path"].(string)
			d.Git, _ = v["git"].(string)
			for _, k := range []string{"tag", "branch", "rev"} {
				if r, ok := v[k].(string); ok {
					d.Rev = r
				}
			}
		}
		if d.Path == "" && d.Git == "" {
			return nil, fmt.Errorf("%s: dependency %q needs a path or git source", path, name)
		}
		m.Dependencies = append(m.Dependencies, d)
	}
	return m, nil
}

// ---------------------------------------------------------------- dependencies

// lock maps dependency names to resolved git commits.
type lock map[string]string

func readLock(dir string) lock {
	l := lock{}
	data, err := os.ReadFile(filepath.Join(dir, LockName))
	if err != nil {
		return l
	}
	t, err := parseTOML(string(data))
	if err != nil {
		return l
	}
	for name, v := range t["locked"] {
		if s, ok := v.(string); ok {
			l[name] = s
		}
	}
	return l
}

func writeLock(dir string, l lock) error {
	if len(l) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("# Generated by ark. Do not edit.\n[locked]\n")
	var names []string
	for n := range l {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&sb, "%s = %q\n", strconv.Quote(n), l[n])
	}
	path := filepath.Join(dir, LockName)
	if old, err := os.ReadFile(path); err == nil && string(old) == sb.String() {
		return nil
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// fetchGit makes sure a git dependency is checked out and returns its
// directory and resolved commit.
func fetchGit(d Dependency, locked string, update bool) (string, string, error) {
	sum := sha256.Sum256([]byte(d.Git))
	repo := filepath.Join(cacheDir(), "git", d.Name+"-"+hex.EncodeToString(sum[:6]))
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(repo), 0o755); err != nil {
			return "", "", err
		}
		if _, err := git(filepath.Dir(repo), "clone", "--quiet", d.Git, repo); err != nil {
			return "", "", err
		}
	} else if update || locked == "" {
		git(repo, "fetch", "--quiet", "--tags", "origin")
	}
	target := locked
	if target == "" || update {
		rev := d.Rev
		if rev == "" {
			rev = "HEAD"
		}
		c, err := git(repo, "rev-parse", rev+"^{commit}")
		if err != nil {
			c, err = git(repo, "rev-parse", "origin/"+rev+"^{commit}")
			if err != nil {
				return "", "", fmt.Errorf("dependency %s: cannot resolve %q", d.Name, rev)
			}
		}
		target = c
	}
	checkout := filepath.Join(cacheDir(), "pkgs", d.Name+"-"+target[:12])
	if _, err := os.Stat(checkout); err != nil {
		if _, err := git(repo, "worktree", "add", "--detach", "--force", checkout, target); err != nil {
			return "", "", err
		}
	}
	return checkout, target, nil
}

// ResolveProject loads a project and its dependencies (recursively).
func ResolveProject(manifestPath string, update bool) (*Project, error) {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	p := &Project{Manifest: m}
	locks := readLock(m.Dir)
	newLocks := lock{}
	seen := map[string]string{}
	var visit func(dir string, deps []Dependency) error
	visit = func(dir string, deps []Dependency) error {
		for _, d := range deps {
			var depDir string
			switch {
			case d.Path != "":
				depDir = d.Path
				if !filepath.IsAbs(depDir) {
					depDir = filepath.Join(dir, depDir)
				}
			default:
				var commit string
				depDir, commit, err = fetchGit(d, locks[d.Name], update)
				if err != nil {
					return err
				}
				newLocks[d.Name] = commit
			}
			depDir, _ = filepath.Abs(depDir)
			if prev, ok := seen[d.Name]; ok {
				if prev != depDir {
					return fmt.Errorf("dependency %s is required from two different sources", d.Name)
				}
				continue
			}
			seen[d.Name] = depDir
			dm, err := LoadManifest(filepath.Join(depDir, ManifestName))
			if err != nil {
				return fmt.Errorf("dependency %s: %v", d.Name, err)
			}
			p.SrcDirs = append(p.SrcDirs, filepath.Join(depDir, "src"))
			p.Natives = append(p.Natives, nativeFiles(depDir)...)
			if err := visit(depDir, dm.Dependencies); err != nil {
				return err
			}
		}
		return nil
	}
	p.SrcDirs = append(p.SrcDirs, filepath.Join(m.Dir, "src"))
	p.Natives = append(p.Natives, nativeFiles(m.Dir)...)
	if err := visit(m.Dir, m.Dependencies); err != nil {
		return nil, err
	}
	if err := writeLock(m.Dir, newLocks); err != nil {
		return nil, err
	}
	return p, nil
}

// nativeFiles lists Go files of a package's native/ directory. They are
// compiled into the program and can implement `foreign` functions.
func nativeFiles(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "native", "*.go"))
	sort.Strings(files)
	return files
}

// Modules lists the module names defined under a source root.
func modulesIn(root string) []string {
	var mods []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".ark") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		mods = append(mods, strings.ReplaceAll(strings.TrimSuffix(rel, ".ark"), string(filepath.Separator), "."))
		return nil
	})
	sort.Strings(mods)
	return mods
}
