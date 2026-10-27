package db

import "testing"

func TestSQLiteDSN(t *testing.T) {
	defaults := "_foreign_keys=1&_busy_timeout=5000&_txlock=immediate&_time_format=sqlite&_timezone=UTC"

	cases := []struct {
		dsn  string
		want string
	}{
		{"file:site.sqlite", "file:site.sqlite?" + defaults},
		{":memory:", ":memory:?" + defaults},
		{
			// The go-sqlite3 style DSN that older sites use keeps its values.
			"file:site.sqlite?_foreign_keys=on&_busy_timeout=100&_txlock=deferred",
			"file:site.sqlite?_foreign_keys=on&_busy_timeout=100&_txlock=deferred&_time_format=sqlite&_timezone=UTC",
		},
		{
			"file:site.sqlite?_fk=0&_timeout=10&_pragma=journal_mode(WAL)",
			"file:site.sqlite?_fk=0&_timeout=10&_pragma=journal_mode(WAL)&_txlock=immediate&_time_format=sqlite&_timezone=UTC",
		},
		{
			"file:site.sqlite?_pragma=foreign_keys(0)&_pragma=busy_timeout(1)&_timezone=Europe/Amsterdam",
			"file:site.sqlite?_pragma=foreign_keys(0)&_pragma=busy_timeout(1)&_timezone=Europe/Amsterdam&_txlock=immediate&_time_format=sqlite",
		},
	}

	for _, test := range cases {
		if got := sqliteDSN(test.dsn); got != test.want {
			t.Errorf("sqliteDSN(%q)\n got %q\nwant %q", test.dsn, got, test.want)
		}
	}
}
