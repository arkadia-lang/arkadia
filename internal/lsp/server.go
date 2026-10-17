// Package lsp implements a Language Server Protocol server for Arkadia:
// diagnostics, hover (types), go to definition and formatting.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/verniyyy/arkadia/internal/diag"
	"github.com/verniyyy/arkadia/internal/driver"
	"github.com/verniyyy/arkadia/internal/syntax"
	"github.com/verniyyy/arkadia/internal/types"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rng struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string `json:"uri"`
	Range rng    `json:"range"`
}

type textEdit struct {
	Range   rng    `json:"range"`
	NewText string `json:"newText"`
}

type diagnostic struct {
	Range    rng    `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// Server is an LSP server instance.
type Server struct {
	in      *bufio.Reader
	out     io.Writer
	mu      sync.Mutex
	docs    map[string]string // abs path -> text
	info    map[string][]*types.IdentInfo
	lastErr map[string]bool // files with published diagnostics
	sources *diag.Sources   // of the last checked program (for doc comments)
}

// Serve runs the server on stdin/stdout until exit.
func Serve() error {
	s := &Server{in: bufio.NewReader(os.Stdin), out: os.Stdout, docs: map[string]string{}, info: map[string][]*types.IdentInfo{}, lastErr: map[string]bool{}}
	return s.loop()
}

func (s *Server) loop() error {
	for {
		msg, err := s.read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if msg.Method == "exit" {
			return nil
		}
		s.handle(msg)
	}
}

func (s *Server) read() (*message, error) {
	length := 0
	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(s.in, buf); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Server) send(m any) {
	data, _ := json.Marshal(m)
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func (s *Server) reply(id json.RawMessage, result any) {
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *Server) notify(method string, params any) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.Clean(u.Path)
}

func pathToURI(p string) string {
	abs, _ := filepath.Abs(p)
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

func (s *Server) handle(m *message) {
	switch m.Method {
	case "initialize":
		s.reply(m.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           map[string]any{"openClose": true, "change": 1, "save": true},
				"hoverProvider":              true,
				"definitionProvider":         true,
				"documentFormattingProvider": true,
			},
			"serverInfo": map[string]any{"name": "ark-lsp", "version": driver.Version},
		})
	case "shutdown":
		s.reply(m.ID, nil)
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		path := uriToPath(p.TextDocument.URI)
		s.docs[path] = p.TextDocument.Text
		s.check(path)
	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		json.Unmarshal(m.Params, &p)
		path := uriToPath(p.TextDocument.URI)
		if n := len(p.ContentChanges); n > 0 {
			s.docs[path] = p.ContentChanges[n-1].Text
		}
		s.check(path)
	case "textDocument/didSave":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		s.check(uriToPath(p.TextDocument.URI))
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		json.Unmarshal(m.Params, &p)
		delete(s.docs, uriToPath(p.TextDocument.URI))
	case "textDocument/hover":
		path, pos := s.params(m.Params)
		if id := s.find(path, pos); id != nil {
			text := "```arkadia\n" + id.Describe() + "\n```"
			if doc := s.docComment(id); doc != "" {
				text += "\n\n" + doc
			}
			s.reply(m.ID, map[string]any{
				"contents": map[string]any{"kind": "markdown", "value": text},
				"range":    identRange(id),
			})
			return
		}
		s.reply(m.ID, nil)
	case "textDocument/definition":
		path, pos := s.params(m.Params)
		if id := s.find(path, pos); id != nil && id.Def.Line > 0 && !strings.HasPrefix(id.Def.File, "<std>") {
			p := position{Line: id.Def.Line - 1, Character: id.Def.Col - 1}
			s.reply(m.ID, location{URI: pathToURI(id.Def.File), Range: rng{p, p}})
			return
		}
		s.reply(m.ID, nil)
	case "textDocument/formatting":
		path, _ := s.params(m.Params)
		s.reply(m.ID, s.format(path))
	default:
		if m.ID != nil {
			s.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": rpcError{Code: -32601, Message: "method not found"}})
		}
	}
}

// format returns the edits that format a document: none when it is
// formatted or has syntax errors (which the diagnostics already report).
func (s *Server) format(path string) []textEdit {
	text, ok := s.docs[path]
	if !ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text = string(data)
	}
	out, err := syntax.Format(path, text)
	if err != nil || out == text {
		return []textEdit{}
	}
	// replace the whole document; the end is counted in UTF-16 code units
	lines := strings.Split(text, "\n")
	last := lines[len(lines)-1]
	end := position{Line: len(lines) - 1, Character: len(utf16.Encode([]rune(last)))}
	return []textEdit{{Range: rng{End: end}, NewText: out}}
}

// docComment returns the documentation comment of the definition of an
// identifier.
func (s *Server) docComment(id *types.IdentInfo) string {
	if s.sources == nil || id.Def.Line == 0 || id.Kind == "local" {
		return ""
	}
	name := id.Name
	if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	return s.sources.DocComment(id.Def, name)
}

func identRange(id *types.IdentInfo) rng {
	start := position{Line: id.Pos.Line - 1, Character: id.Pos.Col - 1}
	return rng{start, position{Line: start.Line, Character: start.Character + id.Len}}
}

func (s *Server) params(raw json.RawMessage) (string, position) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position position `json:"position"`
	}
	json.Unmarshal(raw, &p)
	return uriToPath(p.TextDocument.URI), p.Position
}

func (s *Server) find(path string, pos position) *types.IdentInfo {
	var best *types.IdentInfo
	for _, id := range s.info[path] {
		if id.Pos.Line-1 != pos.Line {
			continue
		}
		start := id.Pos.Col - 1
		if pos.Character >= start && pos.Character <= start+id.Len {
			if best == nil || id.Len < best.Len {
				best = id
			}
		}
	}
	return best
}

// check type checks the program containing path and publishes diagnostics.
func (s *Server) check(path string) {
	var paths []string
	entry := path
	if mp, ok := driver.FindManifest(filepath.Dir(path)); ok {
		if p, err := driver.ResolveProject(mp, false); err == nil {
			paths = p.SrcDirs
		}
	}
	paths = append([]string{filepath.Dir(path)}, paths...)
	l := driver.NewLoader(paths)
	l.Overlay = s.docs
	l.CollectInfo = true
	_, err := l.LoadFile(entry)
	s.sources = l.Sources
	diags := map[string][]diagnostic{}
	if err != nil {
		var errs []*diag.Error
		switch e := err.(type) {
		case diag.List:
			errs = e
		case *diag.Error:
			errs = []*diag.Error{e}
		default:
			errs = []*diag.Error{{Pos: diag.Pos{File: path, Line: 1, Col: 1}, Msg: err.Error()}}
		}
		for _, e := range errs {
			file := e.Pos.File
			if file == "" {
				file = path
			}
			abs, _ := filepath.Abs(file)
			line, col := max(e.Pos.Line-1, 0), max(e.Pos.Col-1, 0)
			msg := e.Msg
			for _, n := range e.Notes {
				msg += "\n" + n
			}
			diags[abs] = append(diags[abs], diagnostic{
				Range:    rng{position{line, col}, position{line, col + 1}},
				Severity: 1,
				Source:   "arkadia",
				Message:  msg,
			})
		}
	}
	if l.Global != nil {
		byFile := map[string][]*types.IdentInfo{}
		for _, id := range l.Global.Info {
			abs, _ := filepath.Abs(id.Pos.File)
			byFile[abs] = append(byFile[abs], id)
		}
		for f, ids := range byFile {
			sort.Slice(ids, func(i, j int) bool { return ids[i].Pos.Line < ids[j].Pos.Line })
			s.info[f] = ids
		}
	}
	// clear diagnostics of files that are now clean
	for f := range s.lastErr {
		if _, ok := diags[f]; !ok {
			s.notify("textDocument/publishDiagnostics", map[string]any{"uri": pathToURI(f), "diagnostics": []diagnostic{}})
			delete(s.lastErr, f)
		}
	}
	if _, ok := diags[path]; !ok {
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": pathToURI(path), "diagnostics": []diagnostic{}})
	}
	for f, ds := range diags {
		if strings.HasPrefix(f, "<std>") {
			continue
		}
		s.lastErr[f] = true
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": pathToURI(f), "diagnostics": ds})
	}
}
