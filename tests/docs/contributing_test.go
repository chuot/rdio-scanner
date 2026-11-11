// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// these checks hold the documents an outside contributor reads before writing anything —
// the contribution guide, the code of conduct, and the form a pull request opens with —
// to the commands, links, and rules the project actually runs.

// compileGuideFile and securityFile are the two root documents the community files agree
// with rather than restate.
const (
	compileGuideFile = "COMPILING.md"
	securityFile     = "SECURITY.md"
)

// communityDocuments are the documents a contributor reads before writing code, each named
// by its path parts from the repository root.
var communityDocuments = [][]string{
	{"CONTRIBUTING.md"},
	{"CODE_OF_CONDUCT.md"},
	{".github", "pull_request_template.md"},
}

// communityBody reads one of those documents. An absent one fails the check that needed it
// by naming the document, since the absence is the finding.
func communityBody(t *testing.T, parts ...string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatalf("%s does not resolve: %v", filepath.Join(parts...), err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the repository carries no %s, so nothing holds the rule it is meant to hold", filepath.Join(parts...))
	}

	return string(body)
}

// guideText reads the contribution guide.
func guideText(t *testing.T) string {
	t.Helper()

	return communityBody(t, "CONTRIBUTING.md")
}

// folded leaves a document's words without line wrapping, so a rule carried over several
// lines still reads as one rule.
func folded(body string) string {
	return strings.ToLower(strings.Join(strings.Fields(body), " "))
}

// commandStarts are the lines a contributor runs on the source tree.
var commandStarts = []string{"go run ./cmd/build ", "go test ./...", "npm test"}

// guideCommands returns every command the guide states as its own line, each with its
// trailing note removed, in the order the guide states them.
func guideCommands(t *testing.T) []string {
	t.Helper()

	found := []string{}

	for _, raw := range strings.Split(guideText(t), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		named := false
		for _, start := range commandStarts {
			if strings.HasPrefix(line, start) {
				named = true

				break
			}
		}

		if !named {
			continue
		}

		if at := strings.Index(line, "#"); at >= 0 {
			line = strings.TrimSpace(line[:at])
		}

		found = append(found, line)
	}

	return found
}

// contactAddress is the shape of the address a private report goes to.
var contactAddress = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// publishedContacts are the addresses the security policy already answers, which are the
// contacts every other document points a private report to.
func publishedContacts(t *testing.T) []string {
	t.Helper()

	seen := []string{}

	for _, found := range contactAddress.FindAllString(communityBody(t, securityFile), -1) {
		listed := false

		for _, already := range seen {
			if already == found {
				listed = true

				break
			}
		}

		if !listed {
			seen = append(seen, found)
		}
	}

	if len(seen) == 0 {
		t.Fatalf("%s publishes no contact address, so no document can direct a private report", securityFile)
	}

	return seen
}

// the guide carries the run the project runs its suites with and both ways of running the
// same suites by hand, so a contributor reaches the suites without being told twice.
func TestTheGuideNamesTheWholeRunAndBothHandRuns(t *testing.T) {
	body := guideText(t)

	missing := []string{}

	for _, need := range []string{"go run ./cmd/build test", "go test ./...", "npm test"} {
		if !strings.Contains(body, need) {
			missing = append(missing, need)
		}
	}

	if len(missing) > 0 {
		t.Errorf("the guide never names %s, so a contributor has no route to the project's suites", strings.Join(missing, ", "))
	}
}

// a command the guide states and the compile guide does not is a command nobody checks, so
// each line the guide states is read against the compile guide.
func TestEveryCommandTheGuideStatesIsOneTheCompileGuideStates(t *testing.T) {
	commands := guideCommands(t)

	if len(commands) < 4 {
		t.Fatalf("the guide states %d command line(s), fewer than the four a contributor runs while working on the tree: %s",
			len(commands), strings.Join(commands, "; "))
	}

	compileGuide := communityBody(t, compileGuideFile)

	missing := []string{}

	for _, command := range commands {
		if !strings.Contains(compileGuide, command) {
			missing = append(missing, command)
		}
	}

	if len(missing) > 0 {
		t.Errorf("%s never states %s, so the guide names a route the build does not take", compileGuideFile, strings.Join(missing, "; "))
	}
}

// the guide may name only a subcommand the build tool answers for, which is read from the
// constants it dispatches on.
var subcommandName = regexp.MustCompile(`sub[A-Za-z]+\s*=\s*"([a-z]+)"`)

func TestEveryBuildSubcommandTheGuideNamesIsOneTheBuildToolImplements(t *testing.T) {
	commands := guideCommands(t)

	body := repoFile(t, "cmd", "build", "commands.go")

	implemented := map[string]bool{}

	for _, found := range subcommandName.FindAllStringSubmatch(body, -1) {
		implemented[found[1]] = true
	}

	if len(implemented) == 0 {
		t.Fatalf("cmd/build/commands.go states no subcommand constant, so nothing the guide names can be checked against it")
	}

	named := 0
	unknown := []string{}

	for _, command := range commands {
		rest := strings.TrimPrefix(command, "go run ./cmd/build ")
		if rest == command {
			continue
		}

		named++

		fields := strings.Fields(rest)
		if len(fields) == 0 || !implemented[fields[0]] {
			unknown = append(unknown, command)
		}
	}

	if named == 0 {
		t.Fatalf("the guide names no build subcommand at all, so this check proves nothing")
	}

	if len(unknown) > 0 {
		t.Errorf("the guide states %d build line(s) the build tool does not implement: %s", len(unknown), strings.Join(unknown, "; "))
	}
}

// guideLinkFiles are the root documents the guide points a contributor at instead of
// restating.
var guideLinkFiles = []string{compileGuideFile, securityFile, "ICAA.md", "CODE_OF_CONDUCT.md"}

func TestTheGuideLinksTheDocumentsAContributorWorksFrom(t *testing.T) {
	body := guideText(t)

	missing := []string{}

	for _, name := range guideLinkFiles {
		if !strings.Contains(body, "](./"+name+")") {
			missing = append(missing, "./"+name)
		}
	}

	if len(missing) > 0 {
		t.Errorf("the guide never links %s, so a contributor is left to find those documents on their own", strings.Join(missing, ", "))
	}
}

func TestTheCodeOfConductGivesTheContactTheProjectAnswers(t *testing.T) {
	lowered := folded(communityBody(t, "CODE_OF_CONDUCT.md"))

	if strings.Contains(lowered, "insert contact method") {
		t.Errorf("the code of conduct still carries the placeholder contact its adopted text leaves to fill in")
	}

	for _, address := range publishedContacts(t) {
		if !strings.Contains(lowered, strings.ToLower(address)) {
			t.Errorf("the code of conduct does not name the address %s already answers: %s", securityFile, address)
		}
	}
}

// the adopted conduct text is kept whole and named, so a reader can compare it against the
// text the project says it adopted.
func TestTheCodeOfConductKeepsTheTextItIsAdaptedFrom(t *testing.T) {
	lowered := folded(communityBody(t, "CODE_OF_CONDUCT.md"))

	missing := []string{}

	for _, need := range []struct {
		what   string
		phrase string
	}{
		{"the document it is adapted from", "contributor covenant"},
		{"the version adopted", "version 2.1"},
		{"where that version is published", "contributor-covenant.org/version/2/1"},
		{"the attribution section it requires", "## attribution"},
		{"the enforcement ladder the guidelines come from", "mozilla"},
		{"the license the adopted text is carried under", "creative commons attribution 4.0"},
	} {
		if !strings.Contains(lowered, need.phrase) {
			missing = append(missing, need.what)
		}
	}

	if len(missing) > 0 {
		t.Errorf("the code of conduct carries none of %s, so it does not read as the adaptation it is meant to be", strings.Join(missing, ", "))
	}
}

// the form a pull request opens with asks the four answers a review cannot do without, and
// hides none of them behind a comment a contributor has to delete first.
func TestThePullRequestTemplateAsksWhatAReviewNeeds(t *testing.T) {
	body := communityBody(t, ".github", "pull_request_template.md")
	lowered := folded(body)

	missing := []string{}

	for _, need := range []struct {
		what   string
		phrase string
	}{
		{"what the change does", "what does this change do"},
		{"which issue the change carries", "which issue"},
		{"how the change was tested", "how was it tested"},
		{"the whole suite run a contributor can name", "go run ./cmd/build test"},
		{"what the maintainer has to add", "what does the change need"},
		{"the contributor agreement", "individual contributor assignment agreement"},
		{"the code of conduct", "code of conduct"},
		{"a box to agree to", "- [ ]"},
	} {
		if !strings.Contains(lowered, need.phrase) {
			missing = append(missing, need.what)
		}
	}

	if len(missing) > 0 {
		t.Errorf("the pull request template asks for none of %s", strings.Join(missing, ", "))
	}

	if strings.Contains(body, "<!--") {
		t.Errorf("the pull request template hides its prompts behind a comment block a contributor has to delete before submitting")
	}
}

// the issue-first rule is stated once, with its reason. The refusals it used to be repeated
// with are refused here too, so the rule cannot arrive a second time under another name.
var issueFirstRule = "without an assigned issue"

var issueFirstRuleCopies = []string{
	"random pull requests",
	"previously opened and assigned",
	"not related to a previously opened",
	"reject if no issue",
}

func TestTheIssueFirstRuleIsStatedOnceInTheGuide(t *testing.T) {
	lowered := folded(guideText(t))

	at := strings.Count(lowered, issueFirstRule)
	if at != 1 {
		t.Errorf("the guide states the issue-first rule %d time(s) as %q, want exactly one statement of it", at, issueFirstRule)
	}

	for _, copy := range issueFirstRuleCopies {
		if at := strings.Count(lowered, copy); at > 0 {
			t.Errorf("the guide states the issue-first rule %d time(s) again as %q", at, copy)
		}
	}
}

// the community documents are read by the same wording guard the rest of the published
// documents are read by, so none of them restates a position the project withdrew.
func TestNoCommunityFileStatesTheWithdrawnPosition(t *testing.T) {
	for _, parts := range communityDocuments {
		name := filepath.Join(parts...)
		lowered := folded(communityBody(t, parts...))

		for _, retired := range retiredWordings {
			if strings.Contains(lowered, retired.phrase) {
				t.Errorf("%s states %q, which %s", name, retired.phrase, retired.claim)
			}
		}
	}
}

// a document added at the repository root joins the root documents the wording guard
// reads, or it sits outside the only mechanism that keeps a withdrawn position out of
// print.
var guardedRootDocuments = regexp.MustCompile(`(?s)guardFiles = \[\]string\{(.*?)\}`)

func TestTheWordingGuardReadsTheCodeOfConduct(t *testing.T) {
	body := repoFile(t, "tests", "docs", "licence_test.go")

	found := guardedRootDocuments.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("the wording guard's list of root documents does not read")
	}

	if !strings.Contains(found[1], `"CODE_OF_CONDUCT.md"`) {
		t.Errorf("the wording guard reads no CODE_OF_CONDUCT.md, so a withdrawn position can be restated there without a failing test")
	}
}

// The pull request template is read twice over: as a file under .github/, which GitHub
// renders as though it sat at the repository root, and as the body it fills in, where no
// path relative to the repository resolves at all. A relative link there is a link every
// contributor is shown broken, so the documents the form asks a person to read are named by
// their published addresses.
var (
	linkAddresses   = regexp.MustCompile(`\]\(([^)\s]+)`)
	linkDefinitions = regexp.MustCompile(`(?m)^\[[^\]]{1,40}\]:\s*(\S+)`)
	publishedBlob   = regexp.MustCompile(`^https://github\.com/chuot/rdio-scanner/blob/[^/]+/(\S+)`)
	publishedBranch = regexp.MustCompile(`^https://github\.com/chuot/rdio-scanner/tree/[^/]+/?(\S*)`)
)

func TestThePullRequestTemplateReachesTheDocumentsItNames(t *testing.T) {
	const name = ".github/pull_request_template.md"

	body := communityBody(t, ".github", "pull_request_template.md")
	root := []string{"..", ".."}
	targets := []string{}

	for _, match := range linkAddresses.FindAllStringSubmatch(body, -1) {
		targets = append(targets, match[1])
	}

	for _, match := range linkDefinitions.FindAllStringSubmatch(body, -1) {
		targets = append(targets, match[1])
	}

	reached := 0

	for _, address := range targets {
		if strings.HasPrefix(address, "./") || strings.HasPrefix(address, "../") || strings.HasPrefix(address, "/") {
			t.Errorf("%s links %s by a path relative to the repository, which neither the form nor a pull request written from it resolves", name, address)
			continue
		}

		named := publishedBlob.FindStringSubmatch(address)
		if named == nil {
			named = publishedBranch.FindStringSubmatch(address)
		}
		if named == nil {
			continue
		}

		reached++

		if strings.Contains(named[1], "..") {
			t.Errorf("%s names a path that climbs out of the repository", address)
			continue
		}

		parts := append(root, strings.Split(named[1], "/")...)
		if _, err := os.Stat(filepath.Join(parts...)); err != nil {
			t.Errorf("%s names %s, which this repository does not carry", address, named[1])
		}
	}

	if reached == 0 {
		t.Errorf("%s reaches none of this repository's own documents by an address a pull request body can open, so what the form asks a contributor to read cannot be read from it", name)
	}
}
