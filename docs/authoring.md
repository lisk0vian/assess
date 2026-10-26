# Writing an exam with an AI assistant

This guide is for AI assistants (and people) writing an Assess exam as files.
It is self-contained: follow it and `ax validate` will pass. The complete
reference is [format.md](format.md); `ax format` prints both.

Teachers who prefer not to touch files can use `ax new` (a guided wizard)
or `ax edit <folder>` instead.

## Workflow

1. Create the folder, either from the template (`ax new <folder>`, then
   replace the sample questions) or by writing the files below from scratch.
2. Write `manifest.yaml`, one file per question in `questions/`, and the
   answer key in `private/answer-key.yaml`.
3. Run `ax validate --json <folder>` and fix every problem it lists, until
   it reports `"valid": true`. Each problem names the file, line and field.
4. Hand the folder to the teacher: `ax edit <folder>` to review it, then
   `ax start <folder>`.

## A complete exam

Copy this structure. Every block below is a real file; together they form a
valid exam (this is checked by the test suite).

```yaml
# file: manifest.yaml
format: assess/v1
id: revolucion-industrial        # must match the folder name
title: "Historia — La Revolución Industrial"
language: es                     # student UI language (en, es...); omitted: the teacher's setting
duration: 30m                    # Go durations: 90s, 30m, 1h30m
result_visibility: score         # none | score | review

instructions: |
  Lee cada pregunta con atención. Tus respuestas se guardan solas.

order:
  shuffle_questions: true
  shuffle_options: true

proctoring:
  profile: standard              # off | standard | strict

items:                           # the order of the exam; every question once
  - question: q001
  - question: q002
  - question: q003
  - pool:                        # each student gets `pick` of these
      id: fechas
      pick: 1                    # must be less than the number in `from`
      from: [q004, q005]
```

```yaml
# file: questions/q001.yaml
id: q001                         # must match the file name
type: single                     # exactly one right option
prompt: |
  ¿Qué invento se asocia con **James Watt**?
options:
  - id: a
    text: La máquina de vapor mejorada
  - id: b
    text: El telar mecánico
  - id: c
    text: La locomotora
  - id: d
    text: Ninguna de las anteriores
    pin: true                    # stays last when options are shuffled
```

```yaml
# file: questions/q002.yaml
id: q002
type: multiple                   # one or more right options
prompt: |
  ¿Cuáles de estos sectores cambiaron con la Revolución Industrial?
  Marca **todas** las correctas.
options:
  - id: a
    text: El textil
  - id: b
    text: El transporte
  - id: c
    text: La agricultura
```

```yaml
# file: questions/q003.yaml
id: q003
type: truefalse
prompt: La Revolución Industrial comenzó en Gran Bretaña.
```

```yaml
# file: questions/q004.yaml
id: q004
type: short
input: number                    # the student types a number
prompt: ¿En qué año patentó Watt su máquina de vapor?
time_limit: 2m                   # optional: the question locks after 2 minutes
```

```yaml
# file: questions/q005.yaml
id: q005
type: short
input: text                      # the student types a word or phrase
prompt: ¿Cómo se llamaba el movimiento obrero que destruía máquinas?
```

```yaml
# file: private/answer-key.yaml
format: assess/v1
exam: revolucion-industrial      # must equal the manifest id
answers:                         # one entry per question, keys by type
  q001:
    option: a                    # single: the right option id
    points: 1
  q002:
    options: [a, b, c]           # multiple: every right option id
    partial: true                # optional: partial credit instead of all-or-nothing
    points: 2
  q003:
    value: true                  # truefalse
    points: 1
  q004:
    number: 1769                 # short number
    tolerance: 0                 # optional: accepted difference
    points: 2
  q005:
    accept: [ludismo, luditas, movimiento ludita]   # short text: accepted answers
    normalize: [trim, casefold, collapse_spaces, strip_accents]
    points: 2                    # questions of one pool award the same points
```

## Checklist

- **Ids**: lowercase letters, digits, `-` and `_`, starting with a letter or
  digit. A question's `id` equals its file name (`questions/q004.yaml` →
  `id: q004`). The exam `id` should equal the folder name.
- **Items**: every question file appears exactly once in `items`, directly or
  in a pool. A pool picks fewer than it has (`1 <= pick < len(from)`), and all
  its questions award the same points.
- **Options**: 2 to 10 per `single`/`multiple` question, unique ids. Other
  types have no `options`.
- **Answer key**: exactly one entry per question; only the keys of its type:

  | Type | Keys |
  |---|---|
  | `single` | `option`, `points` |
  | `multiple` | `options`, `partial` (optional), `points` |
  | `truefalse` | `value`, `points` |
  | `short` + `input: number` | `number`, `tolerance` (optional), `points` |
  | `short` + `input: text` | `accept`, `normalize` (optional), `points` |

  `points` is greater than 0 (up to 1000). There is no negative marking.
- **Answers never go in question files.** Question files are sent to the
  browser; the key is not.
- **YAML strings**: unknown keys are errors. Quote option texts that YAML
  would read as something else: `"true"`, `"no"`, `"3.0"`, or text starting
  with `*`, `&`, `!`, `%`, `@`, `` ` ``, `[`, `{`, `>` or `|`, or containing
  `: ` or ` #`.
- **Markdown and math**: prompts and option texts are Markdown with `$…$`
  and `$$…$$` math. Write math-heavy text as a block (`prompt: |`) so
  backslashes stay as written; inside double quotes they must be doubled
  (`"$\\frac{1}{2}$"`).
- **Images**: put files in `assets/` and reference them relatively:
  `![Mapa](assets/mapa.svg)`. Only png, jpg, gif, webp and svg are served.
- **Photo protections**: `standard` and `strict` show prompts through a
  lens, one range of 4–15 words at a time (see `ax format`); each range
  repeats the last two words of the previous one so the student keeps the
  context. The first view reads them automatically; after that, tap the zone
  you want to read or use the controls under the prompt, swipe or the
  keyboard. Write `lens: false` on a question to show it whole, and use
  `proctoring.auto_read: false` or `proctoring.read_speed` to tune the first
  pass. Images and formulas count as one token when their turn comes.
- **Durations**: `duration` and `time_limit` use Go syntax (`90s`, `45m`,
  `1h30m`); a `time_limit` is at least `5s` and not longer than the exam.

## A request a teacher can give an AI

> Create an Assess exam (format `assess/v1`) in the folder `tema-3` about
> the Industrial Revolution for 15-year-old students, in Spanish: 10
> questions mixing single choice, multiple choice, true/false and short
> answers, 45 minutes, standard proctoring. Follow `ax format`, then run
> `ax validate --json tema-3` and fix every problem until it is valid.
