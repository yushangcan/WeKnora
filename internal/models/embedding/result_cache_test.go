package embedding

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type cacheTestEmbedder struct {
	mu          sync.Mutex
	embedCalls  int
	batchCalls  int
	embedErr    error
	embedDelay  time.Duration
	batchInputs [][]string
}

func (f *cacheTestEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	f.embedCalls++
	f.mu.Unlock()
	if f.embedDelay > 0 {
		time.Sleep(f.embedDelay)
	}
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	return []float32{float32(len(text)), 1}, nil
}

func (f *cacheTestEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.batchCalls++
	f.batchInputs = append(f.batchInputs, append([]string(nil), texts...))
	f.mu.Unlock()
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	vectors := make([][]float32, len(texts))
	for index, text := range texts {
		vectors[index] = []float32{float32(len(text)), float32(index + 1)}
	}
	return vectors, nil
}

func (f *cacheTestEmbedder) BatchEmbedWithPool(ctx context.Context, model Embedder, texts []string) ([][]float32, error) {
	if model != nil && model != f {
		return model.BatchEmbed(ctx, texts)
	}
	return f.BatchEmbed(ctx, texts)
}
func (f *cacheTestEmbedder) GetModelName() string { return "cache-test" }
func (f *cacheTestEmbedder) GetDimensions() int   { return 2 }
func (f *cacheTestEmbedder) GetModelID() string   { return "cache-test-id" }

func testCacheConfig() Config {
	return Config{ModelID: "model-1", ModelName: "embed-v1", Provider: "test", Dimensions: 2}
}

func TestResultCacheEmbedReusesVector(t *testing.T) {
	inner := &cacheTestEmbedder{}
	wrapped := WrapResultCache(inner, newMemoryResultCache(4), testCacheConfig(), 42)

	first, err := wrapped.Embed(context.Background(), "same text")
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	second, err := wrapped.Embed(context.Background(), "same text")
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}
	if inner.embedCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", inner.embedCalls)
	}
	first[0] = 999
	if second[0] == 999 {
		t.Fatal("cache returned a mutable vector shared with the caller")
	}
}

func TestResultCacheReportsApplicationMetrics(t *testing.T) {
	inner := &cacheTestEmbedder{}
	cache := newMemoryResultCache(8)
	wrapped := WrapResultCache(inner, cache, testCacheConfig(), 42)

	if _, err := wrapped.Embed(context.Background(), "same text"); err != nil {
		t.Fatalf("cold Embed: %v", err)
	}
	if _, err := wrapped.Embed(context.Background(), "same text"); err != nil {
		t.Fatalf("warm Embed: %v", err)
	}
	if _, err := wrapped.BatchEmbed(context.Background(), []string{"same text", "new text", "new text"}); err != nil {
		t.Fatalf("mixed BatchEmbed: %v", err)
	}

	got := CacheMetrics(cache)
	if got.Requests != 3 || got.HitItems != 2 || got.MissItems != 2 {
		t.Fatalf("unexpected request metrics: %#v", got)
	}
	if got.ProviderRequests != 2 || got.ProviderItems != 2 {
		t.Fatalf("unexpected provider metrics: %#v", got)
	}
}

func TestResultCacheBatchDeduplicatesAndRestoresOrder(t *testing.T) {
	inner := &cacheTestEmbedder{}
	wrapped := WrapResultCache(inner, newMemoryResultCache(8), testCacheConfig(), 42)

	result, err := wrapped.BatchEmbed(context.Background(), []string{"a", "bb", "a"})
	if err != nil {
		t.Fatalf("BatchEmbed: %v", err)
	}
	if len(result) != 3 || result[0][0] != 1 || result[1][0] != 2 || result[2][0] != 1 {
		t.Fatalf("unexpected result order: %#v", result)
	}
	if inner.batchCalls != 1 || len(inner.batchInputs[0]) != 2 {
		t.Fatalf("provider batch inputs = %#v, want one batch with two unique texts", inner.batchInputs)
	}
	if _, err := wrapped.BatchEmbed(context.Background(), []string{"a", "bb", "a"}); err != nil {
		t.Fatalf("cached BatchEmbed: %v", err)
	}
	if inner.batchCalls != 1 {
		t.Fatalf("cached batch invoked provider %d times, want 1", inner.batchCalls)
	}
}

func TestResultCachePooledRequestsReuseBatchEntries(t *testing.T) {
	inner := &cacheTestEmbedder{}
	wrapped := WrapResultCache(inner, newMemoryResultCache(8), testCacheConfig(), 42)
	if _, err := wrapped.BatchEmbedWithPool(context.Background(), wrapped, []string{"a", "b"}); err != nil {
		t.Fatalf("cold pooled BatchEmbed: %v", err)
	}
	if _, err := wrapped.BatchEmbedWithPool(context.Background(), wrapped, []string{"a", "b"}); err != nil {
		t.Fatalf("warm pooled BatchEmbed: %v", err)
	}
	if inner.batchCalls != 1 {
		t.Fatalf("pooled provider calls = %d, want 1", inner.batchCalls)
	}
}

func TestResultCacheCoalescesConcurrentEmbedMisses(t *testing.T) {
	inner := &cacheTestEmbedder{embedDelay: 20 * time.Millisecond}
	wrapped := WrapResultCache(inner, newMemoryResultCache(8), testCacheConfig(), 42)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := wrapped.Embed(context.Background(), "same"); err != nil {
				t.Errorf("Embed: %v", err)
			}
		}()
	}
	wg.Wait()
	if inner.embedCalls != 1 {
		t.Fatalf("concurrent provider calls = %d, want 1", inner.embedCalls)
	}
}

func TestResultCacheFailsOpenAndDoesNotStoreInvalidVector(t *testing.T) {
	inner := &cacheTestEmbedder{embedErr: errors.New("provider unavailable")}
	wrapped := WrapResultCache(inner, newMemoryResultCache(4), testCacheConfig(), 42)
	if _, err := wrapped.Embed(context.Background(), "unstable"); !errors.Is(err, inner.embedErr) {
		t.Fatalf("Embed error = %v, want provider error", err)
	}
	if inner.embedCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", inner.embedCalls)
	}
}

func TestResultCacheRedisRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	cache := &redisResultCache{client: client}
	key := EmbeddingCacheKey(7, EmbeddingCacheIdentity(testCacheConfig()), "hello")
	vector := []float32{1.25, -2.5}
	if err := cache.Set(context.Background(), key, vector, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := cache.Get(context.Background(), key)
	if err != nil || !ok || len(got) != len(vector) || got[0] != vector[0] || got[1] != vector[1] {
		t.Fatalf("Get = %#v, ok=%v, err=%v", got, ok, err)
	}
	secondKey := EmbeddingCacheKey(7, EmbeddingCacheIdentity(testCacheConfig()), "world")
	if err := cache.SetMany(context.Background(), map[string][]float32{secondKey: []float32{3, 4}}, time.Minute); err != nil {
		t.Fatalf("SetMany: %v", err)
	}
	many, err := cache.GetMany(context.Background(), []string{key, secondKey, "missing"})
	if err != nil || len(many) != 2 {
		t.Fatalf("GetMany = %#v, err=%v", many, err)
	}
}

func TestEmbeddingCacheIdentityChangesWithModelVersion(t *testing.T) {
	first := testCacheConfig()
	second := first
	second.ModelVersion = time.Unix(10, 0)
	if EmbeddingCacheIdentity(first) == EmbeddingCacheIdentity(second) {
		t.Fatal("model version did not change cache identity")
	}
}

func TestEmbeddingCacheKeyIsTenantAndByteScoped(t *testing.T) {
	model := EmbeddingCacheIdentity(testCacheConfig())
	base := EmbeddingCacheKey(1, model, "text")
	if base == EmbeddingCacheKey(1, model, "text ") {
		t.Fatal("trailing whitespace must change the cache key")
	}
	if base == EmbeddingCacheKey(2, model, "text") {
		t.Fatal("tenant scope must change the cache key")
	}
	if strings.Contains(base, "text") || strings.Contains(base, "model-1") {
		t.Fatalf("cache key contains raw input or model identity: %q", base)
	}
}

func TestWrapResultCacheBypassesWithoutTenant(t *testing.T) {
	inner := &cacheTestEmbedder{}
	wrapped := WrapResultCache(inner, newMemoryResultCache(4), testCacheConfig(), 0)
	if _, err := wrapped.Embed(context.Background(), "same text"); err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	if _, err := wrapped.Embed(context.Background(), "same text"); err != nil {
		t.Fatalf("second Embed: %v", err)
	}
	if inner.embedCalls != 2 {
		t.Fatalf("provider calls = %d, want 2 when tenant scope is missing", inner.embedCalls)
	}
}

func TestNewEmbeddingResultCacheDisabledByDefault(t *testing.T) {
	t.Setenv("WEKNORA_EMBEDDING_CACHE_ENABLED", "false")
	cache := NewEmbeddingResultCache(nil)
	if _, ok := cache.(noopResultCache); !ok {
		t.Fatalf("disabled cache type = %T, want noopResultCache", cache)
	}
}

func TestMemoryResultCacheHonorsLRUAndTTL(t *testing.T) {
	cache := newMemoryResultCache(1)
	if err := cache.Set(context.Background(), "first", []float32{1, 2}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(context.Background(), "second", []float32{3, 4}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(context.Background(), "first"); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if _, ok, _ := cache.Get(context.Background(), "second"); !ok {
		t.Fatal("newest entry was evicted")
	}
	if err := cache.Set(context.Background(), "expired", []float32{5}, time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, ok, _ := cache.Get(context.Background(), "expired"); ok {
		t.Fatal("expired entry was returned")
	}
}

func TestResultCacheRejectsNonFiniteVectors(t *testing.T) {
	inner := &cacheTestEmbedder{}
	cache := newMemoryResultCache(4)
	key := EmbeddingCacheKey(1, EmbeddingCacheIdentity(testCacheConfig()), "bad")
	if err := cache.Set(context.Background(), key, []float32{float32(math.NaN())}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(context.Background(), key); !ok {
		t.Fatal("store should remain a general-purpose store")
	}
	wrapped := WrapResultCache(inner, cache, testCacheConfig(), 1)
	if _, err := wrapped.Embed(context.Background(), "bad"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if inner.embedCalls != 1 {
		t.Fatalf("invalid vector was treated as a hit; provider calls=%d", inner.embedCalls)
	}
}
