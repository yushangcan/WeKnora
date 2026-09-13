// Command fixture writes a tiny synthetic corpus exclusively for CI.
// It never overwrites dataset/samples in the checkout.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
)

type textRow struct {
	ID   int64  `parquet:"id"`
	Text string `parquet:"text"`
}
type relevance struct {
	QID int64 `parquet:"qid"`
	PID int64 `parquet:"pid"`
}
type answerRelation struct {
	QID int64 `parquet:"qid"`
	AID int64 `parquet:"aid"`
}

func main() {
	if len(os.Args) != 2 {
		panic("usage: fixture OUTPUT_DIRECTORY")
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}
	write(dir, "queries.parquet", []textRow{
		{1, "What is the capital of France?"},
		{2, "What is the capital of Japan?"},
	})
	write(dir, "corpus.parquet", []textRow{
		{10, "Paris is the capital of France."},
		{20, "Tokyo is the capital of Japan."},
		{99, "Mars is a planet."},
	})
	write(dir, "answers.parquet", []textRow{{1, "Paris"}, {2, "Tokyo"}})
	write(dir, "qrels.parquet", []relevance{{1, 10}, {2, 20}})
	write(dir, "qas.parquet", []answerRelation{{1, 1}, {2, 2}})
	fmt.Println("generated synthetic CI fixture: 2 cases, 3 passages (including a distractor)")
}

func write[T any](dir, name string, rows []T) {
	if err := parquet.WriteFile(filepath.Join(dir, name), rows); err != nil {
		panic(err)
	}
}
