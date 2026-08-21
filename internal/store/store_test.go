package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConsumeOneTimeConcurrent(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h := []byte("hash")
	s.DB.Exec(`INSERT INTO invites(id,token_hash,expires_at,created_at) VALUES('i',?,?,?)`, h, time.Now().Add(time.Hour).Unix(), time.Now().Unix())
	var wg sync.WaitGroup
	wins := 0
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.ConsumeOneTime(context.Background(), "invites", h, time.Now()) == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
}
func TestPragmasAndTables(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var fk int
	if e = s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); e != nil || fk != 1 {
		t.Fatal(fk, e)
	}
	var n int
	if e = s.DB.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
