package db

import "testing"

func TestSplit(t *testing.T) {
	got := split("-- header\nCREATE TABLE a(\n  id INT\n);\n\nCREATE INDEX i ON a(id);\n")
	if len(got) != 2 || got[1] != "CREATE INDEX i ON a(id)" {
		t.Fatalf("%q", got)
	}
}
