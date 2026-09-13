package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// The bin screen reads bin.items.length, so an empty bin must send an empty
// list rather than dropping the field.
func TestEmptyBinJSONIncludesItems(t *testing.T) {
	s := newTestStore(t)
	b, err := s.CreateBin(BinInput{Name: "Empty"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"items":[]`) {
		t.Fatalf("empty bin JSON = %s, want it to contain \"items\":[]", data)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "test.db"), filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestBinLifecycle(t *testing.T) {
	s := newTestStore(t)

	b, err := s.CreateBin(BinInput{Name: "Holiday lights", Location: "Garage"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if b.Code != "BIN-00001" {
		t.Fatalf("first code = %q, want BIN-00001", b.Code)
	}

	// A lower-case scan of an existing label must not create a duplicate.
	if _, err := s.CreateBin(BinInput{Code: "bin-00001"}, "tester"); err != ErrExists {
		t.Fatalf("duplicate code: err = %v, want ErrExists", err)
	}

	if _, err := s.AddItem(b.Code, "AA batteries", 4, "tester"); err != nil {
		t.Fatal(err)
	}
	b, err = s.AddItem(b.Code, "aa batteries", 3, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items) != 1 || b.Items[0].Quantity != 7 {
		t.Fatalf("adding an existing item should merge quantities, got %+v", b.Items)
	}

	loc := "Basement rack 3"
	if b, err = s.UpdateBin("bin-00001", BinPatch{Location: &loc}, "tester"); err != nil || b.Location != loc {
		t.Fatalf("move: err = %v, bin = %+v", err, b)
	}

	found, err := s.ListBins("batteries", "")
	if err != nil || len(found) != 1 || len(found[0].MatchedItems) != 1 {
		t.Fatalf("item search: err = %v, result = %+v", err, found)
	}
	if none, _ := s.ListBins("batteries", "Garage"); len(none) != 0 {
		t.Fatalf("location filter returned %d bins, want 0", len(none))
	}

	// Created, added, added again, moved.
	if hist, err := s.History(b.Code, "", 50, 0); err != nil || len(hist) != 4 {
		t.Fatalf("history: err = %v, %d entries, want 4", err, len(hist))
	}

	codes, err := s.ReserveCodes(2, "tester")
	if err != nil || len(codes) != 2 || codes[0] != "BIN-00002" || codes[1] != "BIN-00003" {
		t.Fatalf("reserve: err = %v, codes = %v", err, codes)
	}
	next, err := s.CreateBin(BinInput{}, "tester")
	if err != nil || next.Code != "BIN-00004" {
		t.Fatalf("code after reserving: err = %v, code = %q, want BIN-00004", err, next.Code)
	}

	if _, err := s.CreateBin(BinInput{Code: "BIN/1"}, "tester"); err == nil {
		t.Fatal("expected a code with / to be rejected")
	}

	if err := s.DeleteBin(b.Code, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBin(b.Code); err != ErrNotFound {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestBackupRestore(t *testing.T) {
	s := newTestStore(t)
	b, err := s.CreateBin(BinInput{Name: "Keep me"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.BackupNow("manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBin(b.Code, "tester"); err != nil {
		t.Fatal(err)
	}

	if err := s.RestoreBackup(info.Name, "tester"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBin(b.Code)
	if err != nil || got.Name != "Keep me" {
		t.Fatalf("after restore: err = %v, bin = %+v", err, got)
	}

	list, err := s.ListBackups()
	if err != nil || len(list) != 2 {
		t.Fatalf("want the manual and before-restore backups, got %d (err %v)", len(list), err)
	}
	if err := s.RestoreBackup("../test.db", "tester"); err == nil {
		t.Fatal("expected a path outside the backups folder to be rejected")
	}
}
