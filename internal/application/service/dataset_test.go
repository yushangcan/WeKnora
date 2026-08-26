package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func validDatasetRows() ([]TextInfo, []TextInfo, []TextInfo, []RelsInfo, []QaInfo) {
	return []TextInfo{{ID: 1, Text: "question"}},
		[]TextInfo{{ID: 10, Text: "relevant passage"}, {ID: 99, Text: "distractor passage"}},
		[]TextInfo{{ID: 20, Text: "answer"}},
		[]RelsInfo{{QID: 1, PID: 10}},
		[]QaInfo{{QID: 1, AID: 20}}
}

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

func TestDefaultDatasetFilesPassValidation(t *testing.T) {
	datasetDir := filepath.Join("..", "..", "..", "dataset", "samples")
	dataset, err := loadDefaultDataset(datasetDir)
	if err != nil {
		t.Fatalf("load default dataset: %v", err)
	}
	if len(dataset.EvaluationCorpus()) == 0 || len(dataset.Iterate()) == 0 {
		t.Fatal("default dataset must contain corpus passages and evaluation cases")
	}
}

func TestBuildDatasetPreservesCompleteCorpusInStableOrder(t *testing.T) {
	queries, _, answers, qrels, qas := validDatasetRows()
	corpus := []TextInfo{
		{ID: 99, Text: "distractor passage"},
		{ID: 10, Text: "relevant passage"},
	}
	dataset, err := buildDataset(queries, corpus, answers, qrels, qas)
	if err != nil {
		t.Fatalf("build dataset: %v", err)
	}

	passages := dataset.EvaluationCorpus()
	if len(passages) != 2 {
		t.Fatalf("corpus length = %d, want 2", len(passages))
	}
	if passages[0].PID != 10 || passages[1].PID != 99 {
		t.Fatalf("corpus is not ordered by PID: %#v", passages)
	}
	if passages[1].Text != "distractor passage" {
		t.Fatalf("unrelated corpus passage was lost: %#v", passages)
	}
}

func TestBuildDatasetRejectsInvalidIdentifiersAndContent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*[]TextInfo, *[]TextInfo, *[]TextInfo, *[]RelsInfo, *[]QaInfo)
	}{
		{
			name: "empty corpus",
			mutate: func(_ *[]TextInfo, corpus *[]TextInfo, _ *[]TextInfo, _ *[]RelsInfo, _ *[]QaInfo) {
				*corpus = nil
			},
		},
		{
			name: "duplicate passage ID",
			mutate: func(_ *[]TextInfo, corpus *[]TextInfo, _ *[]TextInfo, _ *[]RelsInfo, _ *[]QaInfo) {
				*corpus = append(*corpus, TextInfo{ID: 10, Text: "duplicate"})
			},
		},
		{
			name: "negative passage ID",
			mutate: func(_ *[]TextInfo, corpus *[]TextInfo, _ *[]TextInfo, _ *[]RelsInfo, _ *[]QaInfo) {
				(*corpus)[0].ID = -1
			},
		},
		{
			name: "empty passage",
			mutate: func(_ *[]TextInfo, corpus *[]TextInfo, _ *[]TextInfo, _ *[]RelsInfo, _ *[]QaInfo) {
				(*corpus)[0].Text = "  "
			},
		},
		{
			name: "duplicate question-answer relation",
			mutate: func(_ *[]TextInfo, _ *[]TextInfo, _ *[]TextInfo, _ *[]RelsInfo, qas *[]QaInfo) {
				*qas = append(*qas, (*qas)[0])
			},
		},
		{
			name: "unknown related passage",
			mutate: func(_ *[]TextInfo, _ *[]TextInfo, _ *[]TextInfo, qrels *[]RelsInfo, _ *[]QaInfo) {
				(*qrels)[0].PID = 404
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queries, corpus, answers, qrels, qas := validDatasetRows()
			test.mutate(&queries, &corpus, &answers, &qrels, &qas)
			if _, err := buildDataset(queries, corpus, answers, qrels, qas); err == nil {
				t.Fatal("expected invalid dataset to be rejected")
			}
		})
	}
}

func TestGetPassageListDoesNotExpandSparsePassageIDs(t *testing.T) {
	passages := getPassageList([]types.EvaluationPassage{
		{PID: 10, Text: "first"},
		{PID: 1_000_000, Text: "second"},
	})
	if len(passages) != 2 {
		t.Fatalf("passage count = %d, want 2", len(passages))
	}
}
