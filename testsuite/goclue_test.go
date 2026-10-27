package testsuite

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/template/parse"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/modules/stats"
	"github.com/donseba/go-webthings/modules/submissions"
	"github.com/donseba/go-webthings/modules/testimonials"

	partial "github.com/donseba/go-partial"
)

// templateBuiltins are the functions of text/template itself.
var templateBuiltins = []string{
	"and", "call", "eq", "ge", "gt", "html", "index", "js", "le", "len", "lt", "ne",
	"not", "or", "print", "printf", "println", "slice", "urlquery",
}

// goClueDoc is a template go-clue checks: its text, and the file go-clue
// takes it for.
type goClueDoc struct {
	// Name is the template's file, or the starting point it was generated as.
	Name string
	// Path is the file go-clue checks the text as: Name, or for a generated
	// starting point, which go-clue has not indexed, a site template of the
	// module that has every template function.
	Path string
	Text string
}

// goClueDiagnostic is a problem go-clue reports in a template.
type goClueDiagnostic struct {
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
	} `json:"range"`
	Message string `json:"message"`
}

// goClue runs go-clue, the template checker the module has as a tool (go
// tool go-clue), configured in .go-clue/config.json of the project it checks.
type goClue struct {
	// moduleDir is go-webthings' root, whose go.mod has the tool.
	moduleDir string
}

// findGoClue returns go-clue, or skips the test when it cannot run: in short
// mode, without a go command, or when the tool cannot be built.
func findGoClue(t *testing.T) goClue {
	t.Helper()

	if testing.Short() {
		t.Skip("go-clue runs the go command; skipped in short mode")
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command to run go-clue")
	}

	moduleDir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	clue := goClue{moduleDir: moduleDir}

	if output, err := clue.command("version").CombinedOutput(); err != nil {
		t.Skipf("go tool go-clue is not available: %v\n%s", err, output)
	}

	return clue
}

// command is go-clue with args, run in the module's root.
func (g goClue) command(args ...string) *exec.Cmd {
	cmd := exec.Command("go", append([]string{"tool", "go-clue"}, args...)...)
	cmd.Dir = g.moduleDir

	return cmd
}

// functions are the names of the template functions go-clue knows in the
// project at root: those of the function maps and the template functions
// of its configuration, and those its providers annotate.
func (g goClue) functions(t *testing.T, root string) []string {
	t.Helper()

	var stderr bytes.Buffer

	cmd := g.command("index", root)
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("go-clue index %s: %v\n%s", root, err, stderr.String())
	}

	var index struct {
		Templates map[string]struct {
			Funcs map[string]string `json:"funcs"`
		} `json:"templates"`
	}

	if err := json.Unmarshal(output, &index); err != nil {
		t.Fatalf("go-clue index %s: %v", root, err)
	}

	var names []string

	for _, template := range index.Templates {
		for name := range template.Funcs {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	return slices.Compact(names)
}

// diagnostics opens docs in go-clue's language server for the project at
// root, one after the other, and returns what it reports for each, by name.
func (g goClue) diagnostics(t *testing.T, root string, docs []goClueDoc) map[string][]goClueDiagnostic {
	t.Helper()

	cmd := g.command("lsp", root)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// A language server that stops answering fails the test instead of
	// hanging it.
	timer := time.AfterFunc(2*time.Minute, func() { _ = cmd.Process.Kill() })
	defer timer.Stop()

	reader := bufio.NewReader(output)
	reported := map[string][]goClueDiagnostic{}

	writeLSP(t, input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"rootUri": "file://" + root},
	})
	writeLSP(t, input, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})

	for _, doc := range docs {
		uri := "file://" + doc.Path

		writeLSP(t, input, map[string]any{
			"jsonrpc": "2.0", "method": "textDocument/didOpen",
			"params": map[string]any{"textDocument": map[string]any{
				"uri": uri, "languageId": "gotmpl", "version": 1, "text": doc.Text,
			}},
		})

		// The server answers every opened document with its diagnostics,
		// before it reads the next message.
		for {
			message, err := readLSP(reader)
			if err != nil {
				t.Fatalf("go-clue lsp %s: %v\n%s", root, err, stderr.String())
			}

			if message.Method == "textDocument/publishDiagnostics" && message.Params.URI == uri {
				reported[doc.Name] = message.Params.Diagnostics
				break
			}
		}
	}

	writeLSP(t, input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
	writeLSP(t, input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	_ = input.Close()

	_, _ = io.Copy(io.Discard, reader)
	_ = cmd.Wait()

	return reported
}

// lspMessage is a message of go-clue's language server, as far as the test
// reads it.
type lspMessage struct {
	Method string `json:"method"`
	Params struct {
		URI         string             `json:"uri"`
		Diagnostics []goClueDiagnostic `json:"diagnostics"`
	} `json:"params"`
}

// writeLSP sends message to a language server.
func writeLSP(t *testing.T, w io.Writer, message any) {
	t.Helper()

	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		t.Fatal(err)
	}
}

// readLSP reads the next message of a language server.
func readLSP(r *bufio.Reader) (lspMessage, error) {
	length := -1

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return lspMessage{}, err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		if value, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return lspMessage{}, err
			}
		}
	}

	if length < 0 {
		return lspMessage{}, fmt.Errorf("message without Content-Length")
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return lspMessage{}, err
	}

	var message lspMessage

	err := json.Unmarshal(body, &message)

	return message, err
}

// projectTemplates are the templates of the project at root, outside the
// folders go-clue skips and the sites' state (var).
func projectTemplates(t *testing.T, root string) []goClueDoc {
	t.Helper()

	var docs []goClueDoc

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "var", "bin":
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".gohtml" {
			return nil
		}

		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		docs = append(docs, goClueDoc{Name: name, Path: path, Text: string(text)})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return docs
}

// startingPoints are the templates App.WriteTemplates generates for the site
// and every bundled module: the modules' default templates with their
// go-clue contracts. go-clue checks them as host, an indexed site template.
func startingPoints(t *testing.T, host string) []goClueDoc {
	t.Helper()

	app := webthings.New(webthings.Config{})

	err := app.Use(
		banners.Module("banners", "Banners"),
		comments.Module("comments", "Comments"),
		directory.Module("directory", "Directory"),
		documents.Module("documents", "Documents"),
		events.Module("agenda", "Agenda"),
		forms.Module("contact", "Contact"),
		gallery.Module("gallery", "Gallery"),
		lists.Module("faq", "FAQ"),
		newsletter.Module("newsletter", "Newsletter"),
		people.Module("team", "Team"),
		polls.Module("polls", "Polls"),
		publishing.Module("news", "News"),
		redirects.Module(),
		registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
		stats.Module(),
		submissions.Module("proposals", "Proposals", "news"),
		testimonials.Module("quotes", "Testimonials"),
	)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()

	for _, key := range app.TemplateKeys() {
		if _, err := app.WriteTemplates(dir, key); err != nil {
			t.Fatal(err)
		}
	}

	docs := projectTemplates(t, dir)

	for i := range docs {
		docs[i].Name = "starting point " + filepath.ToSlash(docs[i].Name)
		docs[i].Path = host
	}

	return docs
}

// unresolvedFunctions are the functions doc calls that are neither
// text/template's, nor go-partial's own (such as content and runtime, which
// go-clue cannot know), nor a typed root the template declares (@model),
// nor among known.
func unresolvedFunctions(doc goClueDoc, known []string) ([]string, error) {
	declarations, err := partial.ParseDeclarations(doc.Text)
	if err != nil {
		return nil, err
	}

	allowed := slices.Concat(known, templateBuiltins, partial.ReservedFuncNames())

	for _, declaration := range declarations {
		allowed = append(allowed, declaration.Name)
	}

	tree := parse.New(doc.Name)
	tree.Mode = parse.SkipFuncCheck

	trees := map[string]*parse.Tree{}
	if _, err := tree.Parse(doc.Text, "{{", "}}", trees); err != nil {
		return nil, err
	}

	var unresolved []string

	for _, parsed := range trees {
		for _, name := range calledFunctions(parsed.Root) {
			if !slices.Contains(allowed, name) && !slices.Contains(unresolved, name) {
				unresolved = append(unresolved, name)
			}
		}
	}

	slices.Sort(unresolved)

	return unresolved, nil
}

// calledFunctions are the names of the functions node calls: in a parsed
// template every identifier is a function.
func calledFunctions(node parse.Node) []string {
	var names []string

	switch node := node.(type) {
	case *parse.ListNode:
		if node == nil {
			return nil
		}

		for _, child := range node.Nodes {
			names = append(names, calledFunctions(child)...)
		}
	case *parse.ActionNode:
		names = calledFunctions(node.Pipe)
	case *parse.IfNode:
		names = slices.Concat(calledFunctions(node.Pipe), calledFunctions(node.List), calledFunctions(node.ElseList))
	case *parse.RangeNode:
		names = slices.Concat(calledFunctions(node.Pipe), calledFunctions(node.List), calledFunctions(node.ElseList))
	case *parse.WithNode:
		names = slices.Concat(calledFunctions(node.Pipe), calledFunctions(node.List), calledFunctions(node.ElseList))
	case *parse.TemplateNode:
		names = calledFunctions(node.Pipe)
	case *parse.PipeNode:
		if node == nil {
			return nil
		}

		for _, command := range node.Cmds {
			names = append(names, calledFunctions(command)...)
		}
	case *parse.CommandNode:
		for _, arg := range node.Args {
			names = append(names, calledFunctions(arg)...)
		}
	case *parse.ChainNode:
		names = calledFunctions(node.Node)
	case *parse.IdentifierNode:
		names = []string{node.Ident}
	}

	return names
}

// signatureProblem reports whether go-clue's message is about a function
// call: arguments that do not match the function's signature, or a function
// whose results a template cannot use. go-clue's other diagnostics are about
// fields and template contracts; see TestGoClue.
func signatureProblem(message string) bool {
	for _, prefix := range []string{"Cannot pass ", "Cannot pipe ", "Function ", "Unknown go-clue function "} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}

	return false
}

// TestGoClue checks the templates with go-clue (go tool go-clue): the admin's
// and the modules' templates, the test sites' and the starting points with
// the modules' default templates, and those of go-webthings-demo and
// go-webthings-clone when they are next to this repository. Every function a
// template calls must be one go-clue knows from .go-clue/config.json and the
// //go-clue:sig comments, and every call must match its signature.
//
// go-clue has no command that checks templates: the functions it knows come
// from its index (go-clue index), the problems from its language server
// (go-clue lsp), which does not report unknown functions. Its other
// diagnostics, about fields and template contracts, are logged: some are
// limits of go-clue, such as fields of embedded structs.
func TestGoClue(t *testing.T) {
	t.Parallel()

	clue := findGoClue(t)

	projects := []string{clue.moduleDir}

	for _, sibling := range []string{"go-webthings-demo", "go-webthings-clone"} {
		dir := filepath.Join(filepath.Dir(clue.moduleDir), sibling)

		if _, err := os.Stat(filepath.Join(dir, ".go-clue", "config.json")); err == nil {
			projects = append(projects, dir)
		}
	}

	for _, root := range projects {
		t.Run(filepath.Base(root), func(t *testing.T) {
			t.Parallel()

			docs := projectTemplates(t, root)

			if root == clue.moduleDir {
				host := filepath.Join(root, "sitetest", "templates", "page.gohtml")
				docs = append(docs, startingPoints(t, host)...)
			}

			known := clue.functions(t, root)

			for _, doc := range docs {
				unresolved, err := unresolvedFunctions(doc, known)
				if err != nil {
					t.Errorf("%s: %v", doc.Name, err)
					continue
				}

				if len(unresolved) > 0 {
					t.Errorf("%s: go-clue does not know %s", doc.Name, strings.Join(unresolved, ", "))
				}
			}

			reported := clue.diagnostics(t, root, docs)

			for _, doc := range docs {
				for _, diagnostic := range reported[doc.Name] {
					start := diagnostic.Range.Start
					position := fmt.Sprintf("%s:%d:%d", doc.Name, start.Line+1, start.Character+1)

					if signatureProblem(diagnostic.Message) {
						t.Errorf("%s: %s", position, diagnostic.Message)
						continue
					}

					t.Logf("%s: %s", position, diagnostic.Message)
				}
			}
		})
	}
}
