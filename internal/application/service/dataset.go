package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/parquet-go/parquet-go"
)

// DatasetService provides operations for working with datasets
type DatasetService struct{}

const (
	defaultDatasetID      = "default"
	defaultDatasetVersion = "1"
	defaultDatasetDir     = "./dataset/samples"
)

var defaultDatasetFiles = []string{
	"queries.parquet",
	"corpus.parquet",
	"qrels.parquet",
	"qas.parquet",
	"answers.parquet",
}

// NewDatasetService creates a new DatasetService instance
func NewDatasetService() interfaces.DatasetService {
	return &DatasetService{}
}

// TextInfo represents text data with ID in parquet format
type TextInfo struct {
	ID   int64  `parquet:"id"`   // Unique identifier
	Text string `parquet:"text"` // Text content
}

// RelsInfo represents question-passage relations in parquet format
type RelsInfo struct {
	QID int64 `parquet:"qid"` // Question ID
	PID int64 `parquet:"pid"` // Passage ID
}

// QaInfo represents question-answer relations in parquet format
type QaInfo struct {
	QID int64 `parquet:"qid"` // Question ID
	AID int64 `parquet:"aid"` // Answer ID
}

// GetDatasetByID retrieves QA pairs from dataset by ID
func (d *DatasetService) GetDatasetByID(ctx context.Context, datasetID string) ([]*types.QAPair, error) {
	dataset, err := d.LoadDataset(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	return dataset.Cases, nil
}

// LoadDataset loads, validates and fingerprints a supported evaluation dataset.
func (d *DatasetService) LoadDataset(ctx context.Context, datasetID string) (*types.EvaluationDataset, error) {
	logger.Info(ctx, "Start getting dataset by ID")
	logger.Infof(ctx, "Getting dataset with ID: %s", datasetID)

	if datasetID != defaultDatasetID {
		return nil, fmt.Errorf("unsupported evaluation dataset: %s", datasetID)
	}
	dataset, err := loadDefaultDataset(defaultDatasetDir)
	if err != nil {
		return nil, err
	}
	dataset.PrintStats(ctx)
	qaPairs := dataset.Iterate()
	fingerprint, err := fingerprintDataset(defaultDatasetDir, defaultDatasetFiles)
	if err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d QA pairs from dataset", len(qaPairs))
	return &types.EvaluationDataset{
		Descriptor: types.EvaluationDatasetDescriptor{
			ID:                 defaultDatasetID,
			Version:            defaultDatasetVersion,
			ContentFingerprint: fingerprint,
			QueryCount:         len(dataset.queries),
			CorpusCount:        len(dataset.corpus),
			CaseCount:          len(qaPairs),
			IngestionMode:      types.EvaluationDatasetModePassageChunking,
		},
		Cases: qaPairs,
	}, nil
}

// DefaultDataset loads and initializes the default dataset from parquet files
func DefaultDataset() dataset {
	result, err := loadDefaultDataset(defaultDatasetDir)
	if err != nil {
		panic(err)
	}
	return result
}

func loadDefaultDataset(datasetDir string) (dataset, error) {
	queries, err := loadParquet[TextInfo](fmt.Sprintf("%s/queries.parquet", datasetDir))
	if err != nil {
		return dataset{}, err
	}
	corpus, err := loadParquet[TextInfo](fmt.Sprintf("%s/corpus.parquet", datasetDir))
	if err != nil {
		return dataset{}, err
	}
	answers, err := loadParquet[TextInfo](fmt.Sprintf("%s/answers.parquet", datasetDir))
	if err != nil {
		return dataset{}, err
	}
	qrels, err := loadParquet[RelsInfo](fmt.Sprintf("%s/qrels.parquet", datasetDir))
	if err != nil {
		return dataset{}, err
	}
	qas, err := loadParquet[QaInfo](fmt.Sprintf("%s/qas.parquet", datasetDir))
	if err != nil {
		return dataset{}, err
	}

	res := dataset{
		queries: make(map[int64]string),  // qid -> question text
		corpus:  make(map[int64]string),  // pid -> passage text
		answers: make(map[int64]string),  // aid -> answer text
		qrels:   make(map[int64][]int64), // qid -> list of pid
		qas:     make(map[int64]int64),   // qid -> aid
	}
	for _, qi := range queries {
		res.queries[qi.ID] = qi.Text
	}
	for _, ci := range corpus {
		res.corpus[ci.ID] = ci.Text
	}
	for _, ai := range answers {
		res.answers[ai.ID] = ai.Text
	}
	for _, ri := range qrels {
		if _, ok := res.queries[ri.QID]; !ok {
			return dataset{}, fmt.Errorf("qrels references unknown query %d", ri.QID)
		}
		if _, ok := res.corpus[ri.PID]; !ok {
			return dataset{}, fmt.Errorf("qrels references unknown passage %d", ri.PID)
		}
		res.qrels[ri.QID] = append(res.qrels[ri.QID], ri.PID)
	}
	for _, qi := range qas {
		if _, ok := res.queries[qi.QID]; !ok {
			return dataset{}, fmt.Errorf("qas references unknown query %d", qi.QID)
		}
		if _, ok := res.answers[qi.AID]; !ok {
			return dataset{}, fmt.Errorf("qas references unknown answer %d", qi.AID)
		}
		res.qas[qi.QID] = qi.AID
	}
	for qid, question := range res.queries {
		if question == "" {
			return dataset{}, fmt.Errorf("query %d is empty", qid)
		}
		if len(res.qrels[qid]) == 0 {
			return dataset{}, fmt.Errorf("query %d has no relevant passages", qid)
		}
		aid, ok := res.qas[qid]
		if !ok || res.answers[aid] == "" {
			return dataset{}, fmt.Errorf("query %d has no reference answer", qid)
		}
	}
	return res, nil
}

// dataset represents the in-memory dataset structure
type dataset struct {
	queries map[int64]string  // qid -> question text
	corpus  map[int64]string  // pid -> passage text
	answers map[int64]string  // aid -> answer text
	qrels   map[int64][]int64 // qid -> list of related pids
	qas     map[int64]int64   // qid -> aid
}

// Iterate generates QA pairs from the dataset
func (d *dataset) Iterate() []*types.QAPair {
	qids := make([]int64, 0, len(d.queries))
	for qid := range d.queries {
		qids = append(qids, qid)
	}
	sort.Slice(qids, func(i, j int) bool { return qids[i] < qids[j] })
	pairs := make([]*types.QAPair, 0, len(qids))

	for _, qid := range qids {
		question := d.queries[qid]
		// Get answer info
		aid, hasAnswer := d.qas[qid]
		answer := ""
		if hasAnswer {
			answer = d.answers[aid]
		}

		// Get related passages
		pids := d.qrels[qid]
		var pidStr []int
		for _, pid := range pids {
			pidStr = append(pidStr, int(pid))
		}
		var passages []string
		for _, pid := range pids {
			passages = append(passages, d.corpus[pid])
		}

		pairs = append(pairs, &types.QAPair{
			QID:      int(qid),
			Question: question,
			PIDs:     pidStr,
			Passages: passages,
			AID:      int(aid),
			Answer:   answer,
		})
	}

	return pairs
}

func fingerprintDataset(datasetDir string, names []string) (string, error) {
	hash := sha256.New()
	for _, name := range names {
		path := filepath.Join(datasetDir, name)
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("open dataset file %s: %w", name, err)
		}
		_, _ = io.WriteString(hash, name)
		_, err = io.Copy(hash, file)
		closeErr := file.Close()
		if err != nil {
			return "", fmt.Errorf("fingerprint dataset file %s: %w", name, err)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close dataset file %s: %w", name, closeErr)
		}
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

// GetContextForQID retrieves context passages for a given question ID
func (d *dataset) GetContextForQID(qid int64) ([]string, error) {
	pids, ok := d.qrels[qid]
	if !ok {
		return nil, errors.New("question ID not found")
	}

	var contextParts []string
	for _, pid := range pids {
		if text, exists := d.corpus[pid]; exists {
			contextParts = append(contextParts, text)
		}
	}

	return contextParts, nil
}

// PrintStats prints dataset statistics to the logger
func (d *dataset) PrintStats(ctx context.Context) {
	logger.Infof(ctx, "QA System Statistics:")
	logger.Infof(ctx, "- Total queries: %d", len(d.queries))
	logger.Infof(ctx, "- Total corpus passages: %d", len(d.corpus))
	logger.Infof(ctx, "- Total answers: %d", len(d.answers))

	// Calculate average passages per query
	totalRelations := 0
	for _, pids := range d.qrels {
		totalRelations += len(pids)
	}
	avgPassages := float64(totalRelations) / float64(len(d.qrels))
	logger.Infof(ctx, "- Average passages per query: %.2f", avgPassages)

	// Calculate coverage
	coveredQueries := len(d.qas)
	coverage := float64(coveredQueries) / float64(len(d.queries)) * 100
	logger.Infof(ctx, "- Answer coverage: %.2f%% (%d/%d)", coverage, coveredQueries, len(d.queries))
}

// PrintRandomQA prints a random question with its related passages and answer
func (d *dataset) PrintRandomQA() error {
	// Get a random qid
	var qid int64
	for k := range d.qas {
		qid = k
		break
	}
	if qid == 0 {
		return errors.New("no questions available")
	}

	// Get question text
	question, ok := d.queries[qid]
	if !ok {
		return fmt.Errorf("question %d not found", qid)
	}

	// Get answer info
	aid, ok := d.qas[qid]
	if !ok {
		return fmt.Errorf("answer for question %d not found", qid)
	}
	answer, ok := d.answers[aid]
	if !ok {
		return fmt.Errorf("answer %d not found", aid)
	}

	// Print formatted QA
	fmt.Println("===== Random QA =====")
	fmt.Printf("QID: %d\n", qid)
	fmt.Printf("Question: %s\n", question)

	// Print passages if available
	if pids, exists := d.qrels[qid]; exists && len(pids) > 0 {
		fmt.Println("\nRelated passages:")
		for i, pid := range pids {
			if text, exists := d.corpus[pid]; exists {
				fmt.Printf("\nPassage %d (PID: %d):\n%s\n", i+1, pid, text)
			}
		}
	} else {
		fmt.Println("\nNo related passages found")
	}

	// Print answer
	fmt.Printf("\nAnswer (AID: %d):\n%s\n", aid, answer)

	return nil
}

// loadParquet loads data from parquet file into specified type
func loadParquet[T any](filePath string) ([]T, error) {
	rows, err := parquet.ReadFile[T](filePath)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
