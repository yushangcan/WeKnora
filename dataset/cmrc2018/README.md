# CMRC2018 for WeKnora evaluation

`raw/` contains the unmodified `hfl/cmrc2018` Parquet files pinned in the
top-level `manifest.json`. The default evaluation subset is generated only
from the validation split:

```powershell
go run ./dataset/cmrc2018/convert
```

The converter verifies the validation Parquet SHA-256 against the pinned source
before parsing it. A train/test file, another revision, or modified bytes are
rejected even if their schema matches. `-input` may relocate the same pinned
validation file; it does not enable arbitrary source datasets. Existing output
directories are rejected to preserve earlier results.

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

On a fresh Windows checkout, download the pinned validation file from the
tracked manifest and verify it before conversion (PowerShell, repository root):

```powershell
$cmrcManifest = Get-Content dataset/cmrc2018/manifest.json -Raw | ConvertFrom-Json
$cmrcSource = $cmrcManifest.files | Where-Object split -eq 'validation'
$cmrcPath = Join-Path 'dataset/cmrc2018' $cmrcSource.path
if (-not (Test-Path -LiteralPath $cmrcPath)) {
    New-Item -ItemType Directory -Force (Split-Path $cmrcPath) | Out-Null
    Invoke-WebRequest -Uri $cmrcSource.source_url -OutFile $cmrcPath
}
if ((Get-FileHash -LiteralPath $cmrcPath -Algorithm SHA256).Hash.ToLowerInvariant() -ne $cmrcSource.sha256) {
    throw 'CMRC2018 source SHA-256 mismatch; inspect the downloaded file before continuing.'
}
go run ./dataset/cmrc2018/convert
```

Raw and generated data are ignored by Git; the source manifest, dataset card,
converter and tests are versioned. See `SOURCE_README.md` for the dataset's
source, citation and license information.

`go test ./dataset/cmrc2018/convert` always runs offline checks for source
rejection, Unicode answer offsets, invalid annotations, corpus preservation and
stable selection. The official 200-question conversion test additionally runs
when the pinned source exists under `raw/`; otherwise only that test is skipped.
