# CMRC2018 for WeKnora evaluation

`raw/` contains the unmodified `hfl/cmrc2018` Parquet files pinned in the
top-level `manifest.json`. The default evaluation subset is generated only
from the validation split:

```powershell
go run ./dataset/cmrc2018/convert
```

The converter audits every annotated answer span, keeps every unique validation
context as the retrieval corpus, and selects 200 eligible questions by the
stable order `sha256(seed + NUL + source_question_id)`. The first span-valid
annotation becomes the single WeKnora reference answer. Invalid alternative
spans and rows without a valid extractive answer are recorded rather than
silently accepted.

It writes the five files required by WeKnora to
`dataset/cmrc2018/weknora/`, plus:

- `manifest.json` for source, selection, counts, and output hashes;
- `selection-audit.jsonl` for original question IDs, answer variants, and
  character offsets;
- `source-quality-issues.jsonl` for invalid source annotations.

The generated subset is intended for compact engineering regression tests.
It is not a replacement for reporting results on the complete official split.
