package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func syntheticRows() []sourceRow {
	return []sourceRow{
		{ID: "first", Context: "北京是首都。", Question: "首都是哪里？",
			Answers: sourceAnswers{Text: []string{"错误", "北京"}, AnswerStart: []int32{0, 0}}},
		{ID: "second", Context: "日本首都是东京。", Question: "日本首都是哪里？",
			Answers: sourceAnswers{Text: []string{"东京"}, AnswerStart: []int32{5}}},
		{ID: "invalid", Context: "保留干扰段落。", Question: "无有效答案？",
			Answers: sourceAnswers{Text: []string{"越界"}, AnswerStart: []int32{99}}},
	}
}

func TestRejectsUnpinnedSourceBeforePublishing(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "source.parquet"), filepath.Join(dir, "output")
	if err := parquet.WriteFile(input, syntheticRows()); err != nil {
		t.Fatal(err)
	}
	err := convert(input, output, 1, defaultSeed)
	if err == nil || !strings.Contains(err.Error(), "source fingerprint mismatch") {
		t.Fatalf("unpinned input error = %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("rejected source created output: %v", err)
	}
}

func TestUnicodeSpansAndInvalidAlternatives(t *testing.T) {
	references, issues, excluded, err := validateSourceRows(syntheticRows())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(references, map[string]int{"first": 1, "second": 0}) || excluded != 1 {
		t.Fatalf("references=%v excluded=%d", references, excluded)
	}
	if len(issues) != 2 || issues[0].Reason != "span_mismatch" || issues[0].Actual != "北京" ||
		issues[1].Reason != "invalid_range" {
		t.Fatalf("invalid annotations lost from audit: %#v", issues)
	}
}

func TestInvalidSourceRowsFailClosed(t *testing.T) {
	for _, name := range []string{"duplicate id", "missing question", "mismatched arrays", "no annotations"} {
		t.Run(name, func(t *testing.T) {
			rows := syntheticRows()
			switch name {
			case "duplicate id":
				rows[1].ID = rows[0].ID
			case "missing question":
				rows[0].Question = " "
			case "mismatched arrays":
				rows[0].Answers.AnswerStart = []int32{0}
			case "no annotations":
				rows[0].Answers = sourceAnswers{}
			}
			if _, _, _, err := validateSourceRows(rows); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestSelectionAndFullCorpusAreStableAcrossSourceOrder(t *testing.T) {
	rows := syntheticRows()
	references, _, _, err := validateSourceRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	selected := selectRows(rows, references, 2, defaultSeed)
	corpus, pids := buildCorpus(rows)
	rows[0], rows[2] = rows[2], rows[0]
	repeated := selectRows(rows, references, 2, defaultSeed)
	otherCorpus, otherPIDs := buildCorpus(rows)
	if len(corpus) != 3 || !reflect.DeepEqual(corpus, otherCorpus) || !reflect.DeepEqual(pids, otherPIDs) {
		t.Fatalf("full corpus changed or discarded the ineligible question's context: %v", corpus)
	}
	for i := range selected {
		if selected[i].Row.ID != repeated[i].Row.ID || selected[i].Row.ID == "invalid" {
			t.Fatalf("selection changed or included an invalid answer: %v", selected)
		}
	}
}

func TestConversionPreservesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "previous-result.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := convert("missing.parquet", dir, 1, defaultSeed); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing output was not rejected: %v", err)
	}
	if content, err := os.ReadFile(sentinel); err != nil || string(content) != "keep" {
		t.Fatalf("existing result changed: %q, %v", content, err)
	}
}
