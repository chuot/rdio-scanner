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

package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The administrator's standing questions, packaged so a client starts a session with them. A prompt
// is not a capability: it asks a question in the operator's words and names the capabilities the
// assistant should reach for, in the order that answers it. An assistant that has to work out the
// sequence itself is an assistant that will work out a different one.

func TestTheAdministratorsStandingQuestionsAreInTheCatalog(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	listed, err := s.ListPrompts(context.Background(), &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("list the prompts: %v", err)
	}

	got := []string{}
	for _, prompt := range listed.Prompts {
		got = append(got, prompt.Name)

		if prompt.Description == "" {
			t.Fatalf("%s carries no description for an assistant to choose from", prompt.Name)
		}
	}

	want := standingQuestionNames()

	assertSame(t, "the administrator's prompts", want, got)
}

// Each prompt declares the arguments the contract names it taking, and says what each one is for.
func TestEachStandingQuestionDeclaresItsArguments(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	listed, err := s.ListPrompts(context.Background(), &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("list the prompts: %v", err)
	}

	want := map[string][]string{
		"daily_health_review":           {"date"},
		"troubleshoot_silent_talkgroup": {"system", "talkgroup", "since"},
		"explain_recent_errors":         {"hours"},
		"audit_access_codes":            {},
		"onboard_listener":              {"ident", "systems", "expires"},
	}

	if len(listed.Prompts) != len(want) {
		t.Fatalf("the surface packages %d standing questions, want the %d the inventory names: %+v",
			len(listed.Prompts), len(want), listed.Prompts)
	}

	for _, prompt := range listed.Prompts {
		arguments, ok := want[prompt.Name]
		if !ok {
			t.Fatalf("%s is not a standing question this surface was asked to package", prompt.Name)
		}

		got := []string{}
		for _, argument := range prompt.Arguments {
			got = append(got, argument.Name)

			if argument.Description == "" {
				t.Fatalf("%s declares argument %s with no description", prompt.Name, argument.Name)
			}
		}

		assertSame(t, prompt.Name+" arguments", arguments, got)
	}
}

func TestDailyHealthReviewAsksForTheReviewAndTheLinesBehindIt(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	day := fixtureTime.Format("2006-01-02")

	text := promptText(t, s, "daily_health_review", map[string]string{"date": day})

	for _, want := range []string{"analyze_health", "search_logs", "warn", "error", day} {
		if !strings.Contains(text, want) {
			t.Fatalf("the health review says nothing about %s: %s", want, text)
		}
	}
}

func TestTroubleshootSilentTalkgroupAsksForTheDiagnosisAndTwoLikelyFixes(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	text := promptText(t, s, "troubleshoot_silent_talkgroup", map[string]string{
		"system":    "10001",
		"talkgroup": "1514000",
	})

	for _, want := range []string{"diagnose_feed_problem", "10001", "1514000", "fix"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatalf("the talkgroup question says nothing about %s: %s", want, text)
		}
	}
}

func TestExplainRecentErrorsAsksForTheWindowThenEachDistinctError(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	text := promptText(t, s, "explain_recent_errors", map[string]string{"hours": "6"})

	for _, want := range []string{"search_logs", "explain_error", "error"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the recent-errors question says nothing about %s: %s", want, text)
		}
	}
}

// The access audit reads the masked inventory. The inventory carries no record of when a code was
// last used, and the prompt says where that is answered instead of pretending the inventory knows.
func TestAuditAccessCodesAsksForTheInventoryAndNamesWhereUseIsAnswered(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	text := promptText(t, s, "audit_access_codes", nil)

	for _, want := range []string{accessURI, "expired", "scope", "search_logs"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the access audit says nothing about %s: %s", want, text)
		}
	}
}

// The listener onboarding produces the exact write call, with the scope spelled out, and says the
// change happens in the confirmed round.
func TestOnboardListenerProducesTheExactWriteCallWithTheScopeSpelledOut(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	text := promptText(t, s, "onboard_listener", map[string]string{
		"ident":   "dispatch-desk",
		"systems": "10001:1514000,10002:*",
		"expires": "1798761600",
	})

	for _, want := range []string{"manage_access_user", "dispatch-desk", "10001", "1514000", "confirm"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatalf("the onboarding question says nothing about %s: %s", want, text)
		}
	}
}

// A standing question that was not given the argument it cannot do without says which one is
// missing. The protocol does not check the declared arguments, so the question checks its own.
func TestAStandingQuestionRefusesAnArgumentItCannotDoWithout(t *testing.T) {
	s := newCallerSession(t, Caller{Tier: TierFull})

	for _, probe := range []struct {
		name string
		args map[string]string
		want string
	}{
		{"troubleshoot_silent_talkgroup", map[string]string{"talkgroup": "1514000"}, "system"},
		{"troubleshoot_silent_talkgroup", map[string]string{"system": "10001"}, "talkgroup"},
		{"onboard_listener", map[string]string{"systems": "10001:*", "expires": "1798761600"}, "ident"},
	} {
		_, err := s.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: probe.name, Arguments: probe.args})
		if err == nil {
			t.Fatalf("%s answered without its %s argument", probe.name, probe.want)
		}

		if !strings.Contains(err.Error(), probe.want) {
			t.Fatalf("%s refused with %q, which does not name the argument it is missing", probe.name, err.Error())
		}
	}
}

// The standing questions are the administrator's. The read-only tier's catalog names none, and
// asking for one by name answers the protocol's own answer and nothing about the deployment.
func TestTheReadOnlyTierSeesNoStandingQuestion(t *testing.T) {
	s := newCallerSession(t, readScoped())

	listed, err := s.ListPrompts(context.Background(), &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("list the prompts: %v", err)
	}

	if len(listed.Prompts) != 0 {
		t.Fatalf("the read-only tier is handed %d standing questions: %+v", len(listed.Prompts), listed.Prompts)
	}

	_, err = s.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "daily_health_review"})
	if err == nil {
		t.Fatal("daily_health_review answered an access-code caller")
	}
}

// promptText is what a standing question answers, as one block of text: the messages it returns,
// joined, because an assistant reads them as one instruction.
func promptText(t *testing.T, s *session, name string, args map[string]string) string {
	t.Helper()

	got, err := s.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("ask for %s: %v", name, err)
	}

	if len(got.Messages) == 0 {
		t.Fatalf("%s returned no messages", name)
	}

	lines := []string{got.Description}

	for _, message := range got.Messages {
		if message.Role != "user" {
			t.Fatalf("%s returned a message with role %q, want the user's turn", name, message.Role)
		}

		text, ok := message.Content.(*mcp.TextContent)
		if !ok {
			t.Fatalf("%s returned a %T message, want text", name, message.Content)
		}

		if text.Text == "" {
			t.Fatalf("%s returned an empty message", name)
		}

		lines = append(lines, text.Text)
	}

	answer := strings.Join(lines, "\n")

	if len(answer) < 120 {
		t.Fatalf("%s answers %d characters, which is not an instruction an assistant can follow: %s",
			name, len(answer), answer)
	}

	return answer
}
