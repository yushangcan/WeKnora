package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDatasetIterateUsesStableQueryOrder(t *testing.T) {
	dataset := dataset{
		queries: map[int64]string{2: "second", 1: "first"},
		corpus:  map[int64]string{10: "passage"},
		answers: map[int64]string{20: "answer"},
		qrels:   map[int64][]int64{1: {10}, 2: {10}},
		qas:     map[int64]int64{1: 20, 2: 20},
	}

	pairs := dataset.Iterate()
	if len(pairs) != 2 || pairs[0].QID != 1 || pairs[1].QID != 2 {
		t.Fatalf("cases are not sorted by query ID: %#v", pairs)
	}
}

func TestFingerprintDatasetChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	names := []string{"a", "b"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}

	first, err := fingerprintDataset(dir, names)
	if err != nil {
		t.Fatalf("fingerprint first dataset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("changed"), 0o600); err != nil {
		t.Fatalf("change fixture: %v", err)
	}
	second, err := fingerprintDataset(dir, names)
	if err != nil {
		t.Fatalf("fingerprint second dataset: %v", err)
	}
	if first == second {
		t.Fatal("dataset fingerprint did not change after file content changed")
	}
}

func TestLoadDatasetRejectsUnknownID(t *testing.T) {
	service := NewDatasetService()
	if _, err := service.LoadDataset(context.Background(), "unknown"); err == nil {
		t.Fatal("expected unknown dataset ID to be rejected")
	}
}
