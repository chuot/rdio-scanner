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
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

// The differences this harness expects to see between the two servers, and nothing else.
//
// Each one is a scenario, a step or a table, and a description of the difference that has to be there. A
// difference that is not declared fails the run. A declaration whose difference is not there also fails it:
// an approved divergence that quietly stopped applying is news, and the harness is how it gets heard.
//
// The five are the approved divergences recorded in
// docs/specs/FEAT-003-server-ng-refactor/decision-preserved-quirks.md.

export const expectations = [
  {
    id: 'Q51',
    kind: 'step',
    scenario: 'scoped-upload',
    step: 'a-scoped-key-uploads-a-call',
    summary:
      'the legacy server checks an apikey scope before the call has a system, dereferences the missing system, and the request ends with no reply at all; server-ng resolves the references first and answers',
    check: ({ legacy, ng }) => {
      if (legacy.error === undefined) {
        return `the legacy server answered the scoped upload (${legacy.status ?? 'no answer'}), want no answer at all`;
      }

      if (ng.status !== 200 || ng.body !== 'Call imported successfully.\n') {
        return `server-ng answered the scoped upload ${ng.status ?? ng.error} ${JSON.stringify(ng.body ?? '')}, want 200 and the success notice`;
      }

      return true;
    },
  },
  {
    id: 'Q51',
    kind: 'dump',
    scenario: 'scoped-upload',
    tables: ['calls', 'callFrequencies', 'callPatches', 'callUnits', 'logs'],
    summary: 'the call the legacy server never ingested, and everything its ingest would have written beside it, are in server-ng',
    check: ({ table, legacy, ng }) => {
      if (table === 'logs') {
        const mentions = (rows) => rows.filter((row) => row.message.startsWith('newcall:')).length;

        if (mentions(legacy) !== 0) {
          return 'the legacy server logged the notice for a call it never ingested';
        }

        if (mentions(ng) !== 1) {
          return 'server-ng did not log the notice for the call it ingested';
        }

        return true;
      }

      if (legacy.length !== 0) {
        return `the legacy server stored ${legacy.length} rows from an upload it never answered, want none`;
      }

      if (table === 'calls' && (ng.length !== 1 || ng[0].audioFilename !== 'capture.wav')) {
        return `server-ng stored ${JSON.stringify(ng)}, want the one call it ingested`;
      }

      if (ng.length === 0) {
        return `server-ng stored nothing beside the call it ingested`;
      }

      return true;
    },
  },

  {
    id: 'Q56',
    kind: 'timing',
    scenario: 'missing-call-fetch',
    step: 'a-write-after-the-missing-call-fetch',
    summary:
      'the legacy server leaves its transaction open when the call it was asked for is not there, and the next write waits out the ten-second busy timeout; server-ng rolls back and answers at once',
    check: ({ legacy, ng }) => {
      if (legacy.status !== ng.status) {
        return `the two servers answered the same login differently: ${legacy.status} against ${ng.status}`;
      }

      if (legacy.elapsedMs < 5000) {
        return `the legacy server's write took ${legacy.elapsedMs}ms, want the busy timeout it blocks on`;
      }

      if (ng.elapsedMs > 2000) {
        return `server-ng's write took ${ng.elapsedMs}ms, want it to answer without waiting`;
      }

      return true;
    },
  },
  {
    id: 'Q56',
    kind: 'dump',
    scenario: 'missing-call-fetch',
    table: 'logs',
    summary: 'the log row the legacy server could not write while its database was locked is in server-ng',
    check: ({ legacy, ng }) => {
      const mentions = (rows) => rows.filter((row) => row.message.includes('invalid login attempt')).length;

      if (mentions(legacy) !== 0) {
        return `the legacy server stored the login attempt it was blocked from writing, want it to have failed the write`;
      }

      if (mentions(ng) !== 1) {
        return `server-ng did not store the login attempt, want the one row it writes`;
      }

      return true;
    },
  },

  {
    id: 'Q58',
    kind: 'step',
    scenario: 'admin-rest',
    step: 'the-whole-configuration',
    summary: 'the legacy server reports every site as belonging to no system; server-ng gives it the system it was found under',
    paths: [/^body\.config\.systems\[\d+\]\.sites\[\d+\]\.systemId$/],
  },
  {
    id: 'Q58',
    kind: 'step',
    scenario: 'admin-rest',
    step: 'the-systems-section',
    summary: 'the legacy server reports every site as belonging to no system; server-ng gives it the system it was found under',
    paths: [/^body\[\d+\]\.sites\[\d+\]\.systemId$/],
  },
  {
    id: 'Q58',
    kind: 'step',
    scenario: 'admin-config-socket',
    step: 'the-configuration-is-pushed',
    summary: 'the legacy server reports every site as belonging to no system; server-ng gives it the system it was found under',
    paths: [/^frame\.systems\[\d+\]\.sites\[\d+\]\.systemId$/],
  },

  {
    id: 'Q61',
    kind: 'step',
    scenario: 'scope-normalization',
    step: 'an-access-with-an-odd-scope-is-saved',
    summary: 'the legacy server answers a save with the scope it could not use, exactly as it was submitted',
    paths: [/^body\[0\]\.systems\[0\]\.talkgroups$/],
  },
  {
    id: 'Q61',
    kind: 'step',
    scenario: 'scope-normalization',
    step: 'an-apikey-with-an-odd-scope-is-saved',
    summary: 'the legacy server answers a save with the scope it could not use, exactly as it was submitted',
    paths: [/^body\[1\]\.systems\[0\]\.talkgroups$/],
  },
  {
    id: 'Q61',
    kind: 'step',
    scenario: 'scope-normalization',
    step: 'the-access-with-the-odd-scope',
    summary: 'the legacy server stores a scope it could not use exactly as it was submitted; server-ng stores the scope it understood',
    paths: [/^body\[0\]\.systems\[0\]\.talkgroups$/],
  },
  {
    id: 'Q61',
    kind: 'step',
    scenario: 'scope-normalization',
    step: 'the-apikey-with-the-odd-scope',
    summary: 'the legacy server stores a scope it could not use exactly as it was submitted; server-ng stores the scope it understood',
    paths: [/^body\[1\]\.systems\[0\]\.talkgroups$/],
  },
  {
    id: 'Q61',
    kind: 'dump',
    scenario: 'scope-normalization',
    table: 'accesses',
    summary: 'the stored scope differs the same way the answered one does',
    paths: [/^\[\d+\]\.systems$/],
  },
  {
    id: 'Q61',
    kind: 'dump',
    scenario: 'scope-normalization',
    table: 'apikeys',
    summary: 'the stored scope differs the same way the answered one does',
    paths: [/^\[\d+\]\.systems$/],
  },

  {
    id: 'Q62',
    kind: 'step',
    scenario: 'access-limit',
    step: 'the-third-listener-is-refused',
    summary:
      'the legacy server counts the sessions holding an access by the in-memory object it holds, so after the access is saved again its limit stops counting and the third connection is let in; server-ng counts the access itself and refuses',
    check: ({ legacy, ng }) => {
      if (legacy.command !== 'CFG') {
        return `the legacy server answered the third listener with ${legacy.command}, want it to let the connection in`;
      }

      if (ng.command !== 'MAX') {
        return `server-ng answered the third listener with ${ng.command}, want the refusal it gives`;
      }

      return true;
    },
  },
  {
    id: 'Q62',
    kind: 'dump',
    scenario: 'access-limit',
    table: 'logs',
    summary: 'server-ng wrote the log line for a refusal the legacy server never made',
    check: ({ legacy, ng }) => {
      const mentions = (rows) => rows.filter((row) => row.message.includes('too many concurrent connections')).length;

      if (mentions(legacy) !== 0) {
        return `the legacy server logged a refusal it did not make, want none`;
      }

      if (mentions(ng) !== 1) {
        return `server-ng did not log the refusal it made, want the one line it writes`;
      }

      return true;
    },
  },
];
