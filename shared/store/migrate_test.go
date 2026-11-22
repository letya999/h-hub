package store

import (
	"testing"
	"testing/fstest"

	"github.com/letya999/h-hub/shared/testkit"
)

func migFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out["migrations/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

func TestReadMigrationsOrder(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.store.numbered-sql")
	fsys := migFS(map[string]string{
		"0002_two.sql": "SELECT 2",
		"0001_one.sql": "SELECT 1",
	})
	migs, err := readMigrations(fsys, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) != 2 || migs[0].num != 1 || migs[1].num != 2 {
		t.Fatalf("order=%+v", migs)
	}
}

func TestReadMigrationsRejects(t *testing.T) {
	t.Parallel()
	testkit.Req(t, "adr.store.numbered-sql")
	cases := map[string]map[string]string{
		"gap":     {"0001_a.sql": "SELECT 1", "0003_b.sql": "SELECT 3"},
		"badname": {"init.sql": "SELECT 1"},
		"zero":    {"0000_a.sql": "SELECT 1"},
	}
	for name, files := range cases {
		if _, err := readMigrations(migFS(files), "migrations"); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}
