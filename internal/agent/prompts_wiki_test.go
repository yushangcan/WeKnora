package agent

import (
	"strings"
	"testing"
	"text/template"
)

func TestWikiGranularityGuidance_RoutesByKey(t *testing.T) {
	cases := map[string]string{
		"focused":    WikiGranularityGuidanceFocused,
		"standard":   WikiGranularityGuidanceStandard,
		"exhaustive": WikiGranularityGuidanceExhaustive,
	}
	for key, want := range cases {
		if got := WikiGranularityGuidance(key); got != want {
			t.Errorf("WikiGranularityGuidance(%q) returned unexpected block", key)
			_ = got
		}
	}
}

func TestWikiGranularityGuidance_UnknownDefaultsToStandard(t *testing.T) {
	unknowns := []string{"", "FOCUSED", "detailed", "minimal", "full", "unknown"}
	for _, k := range unknowns {
		if WikiGranularityGuidance(k) != WikiGranularityGuidanceStandard {
			t.Errorf("WikiGranularityGuidance(%q) should fall back to STANDARD block", k)
		}
	}
}

// Sanity check that the three guidance blocks are meaningfully different.
// A regression (e.g. two constants accidentally pointing at the same string)
// would silently disable the user-facing level control.
func TestWikiGranularityGuidance_BlocksAreDistinct(t *testing.T) {
	blocks := []string{
		WikiGranularityGuidanceFocused,
		WikiGranularityGuidanceStandard,
		WikiGranularityGuidanceExhaustive,
	}
	seen := make(map[string]bool, len(blocks))
	for _, b := range blocks {
		if b == "" {
			t.Error("granularity guidance block must not be empty")
			continue
		}
		if seen[b] {
			t.Error("granularity guidance blocks must be distinct")
		}
		seen[b] = true
	}

	// Each block should name its mode, so the LLM can't silently get the
	// wrong guidance without us noticing in review.
	if !strings.Contains(WikiGranularityGuidanceFocused, "FOCUSED") {
		t.Error("focused block should self-identify")
	}
	if !strings.Contains(WikiGranularityGuidanceStandard, "STANDARD") {
		t.Error("standard block should self-identify")
	}
	if !strings.Contains(WikiGranularityGuidanceExhaustive, "EXHAUSTIVE") {
		t.Error("exhaustive block should self-identify")
	}
}

func renderWikiChunkCitation(t *testing.T, candidateSlugs, chunksXML, lang string) string {
	t.Helper()
	tmpl, err := template.New("cite").Parse(WikiChunkCitationPrompt)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, map[string]string{
		"CandidateSlugs": candidateSlugs,
		"ChunksXML":      chunksXML,
		"Language":       lang,
	}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return b.String()
}

// TestWikiChunkCitationPrompt_StablePrefixAcrossBatches verifies the property
// that enables provider prefix caching (issue #1687): within one document the
// candidate slugs and the static rules are constant and only the per-batch
// <chunks> block changes, so everything up to <chunks> must be byte-identical
// across batches. If the static rules trailed <chunks> they would be re-billed
// on every batch and this prefix would diverge.
func TestWikiChunkCitationPrompt_StablePrefixAcrossBatches(t *testing.T) {
	const slugs = "entity/acme = Acme Corp\nconcept/rag = Retrieval-Augmented Generation"

	a := renderWikiChunkCitation(t, slugs, `<c id="c001">first batch text</c>`, "English")
	b := renderWikiChunkCitation(t, slugs, `<c id="c099">a completely different second batch</c>`, "English")

	// Match the standalone <chunks> tag line (the per-batch data block), not the
	// inline "<chunks> block" references inside the instructions prose.
	const marker = "\n<chunks>\n"
	ia := strings.Index(a, marker)
	ib := strings.Index(b, marker)
	if ia < 0 || ib < 0 {
		t.Fatalf("rendered prompt missing %q block", marker)
	}

	if a[:ia] != b[:ib] {
		t.Errorf("prompt prefix before <chunks> differs across batches — provider prefix cache will miss.\nA-prefix:\n%s\n---\nB-prefix:\n%s", a[:ia], b[:ib])
	}

	// The static rules and per-document candidate-slug block must live inside
	// that shared prefix, not after the varying chunks.
	for _, must := range []string{"### Primary task", "### JSON Formatting Rules", "\n<candidate_slugs>\n"} {
		if idx := strings.Index(a, must); idx < 0 || idx > ia {
			t.Errorf("%q must appear before <chunks> to be part of the cached prefix (idx=%d, chunks=%d)", must, idx, ia)
		}
	}
}

// TestWikiChunkCitationPrompt_PreservesPlaceholders guards against accidental
// loss of a template field during future reorders.
func TestWikiChunkCitationPrompt_PreservesPlaceholders(t *testing.T) {
	for _, field := range []string{"{{.Language}}", "{{.CandidateSlugs}}", "{{.ChunksXML}}"} {
		if !strings.Contains(WikiChunkCitationPrompt, field) {
			t.Errorf("WikiChunkCitationPrompt lost template field %q", field)
		}
	}
}

func renderWikiPrompt(t *testing.T, name, prompt string, data map[string]string) string {
	t.Helper()
	tmpl, err := template.New(name).Parse(prompt)
	if err != nil {
		t.Fatalf("parse %s prompt: %v", name, err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatalf("execute %s prompt: %v", name, err)
	}
	return b.String()
}

func TestWikiCandidateAndSummaryPromptsKeepRulesBeforeDynamicContent(t *testing.T) {
	candidateData := map[string]string{
		"Language":            "English",
		"Granularity":         "standard",
		"GranularityGuidance": "balanced guidance",
		"PreviousSlugs":       "concept/existing = Existing",
	}
	candidateA := renderWikiPrompt(t, "candidate", WikiCandidateSlugPrompt, mergePromptData(candidateData, map[string]string{"Content": "document A"}))
	candidateB := renderWikiPrompt(t, "candidate", WikiCandidateSlugPrompt, mergePromptData(candidateData, map[string]string{"Content": "document B"}))
	assertStableWikiPrefix(t, candidateA, candidateB, "candidate")
	assertBefore(t, candidateA, "<previous_slugs>", "<document>", "candidate previous slugs")

	summaryData := map[string]string{"Language": "English", "ExtractedSlugs": "entity/acme = Acme"}
	summaryA := renderWikiPrompt(t, "summary", WikiSummaryPrompt, mergePromptData(summaryData, map[string]string{"Content": "document A"}))
	summaryB := renderWikiPrompt(t, "summary", WikiSummaryPrompt, mergePromptData(summaryData, map[string]string{"Content": "document B"}))
	assertStableWikiPrefix(t, summaryA, summaryB, "summary")
	assertBefore(t, summaryA, "<available_wiki_pages>", "<document>", "summary available pages")
}

func assertBefore(t *testing.T, prompt, first, second, name string) {
	t.Helper()
	firstIndex, secondIndex := strings.Index(prompt, first), strings.Index(prompt, second)
	if firstIndex < 0 || secondIndex < 0 || firstIndex > secondIndex {
		t.Fatalf("%s must appear before %s", name, second)
	}
}

func assertStableWikiPrefix(t *testing.T, first, second, name string) {
	t.Helper()
	marker := "\n<document>\n"
	firstIndex, secondIndex := strings.Index(first, marker), strings.Index(second, marker)
	if firstIndex < 0 || secondIndex < 0 {
		t.Fatalf("%s prompt missing document marker", name)
	}
	if first[:firstIndex] != second[:secondIndex] {
		t.Fatalf("%s prompt prefix changed with document content", name)
	}
	if !strings.Contains(first[:firstIndex], "<instructions>") || !strings.Contains(first[:firstIndex], "Output") {
		t.Fatalf("%s prompt rules/schema are not before dynamic document content", name)
	}
}

func mergePromptData(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func TestWikiPageModifyUserPrompt_HidesInternalChunkHandles(t *testing.T) {
	combined := WikiPageModifySystemPrompt + "\n" + WikiPageModifyUserPrompt
	for _, guidance := range []string{
		"NEVER output them in the page body or summary",
		"Source associations are stored separately by the system",
		"clean Markdown without inline chunk IDs",
	} {
		if !strings.Contains(combined, guidance) {
			t.Errorf("WikiPageModifyUserPrompt missing chunk-handle guidance %q", guidance)
		}
	}

	for _, obsolete := range []string{"Preserve Citations", "followed by an inline citation"} {
		if strings.Contains(combined, obsolete) {
			t.Errorf("WikiPageModifyUserPrompt still contains obsolete inline-citation rule %q", obsolete)
		}
	}
}

func renderWikiPageModify(t *testing.T, sourceContexts, title string) string {
	t.Helper()
	tmpl, err := template.New("modify").Parse(WikiPageModifyUserPrompt)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, map[string]string{
		"HasAdditions":         "1",
		"SharedSourceContexts": sourceContexts,
		"PageSlug":             "concept/" + strings.ToLower(title),
		"PageTitle":            title,
		"PageType":             "concept",
		"ExistingContent":      "(New page)",
		"NewContent":           "page-specific chunks for " + title,
		"Language":             "English",
	}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return b.String()
}

func TestWikiPageModifyUserPrompt_SharedSourceContextPrecedesPageVariables(t *testing.T) {
	const shared = "<document><title>Same Source</title><context>long shared summary</context></document>"
	a := renderWikiPageModify(t, shared, "Alpha")
	b := renderWikiPageModify(t, shared, "Beta")
	marker := "\n<page_metadata>\n"
	ia, ib := strings.Index(a, marker), strings.Index(b, marker)
	if ia < 0 || ib < 0 {
		t.Fatalf("rendered prompt missing page metadata marker")
	}
	if a[:ia] != b[:ib] {
		t.Fatalf("shared source prefix differs across pages\nA: %s\nB: %s", a[:ia], b[:ib])
	}
	if !strings.Contains(a[:ia], shared) {
		t.Fatalf("shared source context is not part of the cacheable prefix: %s", a[:ia])
	}
}

// TestWikiCacheablePromptPrefixes covers the remaining wiki calls that are
// repeated while one document is being processed.  Each case names the first
// per-call data block for that template; bytes before that block must remain
// identical when only the data block changes, otherwise a provider prefix
// cache cannot reuse the prompt prefix.
func TestWikiCacheablePromptPrefixes(t *testing.T) {
	tests := []struct {
		name        string
		prompt      string
		base        map[string]string
		vary        string
		first       string
		required    []string
		constraints []string
	}{
		{
			name:   "taxonomy",
			prompt: WikiTaxonomyPlanPrompt,
			base: map[string]string{
				"ExistingTaxonomy": "folder: engineering",
				"Items":            "entity/acme",
				"Language":         "English",
			},
			vary:  "Items",
			first: "\n<items>\n",
			required: []string{
				"{{.ExistingTaxonomy}}", "{{.Items}}", "{{.Language}}",
			},
			constraints: []string{
				"### JSON Formatting Rules", "Output format:", "Every item slug",
			},
		},
		{
			name:   "knowledge-extract",
			prompt: WikiKnowledgeExtractPrompt,
			base: map[string]string{
				"Content":       "document A",
				"PreviousSlugs": "concept/rag = RAG",
				"Language":      "English",
			},
			vary:  "Content",
			first: "\n<content>\n",
			required: []string{
				"{{.Content}}", "{{.PreviousSlugs}}", "{{.Language}}",
			},
			constraints: []string{
				"### Slug Continuity Rules", "### Deduplication Rules", "Output ONLY valid JSON",
			},
		},
		{
			name:   "deduplication",
			prompt: WikiDeduplicationPrompt,
			base: map[string]string{
				"Candidates": "<item slug=\"concept/rag\">RAG</item>",
			},
			vary:  "Candidates",
			first: "\n<items>\n",
			required: []string{
				"{{.Candidates}}",
			},
			constraints: []string{
				"Hard constraints", "Merge criteria", "Output ONLY valid JSON",
			},
		},
		{
			name:   "index-intro",
			prompt: WikiIndexIntroPrompt,
			base: map[string]string{
				"DocumentSummaries": "summary A",
				"Language":          "English",
			},
			vary:  "DocumentSummaries",
			first: "\n<document_summaries>\n",
			required: []string{
				"{{.DocumentSummaries}}", "{{.Language}}",
			},
			constraints: []string{
				"Write a title line", "2-3 sentences", "Output ONLY the title",
			},
		},
		{
			name:   "index-intro-update",
			prompt: WikiIndexIntroUpdatePrompt,
			base: map[string]string{
				"ExistingIntro":     "# Existing wiki\n\nAn introduction.",
				"ChangeDescription": "added a document",
				"DocumentSummaries": "summary A",
				"Language":          "English",
			},
			vary:  "ChangeDescription",
			first: "\n<changes>\n",
			required: []string{
				"{{.ExistingIntro}}", "{{.ChangeDescription}}", "{{.DocumentSummaries}}", "{{.Language}}",
			},
			constraints: []string{
				"Update the introduction", "1 title line + 2-3 sentences", "Output ONLY the updated title",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range tc.required {
				if !strings.Contains(tc.prompt, field) {
					t.Errorf("prompt lost template field %q", field)
				}
			}
			for _, constraint := range tc.constraints {
				if !strings.Contains(tc.prompt, constraint) {
					t.Errorf("prompt lost output constraint %q", constraint)
				}
			}

			firstData := renderWikiPrompt(t, tc.name, tc.prompt, tc.base)
			changed := mergePromptData(tc.base, map[string]string{tc.vary: tc.base[tc.vary] + " (changed)"})
			secondData := renderWikiPrompt(t, tc.name, tc.prompt, changed)
			firstIndex := strings.Index(firstData, tc.first)
			secondIndex := strings.Index(secondData, tc.first)
			if firstIndex < 0 || secondIndex < 0 {
				t.Fatalf("rendered prompt missing dynamic marker %q", tc.first)
			}
			if firstData[:firstIndex] != secondData[:secondIndex] {
				t.Fatalf("prompt prefix before %s changed with dynamic data", tc.first)
			}
			if firstIndex == 0 {
				t.Fatalf("dynamic marker %s has no static prefix", tc.first)
			}
		})
	}
}

func TestWikiPromptsParseWithAllTemplateFields(t *testing.T) {
	data := map[string]string{
		"AvailableSlugs":          "entity/acme",
		"Candidates":              "<item>candidate</item>",
		"CandidateSlugs":          "entity/acme = Acme",
		"ChangeDescription":       "added a document",
		"ChunksXML":               `<c id="c001">chunk</c>`,
		"Content":                 "document content",
		"DeletedContent":          "deleted content",
		"DocumentSummaries":       "summary",
		"ExistingContent":         "existing page",
		"ExistingIntro":           "# Existing",
		"ExistingTaxonomy":        "folder: engineering",
		"ExtractedSlugs":          "entity/acme = Acme",
		"Granularity":             "standard",
		"GranularityGuidance":     "balanced guidance",
		"Items":                   "entity/acme",
		"Language":                "English",
		"NewContent":              "new content",
		"PageAliases":             "Acme",
		"PageSlug":                "entity/acme",
		"PageTitle":               "Acme",
		"PageType":                "entity",
		"PreviousSlugs":           "entity/acme = Acme",
		"RemainingSourcesContent": "remaining source",
		"SharedSourceContexts":    "shared context",
		"HasAdditions":            "1",
		"HasRetractions":          "1",
	}

	prompts := map[string]string{
		"taxonomy":           WikiTaxonomyPlanPrompt,
		"summary":            WikiSummaryPrompt,
		"knowledge-extract":  WikiKnowledgeExtractPrompt,
		"candidate":          WikiCandidateSlugPrompt,
		"chunk-citation":     WikiChunkCitationPrompt,
		"page-modify-system": WikiPageModifySystemPrompt,
		"page-modify-user":   WikiPageModifyUserPrompt,
		"index-intro":        WikiIndexIntroPrompt,
		"index-intro-update": WikiIndexIntroUpdatePrompt,
		"deduplication":      WikiDeduplicationPrompt,
	}
	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			if _, err := template.New(name).Parse(prompt); err != nil {
				t.Fatalf("parse prompt: %v", err)
			}
			_ = renderWikiPrompt(t, name, prompt, data)
		})
	}
}
