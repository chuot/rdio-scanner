package main

import (
	"os"
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
	const path = "../../internal/presentation/clients/admin/domain/log-search.js"

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
