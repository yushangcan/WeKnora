package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSplitEvaluationPassagesAppliesConfigurationAndKeepsBoundaries(t *testing.T) {
	firstPassage := strings.Repeat("alpha ", 80)
	secondPassage := strings.Repeat("beta ", 80)
	config := types.ChunkingConfig{ChunkSize: 120, ChunkOverlap: 20, Separators: []string{" "}}

	chunks, parents := splitEvaluationPassages([]types.EvaluationPassage{
		{PID: 10, Text: firstPassage},
		{PID: 20, Text: secondPassage},
	}, config)
	if len(parents) != 0 {
		t.Fatalf("unexpected parent chunks: %d", len(parents))
	}
	if len(chunks) <= 2 {
		t.Fatalf("configured splitter did not split passages: %d chunks", len(chunks))
	}
	for i, chunk := range chunks {
		if chunk.Seq != i {
			t.Fatalf("chunk sequence is not stable at %d: %d", i, chunk.Seq)
		}
		if strings.Contains(chunk.Content, "alpha") && strings.Contains(chunk.Content, "beta") {
			t.Fatalf("chunk crosses labeled passage boundary: %q", chunk.Content)
		}
		var metadata map[string]int
		if err := json.Unmarshal(chunk.Metadata, &metadata); err != nil {
			t.Fatalf("decode chunk metadata: %v", err)
		}
		if strings.Contains(chunk.Content, "alpha") && metadata[types.EvaluationPassageIDMetadataKey] != 10 {
			t.Fatalf("first passage PID was not preserved: %v", metadata)
		}
		if strings.Contains(chunk.Content, "beta") && metadata[types.EvaluationPassageIDMetadataKey] != 20 {
			t.Fatalf("second passage PID was not preserved: %v", metadata)
		}
	}
}

func TestSplitEvaluationPassagesPreservesParentReferences(t *testing.T) {
	config := types.ChunkingConfig{
		ChunkSize:         120,
		ChunkOverlap:      20,
		Separators:        []string{" "},
		EnableParentChild: true,
		ParentChunkSize:   240,
		ChildChunkSize:    60,
	}
	chunks, parents := splitEvaluationPassages(
		[]types.EvaluationPassage{
			{PID: 10, Text: strings.Repeat("first ", 100)},
			{PID: 20, Text: strings.Repeat("second ", 100)},
		},
		config,
	)
	if len(chunks) == 0 || len(parents) == 0 {
		t.Fatalf("expected parent-child chunks, got children=%d parents=%d", len(chunks), len(parents))
	}
	for _, chunk := range chunks {
		if chunk.ParentIndex >= len(parents) {
			t.Fatalf("child points outside parent list: %d >= %d", chunk.ParentIndex, len(parents))
		}
	}
	for _, parent := range parents {
		if len(parent.Metadata) == 0 {
			t.Fatal("parent chunk is missing evaluation passage metadata")
		}
	}
}
