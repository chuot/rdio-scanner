package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"rdio-scanner/internal/domain"
)

// The administrator's log view offers a select of the parts of the server, and the choices it offers are a
// copy of the vocabulary: the server does not publish the list, and a select built from whatever subsystems
// happen to be in today's answer would hide the one the operator is troubleshooting on a day it wrote
// nothing.
//
// A copy that is not checked is how the two drift apart. This is the check: the client's list is the
// vocabulary — the same names, in the order the server lists them, each with a label an operator can read.
// A server that gains a subsystem and does not also give the administrator's log view the way to ask for it
// fails here, and not in front of an operator who could not find the filter.
func TestTheLogViewOffersEverySubsystemTheServerHas(t *testing.T) {
	const path = "../../internal/presentation/admin/frontend/domain/log-search.js"

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is missing: %v\nthe administrator's log view builds its subsystem filter from it", path, err)
	}

	offered := logSourceChoices(t, string(b))

	want := domain.KnownLogSources()

	if len(offered) != len(want) {
		t.Fatalf("the log view offers %d subsystems and the server has %d\n  offered %s\n  the server %s",
			len(offered), len(want), logSourceValues(offered), knownLogSourceNames(want))
	}

	for i, source := range want {
		if offered[i].value != string(source) {
			t.Errorf("the log view's choice %d is %q, want %q: the vocabulary is listed in the order the "+
				"log view shows it, so a mismatch here is a subsystem missing and another moved to take its "+
				"place", i, offered[i].value, source)
		}

		if offered[i].label == "" {
			t.Errorf("the subsystem %q is offered with no label, and a filter choice nobody can read is a "+
				"filter choice nobody uses", source)
		}
	}
}

// TestTheLogDocumentationNamesEverySubsystemTheServerHas is the same pin on the other copy of the
// vocabulary: the operator's page. The document is where an operator learns that `dirwatch` is the thing to
// ask for when recordings stop arriving, so a subsystem the server writes and the document does not name is a
// subsystem nobody can look for.
func TestTheLogDocumentationNamesEverySubsystemTheServerHas(t *testing.T) {
	const path = "../../docs/admin-logs.md"

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is missing: %v\nthe page that tells an operator which subsystem to filter on", path, err)
	}

	document := string(b)

	for _, source := range domain.KnownLogSources() {
		if !strings.Contains(document, "`"+string(source)+"`") {
			t.Errorf("%s is not named in %s\nan operator cannot filter on a subsystem the documentation never "+
				"mentions, and the log view offers it", source, path)
		}
	}
}

// TestEveryWrittenSubsystemHasAWriter holds the vocabulary to the server's own source.
//
// The vocabulary records which names a writer writes and which names exist only so that rows written before
// the source column existed can still be found. The first half of that is a claim about the rest of the
// server, and a claim nothing checks is a comment: a name recorded as written that no writer files under is a
// filter the log view offers and nothing can ever answer, and a name some writer files under that the
// vocabulary does not record is a subsystem an operator has no way to ask for.
//
// Three places name a source without writing an entry, and they are the only places this walk ignores: the
// vocabulary's own declarations, the persistence layer — which stores entries, and files the ones written
// before the column existed by their prefix, but never decides which part of the server a new one came from —
// and the stand-ins the tests wire in.
func TestEveryWrittenSubsystemHasAWriter(t *testing.T) {
	const vocabulary = "../../internal/domain/log.go"

	constant := logSourceConstants(t, vocabulary)

	wrote := logSourcesTheServerWrites(t, constant)

	// A name declared and classified by neither list is a name the log view does not offer and no writer is
	// held to.
	for _, source := range constant {
		if !domain.IsKnownLogSource(source) {
			t.Errorf("%s declares %q, and the vocabulary does not offer it: a name is classified by being put "+
				"in one of the two lists", vocabulary, source)
		}
	}

	for _, source := range domain.KnownLogSources() {
		files := wrote[source]

		switch written := domain.LogSourceIsWritten(source); {
		case written && len(files) == 0:
			t.Errorf("%q is recorded as a part of this server that writes entries, and no writer in the server "+
				"files an entry under it: the log view offers the filter and nothing this server writes can ever "+
				"match it", source)

		case !written && len(files) > 0:
			t.Errorf("%q is recorded as a name only the backfill reaches, and %s file entries under it: a name "+
				"with a writer is a subsystem, not a leftover", source, strings.Join(files, ", "))
		}
	}

	for source, files := range wrote {
		if !domain.IsKnownLogSource(source) {
			t.Errorf("%q is filed under by %s and is not a name in the vocabulary", source, strings.Join(files, ", "))
		}
	}
}

// logSourcesTheServerWrites walks the server's own source and collects, for each name the vocabulary
// declares, the files that name it.
func logSourcesTheServerWrites(t *testing.T, constant map[string]domain.LogSource) map[domain.LogSource][]string {
	t.Helper()

	wrote := map[domain.LogSource][]string{}

	fset := token.NewFileSet()

	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				switch info.Name() {
				// The stand-ins the tests wire in are not writers, and the layer that stores entries is
				// not the one that decides where a new one came from.
				case "fakes", "persistence", "node_modules":
					return filepath.SkipDir
				}

				return nil
			}

			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			for _, name := range logSourcesNamedIn(t, fset, path, constant) {
				wrote[name] = append(wrote[name], strings.TrimPrefix(path, "../../"))
			}

			return nil
		})

		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}

	return wrote
}

// logSourceConstants reads the names out of the vocabulary's own declarations, so that the walk looks for the
// constants the server actually has rather than for a list written down a second time.
func logSourceConstants(t *testing.T, path string) map[string]domain.LogSource {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("%s cannot be read: %v\nthe vocabulary is the one thing the writers, the log view and the "+
			"backfill all have to agree on", path, err)
	}

	constant := map[string]domain.LogSource{}

	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}

		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Values) != 1 {
				continue
			}

			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}

			for _, name := range value.Names {
				// LogSource is the type, and a name is a name only when it says which one.
				if !strings.HasPrefix(name.Name, "LogSource") || name.Name == "LogSource" {
					continue
				}

				constant[name.Name] = domain.LogSource(strings.Trim(literal.Value, `"`))
			}
		}
	}

	if len(constant) == 0 {
		t.Fatalf("%s declares no log source names", path)
	}

	return constant
}

// logSourcesNamedIn reports which source names one file writes entries under. Comments are not code: the
// file is parsed, so a sentence about a subsystem does not count as a writer for it.
func logSourcesNamedIn(t *testing.T, fset *token.FileSet, path string, constant map[string]domain.LogSource) []domain.LogSource {
	t.Helper()

	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("%s cannot be parsed: %v", path, err)
	}

	found := map[domain.LogSource]bool{}

	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "domain" {
			return true
		}

		source, ok := constant[selector.Sel.Name]
		if !ok {
			return true
		}

		found[source] = true

		return true
	})

	named := []domain.LogSource{}

	for _, source := range domain.KnownLogSources() {
		if found[source] {
			named = append(named, source)
		}
	}

	for source := range found {
		if !domain.IsKnownLogSource(source) {
			named = append(named, source)
		}
	}

	return named
}

// logSourceChoice is one entry of the log view's subsystem select.
type logSourceChoice struct {
	value string
	label string
}

// logSourceChoices reads the select's choices out of the client's source. The list is a literal, and the
// point of reading it here is not to parse JavaScript — it is to notice that the two lists are no longer
// the same list.
func logSourceChoices(t *testing.T, source string) []logSourceChoice {
	t.Helper()

	const marker = "export const LOG_SOURCES = ["

	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("the log view's source has no %s: the subsystem filter is not built from the vocabulary", marker)
	}

	block := source[start:]

	end := strings.Index(block, "\n];")
	if end < 0 {
		t.Fatalf("the log view's %s list is not closed", marker)
	}

	block = block[:end]

	matches := regexp.MustCompile(`\['([a-z]+)', '([^']*)'\]`).FindAllStringSubmatch(block, -1)

	if len(matches) == 0 {
		t.Fatalf("the log view's subsystem list holds no choices:\n%s", block)
	}

	out := make([]logSourceChoice, 0, len(matches))

	for _, match := range matches {
		out = append(out, logSourceChoice{value: match[1], label: match[2]})
	}

	return out
}

func logSourceValues(choices []logSourceChoice) []string {
	out := make([]string, 0, len(choices))

	for _, choice := range choices {
		out = append(out, choice.value)
	}

	return out
}

func knownLogSourceNames(sources []domain.LogSource) []string {
	out := make([]string, 0, len(sources))

	for _, source := range sources {
		out = append(out, string(source))
	}

	return out
}
