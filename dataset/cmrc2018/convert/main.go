package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parquet-go/parquet-go"
)

const (
	defaultInput  = "dataset/cmrc2018/raw/validation-00000-of-00001.parquet"
	defaultOutput = "dataset/cmrc2018/weknora"
	defaultCases  = 200
	defaultSeed   = "cmrc2018-validation-v1"
)

var evaluationFileNames = []string{
	"queries.parquet",
	"corpus.parquet",
	"qrels.parquet",
	"qas.parquet",
	"answers.parquet",
}

type sourceAnswers struct {
	Text        []string `parquet:"text,list"`
	AnswerStart []int32  `parquet:"answer_start,list"`
}

type sourceRow struct {
	ID       string        `parquet:"id"`
	Context  string        `parquet:"context"`
	Question string        `parquet:"question"`
	Answers  sourceAnswers `parquet:"answers"`
}

type textRow struct {
	ID   int64  `parquet:"id"`
	Text string `parquet:"text"`
}

type qrelRow struct {
	QID int64 `parquet:"qid"`
	PID int64 `parquet:"pid"`
}

type qaRow struct {
	QID int64 `parquet:"qid"`
	AID int64 `parquet:"aid"`
}

type rankedSourceRow struct {
	Row                  sourceRow
	SourceIndex          int
	ReferenceAnswerIndex int
	RankKey              [32]byte
}

type auditRow struct {
	QID                 int64    `json:"qid"`
	PID                 int64    `json:"pid"`
	AID                 int64    `json:"aid"`
	SourceQuestionID    string   `json:"source_question_id"`
	SourceRowIndex      int      `json:"source_row_index"`
	SelectedAnswer      string   `json:"selected_answer"`
	SelectedAnswerIndex int      `json:"selected_answer_index"`
	AnswerVariants      []string `json:"answer_variants"`
	AnswerStarts        []int32  `json:"answer_starts"`
}

type sourceQualityIssue struct {
	SourceQuestionID string `json:"source_question_id"`
	SourceRowIndex   int    `json:"source_row_index"`
	AnswerIndex      int    `json:"answer_index"`
	Answer           string `json:"answer"`
	AnswerStart      int32  `json:"answer_start"`
	Reason           string `json:"reason"`
	Actual           string `json:"actual,omitempty"`
}

type manifestFile struct {
	Name   string `json:"name"`
	Rows   int    `json:"rows"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type outputManifest struct {
	SchemaVersion          string         `json:"schema_version"`
	SourceDataset          string         `json:"source_dataset"`
	SourceRevision         string         `json:"source_revision"`
	SourceSplit            string         `json:"source_split"`
	SourceFile             string         `json:"source_file"`
	SourceFileSHA256       string         `json:"source_file_sha256"`
	SourceRows             int            `json:"source_rows"`
	EligibleSourceRows     int            `json:"eligible_source_rows"`
	InvalidAnswerSpans     int            `json:"invalid_answer_spans"`
	RowsWithoutValidAnswer int            `json:"rows_without_valid_answer"`
	SelectionMethod        string         `json:"selection_method"`
	SelectionSeed          string         `json:"selection_seed"`
	SelectedCases          int            `json:"selected_cases"`
	CorpusPassages         int            `json:"corpus_passages"`
	ReferenceAnswerRule    string         `json:"reference_answer_rule"`
	Files                  []manifestFile `json:"files"`
}

func main() {
	input := flag.String("input", defaultInput, "CMRC2018 source validation parquet")
	output := flag.String("output", defaultOutput, "output directory for WeKnora files")
	cases := flag.Int("cases", defaultCases, "number of evaluation questions")
	seed := flag.String("seed", defaultSeed, "deterministic selection seed")
	flag.Parse()

	if err := convert(*input, *output, *cases, *seed); err != nil {
		fmt.Fprintln(os.Stderr, "cmrc2018 conversion failed:", err)
		os.Exit(1)
	}
}

func convert(inputPath, outputDir string, caseCount int, seed string) error {
	if caseCount <= 0 {
		return errors.New("cases must be positive")
	}
	if strings.TrimSpace(seed) == "" {
		return errors.New("seed must not be empty")
	}
	if _, err := os.Stat(outputDir); err == nil {
		return fmt.Errorf("output directory already exists: %s", outputDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output directory: %w", err)
	}

	rows, err := parquet.ReadFile[sourceRow](inputPath)
	if err != nil {
		return fmt.Errorf("read source parquet: %w", err)
	}
	if caseCount > len(rows) {
		return fmt.Errorf("requested %d cases from only %d source rows", caseCount, len(rows))
	}
	referenceIndexes, qualityIssues, rowsWithoutValidAnswer, err := validateSourceRows(rows)
	if err != nil {
		return err
	}
	if caseCount > len(referenceIndexes) {
		return fmt.Errorf("requested %d cases from only %d eligible source rows", caseCount, len(referenceIndexes))
	}

	corpus, contextPIDs := buildCorpus(rows)
	selected := selectRows(rows, referenceIndexes, caseCount, seed)
	queries := make([]textRow, 0, caseCount)
	answers := make([]textRow, 0, caseCount)
	qrels := make([]qrelRow, 0, caseCount)
	qas := make([]qaRow, 0, caseCount)
	audit := make([]auditRow, 0, caseCount)
	for index, selectedRow := range selected {
		qid := int64(index + 1)
		aid := qid
		pid := contextPIDs[selectedRow.Row.Context]
		selectedAnswer := selectedRow.Row.Answers.Text[selectedRow.ReferenceAnswerIndex]
		queries = append(queries, textRow{ID: qid, Text: selectedRow.Row.Question})
		answers = append(answers, textRow{ID: aid, Text: selectedAnswer})
		qrels = append(qrels, qrelRow{QID: qid, PID: pid})
		qas = append(qas, qaRow{QID: qid, AID: aid})
		audit = append(audit, auditRow{
			QID:                 qid,
			PID:                 pid,
			AID:                 aid,
			SourceQuestionID:    selectedRow.Row.ID,
			SourceRowIndex:      selectedRow.SourceIndex,
			SelectedAnswer:      selectedAnswer,
			SelectedAnswerIndex: selectedRow.ReferenceAnswerIndex,
			AnswerVariants:      append([]string(nil), selectedRow.Row.Answers.Text...),
			AnswerStarts:        append([]int32(nil), selectedRow.Row.Answers.AnswerStart...),
		})
	}

	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create output parent: %w", err)
	}
	temporaryDir, err := os.MkdirTemp(parent, ".weknora-convert-")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	defer os.RemoveAll(temporaryDir)

	rowCounts := map[string]int{
		"queries.parquet": len(queries),
		"corpus.parquet":  len(corpus),
		"qrels.parquet":   len(qrels),
		"qas.parquet":     len(qas),
		"answers.parquet": len(answers),
	}
	if err := parquet.WriteFile(filepath.Join(temporaryDir, "queries.parquet"), queries); err != nil {
		return fmt.Errorf("write queries: %w", err)
	}
	if err := parquet.WriteFile(filepath.Join(temporaryDir, "corpus.parquet"), corpus); err != nil {
		return fmt.Errorf("write corpus: %w", err)
	}
	if err := parquet.WriteFile(filepath.Join(temporaryDir, "qrels.parquet"), qrels); err != nil {
		return fmt.Errorf("write qrels: %w", err)
	}
	if err := parquet.WriteFile(filepath.Join(temporaryDir, "qas.parquet"), qas); err != nil {
		return fmt.Errorf("write qas: %w", err)
	}
	if err := parquet.WriteFile(filepath.Join(temporaryDir, "answers.parquet"), answers); err != nil {
		return fmt.Errorf("write answers: %w", err)
	}
	if err := writeJSONLines(filepath.Join(temporaryDir, "selection-audit.jsonl"), audit); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(temporaryDir, "source-quality-issues.jsonl"), qualityIssues); err != nil {
		return err
	}

	files := make([]manifestFile, 0, len(evaluationFileNames)+2)
	auxiliaryFiles := []string{"selection-audit.jsonl", "source-quality-issues.jsonl"}
	for _, name := range append(append([]string(nil), evaluationFileNames...), auxiliaryFiles...) {
		path := filepath.Join(temporaryDir, name)
		hash, size, err := hashFile(path)
		if err != nil {
			return err
		}
		files = append(files, manifestFile{Name: name, Rows: rowCounts[name], Bytes: size, SHA256: hash})
	}
	files[len(evaluationFileNames)].Rows = len(audit)
	files[len(evaluationFileNames)+1].Rows = len(qualityIssues)
	sourceHash, _, err := hashFile(inputPath)
	if err != nil {
		return err
	}
	manifest := outputManifest{
		SchemaVersion:          "weknora-cmrc2018/v1",
		SourceDataset:          "hfl/cmrc2018",
		SourceRevision:         "137f2c45a24275fb68f6961c4d357f46288886aa",
		SourceSplit:            "validation",
		SourceFile:             filepath.ToSlash(inputPath),
		SourceFileSHA256:       sourceHash,
		SourceRows:             len(rows),
		EligibleSourceRows:     len(referenceIndexes),
		InvalidAnswerSpans:     len(qualityIssues),
		RowsWithoutValidAnswer: rowsWithoutValidAnswer,
		SelectionMethod:        "ascending sha256(seed + NUL + source_question_id)",
		SelectionSeed:          seed,
		SelectedCases:          len(selected),
		CorpusPassages:         len(corpus),
		ReferenceAnswerRule:    "first span-valid annotated answer; all variants retained in selection-audit.jsonl",
		Files:                  files,
	}
	if err := writeJSON(filepath.Join(temporaryDir, "manifest.json"), manifest); err != nil {
		return err
	}
	if err := os.Rename(temporaryDir, outputDir); err != nil {
		return fmt.Errorf("publish output directory: %w", err)
	}
	fmt.Printf("converted source_rows=%d corpus=%d cases=%d output=%s\n", len(rows), len(corpus), len(selected), outputDir)
	return nil
}

func validateSourceRows(rows []sourceRow) (map[string]int, []sourceQualityIssue, int, error) {
	seenIDs := make(map[string]struct{}, len(rows))
	referenceIndexes := make(map[string]int, len(rows))
	issues := make([]sourceQualityIssue, 0)
	rowsWithoutValidAnswer := 0
	for index, row := range rows {
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Context) == "" || strings.TrimSpace(row.Question) == "" {
			return nil, nil, 0, fmt.Errorf("source row %d has an empty id, context, or question", index)
		}
		if _, exists := seenIDs[row.ID]; exists {
			return nil, nil, 0, fmt.Errorf("duplicate source question id %q", row.ID)
		}
		seenIDs[row.ID] = struct{}{}
		if len(row.Answers.Text) == 0 || len(row.Answers.Text) != len(row.Answers.AnswerStart) {
			return nil, nil, 0, fmt.Errorf("source row %s has invalid answer arrays", row.ID)
		}
		contextRunes := []rune(row.Context)
		for answerIndex, answer := range row.Answers.Text {
			start := int(row.Answers.AnswerStart[answerIndex])
			answerRunes := []rune(answer)
			if strings.TrimSpace(answer) == "" || start < 0 || start+len(answerRunes) > len(contextRunes) {
				issues = append(issues, sourceQualityIssue{
					SourceQuestionID: row.ID,
					SourceRowIndex:   index,
					AnswerIndex:      answerIndex,
					Answer:           answer,
					AnswerStart:      row.Answers.AnswerStart[answerIndex],
					Reason:           "invalid_range",
				})
				continue
			}
			if actual := string(contextRunes[start : start+len(answerRunes)]); actual != answer {
				issues = append(issues, sourceQualityIssue{
					SourceQuestionID: row.ID,
					SourceRowIndex:   index,
					AnswerIndex:      answerIndex,
					Answer:           answer,
					AnswerStart:      row.Answers.AnswerStart[answerIndex],
					Reason:           "span_mismatch",
					Actual:           actual,
				})
				continue
			}
			if _, exists := referenceIndexes[row.ID]; !exists {
				referenceIndexes[row.ID] = answerIndex
			}
		}
		if _, exists := referenceIndexes[row.ID]; !exists {
			rowsWithoutValidAnswer++
		}
	}
	return referenceIndexes, issues, rowsWithoutValidAnswer, nil
}

func buildCorpus(rows []sourceRow) ([]textRow, map[string]int64) {
	contexts := make(map[string]struct{})
	for _, row := range rows {
		contexts[row.Context] = struct{}{}
	}
	ordered := make([]string, 0, len(contexts))
	for context := range contexts {
		ordered = append(ordered, context)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := sha256.Sum256([]byte(ordered[i])), sha256.Sum256([]byte(ordered[j]))
		if comparison := bytes.Compare(left[:], right[:]); comparison != 0 {
			return comparison < 0
		}
		return ordered[i] < ordered[j]
	})
	corpus := make([]textRow, 0, len(ordered))
	contextPIDs := make(map[string]int64, len(ordered))
	for index, context := range ordered {
		pid := int64(index + 1)
		corpus = append(corpus, textRow{ID: pid, Text: context})
		contextPIDs[context] = pid
	}
	return corpus, contextPIDs
}

func selectRows(rows []sourceRow, referenceIndexes map[string]int, count int, seed string) []rankedSourceRow {
	ranked := make([]rankedSourceRow, 0, len(referenceIndexes))
	for index, row := range rows {
		referenceAnswerIndex, eligible := referenceIndexes[row.ID]
		if !eligible {
			continue
		}
		ranked = append(ranked, rankedSourceRow{
			Row:                  row,
			SourceIndex:          index,
			ReferenceAnswerIndex: referenceAnswerIndex,
			RankKey:              sha256.Sum256([]byte(seed + "\x00" + row.ID)),
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if comparison := bytes.Compare(ranked[i].RankKey[:], ranked[j].RankKey[:]); comparison != 0 {
			return comparison < 0
		}
		return ranked[i].Row.ID < ranked[j].Row.ID
	})
	return ranked[:count]
}

func writeJSONLines[T any](path string, rows []T) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create audit file: %w", err)
	}
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			_ = file.Close()
			return fmt.Errorf("write audit row: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush audit file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close audit file: %w", err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read %s for hashing: %w", path, err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), int64(len(data)), nil
}
