# Exam format `assess/v1` (draft)

Status: **draft** — finalized and enforced by `ax validate` in phase 1.
Reference example: [`examples/algebra-01`](../examples/algebra-01).

An exam is a plain folder. It can live anywhere and be versioned with git;
`ax` always refers to exams by folder path.

```
algebra-01/
├── manifest.yaml            # exam definition (public)
├── questions/               # one file per question (public)
│   ├── q001.yaml
│   └── ...
├── private/
│   └── answer-key.yaml      # correct answers and scoring (never sent to browsers)
└── assets/                  # images referenced from Markdown (optional)
```

Splitting the answer key into `private/` limits exposure (the server never
serves it), but it is **not encryption**: anyone with access to the folder can
read it. Encryption applies only to exported `.examx` packages.

## General rules

- YAML is decoded **strictly**: unknown keys are errors.
- Identifiers (`id`, option ids, pool ids) match `^[a-z0-9][a-z0-9_-]{0,63}$`.
- Question ids are unique and must match their file name (`q001.yaml` → `id: q001`).
- Durations use Go syntax: `90s`, `45m`, `1h30m`.
- Text fields marked *Markdown* accept CommonMark plus `$inline$` and
  `$$block$$` KaTeX math. HTML is stripped. Images may only reference files
  under `assets/` (e.g. `![diagram](assets/diagram.png)`).
- Limits (initial): 500 questions per exam, 10 options per question,
  64 KiB per Markdown field, 20 MiB total for `assets/`.

## `manifest.yaml`

| Key | Type | Required | Default | Notes |
|---|---|---|---|---|
| `format` | string | yes | — | Must be `assess/v1`. |
| `id` | id | yes | — | Should match the folder name. |
| `title` | string | yes | — | |
| `language` | `es` \| `en` | no | `es` | Student UI language. |
| `duration` | duration | yes | — | Overall exam time, enforced by the server. |
| `result_visibility` | `none` \| `score` \| `review` | no | `score` | `review` is shown only after the session closes. |
| `instructions` | Markdown | no | — | Shown before starting. |
| `order.shuffle_questions` | bool | no | `true` | Per-attempt, seed stored. |
| `order.shuffle_options` | bool | no | `true` | Options with `pin: true` keep their position. |
| `proctoring.profile` | `off` \| `standard` \| `strict` | no | `standard` | Presets; individual settings override them (phase 5). |
| `items` | list | yes | — | Ordered list of `question` or `pool` entries. |
| `branding.accent` | hex color | no | — | Accent color for the student UI. |
| `branding.logo` | path | no | — | File under `assets/`. |

### `items`

```yaml
items:
  - question: q001          # a fixed question
  - pool:                   # draw `pick` questions from `from`, per attempt
      id: problemas
      pick: 2
      from: [q004, q005, q007]
```

- Every question referenced must exist; a question may appear only once across
  all items and pools.
- `1 <= pick < len(from)`.
- All questions of a pool must award the same `points` (fairness).

## Question files (`questions/*.yaml`)

Common keys:

| Key | Type | Required | Notes |
|---|---|---|---|
| `id` | id | yes | Matches the file name. |
| `type` | `single` \| `multiple` \| `truefalse` \| `short` | yes | `essay` and `parametric` are reserved. |
| `prompt` | Markdown | yes | |
| `time_limit` | duration | no | Per-question limit; the question locks when it expires. |
| `reveal` | `always` \| `hover` | no | Overrides the proctoring profile for this question. |

Type-specific keys:

- `single`, `multiple`: `options` — list of `{id, text (Markdown), pin?}`;
  2–10 options, ids unique within the question.
- `truefalse`: no extra keys.
- `short`: `input: number | text` (required), `placeholder` (optional).

## `private/answer-key.yaml`

```yaml
format: assess/v1
exam: algebra-01            # must equal manifest id
answers:
  q001: { option: b, points: 1 }                       # single
  q002: { options: [a, b, d], partial: true, points: 2 } # multiple
  q003: { value: false, points: 1 }                    # truefalse
  q004: { number: 4, tolerance: 0.01, points: 2 }      # short / number
  q006:                                                # short / text
    accept: [coeficiente]
    normalize: [trim, casefold, collapse_spaces, strip_accents]
    points: 1
```

- Every question has exactly one entry, and the entry's keys must match the
  question type.
- `points` is a positive number. There is no negative marking.
- `multiple` is all-or-nothing unless `partial: true`; partial credit is
  `points × max(0, (correct_selected − wrong_selected) / total_correct)`.
- `number` compares `|given − number| <= tolerance` (default `0`).
- `accept` lists accepted texts after applying `normalize` steps to both sides.

## Publication

`ax start` validates the folder and stores an **immutable published version**
(content plus SHA-256 hash). Editing the YAML later never affects a running
session, and results always reference the hash they were graded against.
