package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestOfficialValidationSubsetIsDeterministicAndConsistent(t *testing.T) {
	source := filepath.Join("..", "raw", "validation-00000-of-00001.parquet")
	if _, err := os.Stat(source); os.IsNotExist(err) {
		t.Skip("local CMRC2018 source file is not present")
	} else if err != nil {
		t.Fatalf("inspect source file: %v", err)
	}

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := convert(source, first, defaultCases, defaultSeed); err != nil {
		t.Fatalf("convert first subset: %v", err)
	}
	if err := convert(source, second, defaultCases, defaultSeed); err != nil {
		t.Fatalf("convert second subset: %v", err)
	}

	for _, name := range append(append([]string(nil), evaluationFileNames...),
		"selection-audit.jsonl", "source-quality-issues.jsonl") {
		firstHash, _, err := hashFile(filepath.Join(first, name))
		if err != nil {
			t.Fatalf("hash first %s: %v", name, err)
		}
		secondHash, _, err := hashFile(filepath.Join(second, name))
		if err != nil {
			t.Fatalf("hash second %s: %v", name, err)
		}
		if firstHash != secondHash {
			t.Fatalf("%s is not deterministic: %s != %s", name, firstHash, secondHash)
		}
	}

	queries := readParquetForTest[textRow](t, filepath.Join(first, "queries.parquet"))
	corpus := readParquetForTest[textRow](t, filepath.Join(first, "corpus.parquet"))
	answers := readParquetForTest[textRow](t, filepath.Join(first, "answers.parquet"))
	qrels := readParquetForTest[qrelRow](t, filepath.Join(first, "qrels.parquet"))
	qas := readParquetForTest[qaRow](t, filepath.Join(first, "qas.parquet"))
	if len(queries) != 200 || len(answers) != 200 || len(qrels) != 200 || len(qas) != 200 {
		t.Fatalf("unexpected case file counts: queries=%d answers=%d qrels=%d qas=%d",
			len(queries), len(answers), len(qrels), len(qas))
	}
	if len(corpus) != 848 {
		t.Fatalf("corpus count = %d, want 848", len(corpus))
	}

	queryIDs := make(map[int64]struct{}, len(queries))
	answerIDs := make(map[int64]struct{}, len(answers))
	passageIDs := make(map[int64]struct{}, len(corpus))
	for _, row := range queries {
		queryIDs[row.ID] = struct{}{}
	}
	for _, row := range answers {
		answerIDs[row.ID] = struct{}{}
	}
	for _, row := range corpus {
		passageIDs[row.ID] = struct{}{}
	}
	for _, row := range qrels {
		if _, ok := queryIDs[row.QID]; !ok {
			t.Fatalf("qrels references unknown qid %d", row.QID)
		}
		if _, ok := passageIDs[row.PID]; !ok {
			t.Fatalf("qrels references unknown pid %d", row.PID)
		}
	}
	for _, row := range qas {
		if _, ok := queryIDs[row.QID]; !ok {
			t.Fatalf("qas references unknown qid %d", row.QID)
		}
		if _, ok := answerIDs[row.AID]; !ok {
			t.Fatalf("qas references unknown aid %d", row.AID)
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest outputManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.SourceRows != 3219 || manifest.EligibleSourceRows != 3216 ||
		manifest.InvalidAnswerSpans != 210 || manifest.RowsWithoutValidAnswer != 3 ||
		manifest.SelectedCases != 200 || manifest.CorpusPassages != 848 {
		t.Fatalf("unexpected source audit summary: %#v", manifest)
	}
}

func readParquetForTest[T any](t *testing.T, path string) []T {
	t.Helper()
	rows, err := parquet.ReadFile[T](path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return rows
}
