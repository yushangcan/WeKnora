package service

import (
	"math"
	"strconv"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func evaluationSearchResult(pid int) *types.SearchResult {
	return &types.SearchResult{
		ChunkMetadata: types.JSON([]byte(`{"evaluation_pid":` + strconv.Itoa(pid) + `}`)),
	}
}

func TestHookMetricUsesRankedPassageIDs(t *testing.T) {
	hook := NewHookMetric(1)
	hook.recordInit(0)
	hook.recordQaPair(0, &types.QAPair{PIDs: []int{1}, Answer: "expected"})
	hook.recordSearchResult(0, []*types.SearchResult{evaluationSearchResult(1)})
	hook.recordRerankResult(0, []*types.SearchResult{
		evaluationSearchResult(2),
		evaluationSearchResult(1),
	})
	hook.recordChatResponse(0, &types.ChatResponse{Content: "expected"})
	hook.recordFinish(0)

	result := hook.MetricResult().RetrievalMetrics
	require.InDelta(t, 0.5, result.Precision, 1e-9)
	require.InDelta(t, 1.0, result.Recall, 1e-9)
	require.InDelta(t, 0.5, result.MRR, 1e-9)
	require.InDelta(t, 1/math.Log2(3), result.NDCG3, 1e-9)
	require.InDelta(t, 1/math.Log2(3), result.NDCG10, 1e-9)
	caseResult := hook.CaseMetricResult(0)
	require.NotNil(t, caseResult)
	require.InDelta(t, result.Precision, caseResult.RetrievalMetrics.Precision, 1e-9)
}

func TestEvaluationRetrievalIDsDeduplicateAndRetainUnmappedResults(t *testing.T) {
	ids, unmappedCount := evaluationRetrievalIDs([]*types.SearchResult{
		evaluationSearchResult(1),
		evaluationSearchResult(1),
		{ChunkMetadata: types.JSON(`{"unexpected":true}`)},
		nil,
		evaluationSearchResult(2),
	})

	require.Equal(t, []int{1, -1, -2, 2}, ids)
	require.Equal(t, 2, unmappedCount)
}
