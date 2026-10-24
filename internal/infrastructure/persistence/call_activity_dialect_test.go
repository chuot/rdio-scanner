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

package persistence

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The hour bucket is the one part of the activity reads that is not the same statement
// in each dialect: timestamps are integer milliseconds, and MySQL answers a DECIMAL for
// `/` between two integers where SQLite and PostgreSQL truncate.

func TestHourBucketExpressionDividesByWholeHoursInEveryDialect(t *testing.T) {
	for _, tc := range []struct {
		dialect Dialect
		// The integer-division form that dialect answers: MySQL and MariaDB need
		// `DIV`, their `/` yields a DECIMAL.
		want string
	}{
		{DialectSqlite, `c."timestamp" / 3600000`},
		{DialectPostgresql, `c."timestamp" / 3600000`},
		{DialectMysql, `c."timestamp" DIV 3600000`},
		{DialectMariadb, `c."timestamp" DIV 3600000`},
	} {
		t.Run(string(tc.dialect), func(t *testing.T) {
			got := hourBucketExpression(tc.dialect, `c."timestamp"`)

			if got != tc.want {
				t.Errorf("hourBucketExpression(%q) = %q, want %q", tc.dialect, got, tc.want)
			}

			// The divisor must stay the same hour the Go side divides by.
			if !strings.Contains(got, "3600000") {
				t.Errorf("hourBucketExpression(%q) = %q, which does not divide by the 3600000 milliseconds an hour counts", tc.dialect, got)
			}
		})
	}
}

// The SQL expression and the Go side that turns a bucket back into an instant must name
// the same hour, at the hour boundary and at the epoch's earliest instant.
func TestHourBucketRoundTripNamesTheSameHourTheDatabaseGroupsBy(t *testing.T) {
	for _, instant := range []time.Time{
		time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 14, 9, 0, 0, 1, time.UTC),
		time.Date(2026, 3, 14, 9, 59, 59, 999_000_000, time.UTC),
		time.Date(2026, 3, 14, 10, 0, 0, 0, time.UTC),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1970, 1, 1, 0, 59, 59, 999_000_000, time.UTC),
	} {
		hour := hourOfBucket(bucketOfHour(instant))

		if !hour.Equal(instant.Truncate(time.Hour)) {
			t.Errorf("the bucket holding %s turns back into %s, want %s",
				instant, hour, instant.Truncate(time.Hour))
		}
	}
}

// The SQLite expression and the Go helper run against the same stored rows, compared
// where they can actually disagree.
func TestCountCallsByHourGroupsByTheSameHourTheGoSideReports(t *testing.T) {
	fixture := newCallsFixture(t)

	base := time.Date(2026, 3, 14, 9, 15, 0, 0, time.UTC)

	fixture.storeCall(t, 10001, 1514000, base)
	fixture.storeCall(t, 10001, 1514001, base.Add(43*time.Minute))
	fixture.storeCall(t, 10001, 1514002, base.Add(44*time.Minute))

	counts, err := fixture.calls.CountCallsByHour(context.Background(), base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountCallsByHour: %v", err)
	}

	if len(counts) != 1 {
		t.Fatalf("CountCallsByHour returned %d buckets, want the 1 hour these three calls fall in: %v", len(counts), counts)
	}

	if want := base.Truncate(time.Hour); !counts[0].Hour.Equal(want) {
		t.Errorf("the bucket begins at %s, want %s", counts[0].Hour, want)
	}

	if counts[0].Count != 3 {
		t.Errorf("the bucket carries %d calls, want 3", counts[0].Count)
	}
}
