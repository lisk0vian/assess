# Translations

Each file is one language of the ax interface: the terminal (menus, panel,
editor, reports, command help) and the student web app (`web:` section).

- `en.yaml` is the reference. Every key exists there.
- To add a language, copy `en.yaml` to `<code>.yaml` (for example `pt.yaml`
  or `pt-BR.yaml`), set `language:` to the language's own name and translate
  the values. Keep the keys and the `{placeholders}` as they are.
- Missing keys fall back to English, so a partial translation already works.
- `ax language check` lists missing keys and placeholder mistakes.
- Teachers can try a catalog without rebuilding: put it in
  `~/.config/assess/locales/` and set `language: <code>` with `ax config`.
