// Student UI strings. The texts live in locales/<lang>.yaml (section web:)
// and the server injects the exam language's catalog into index.html, so
// the bundle carries no dictionaries and any translated language works.
import type { Key } from "./i18n.keys";

export type { Key };

type Dict = Partial<Record<Key, string>>;

function injected(): Dict | null {
  const el = document.getElementById("ax-i18n");
  if (!el?.textContent) return null;
  try {
    return JSON.parse(el.textContent) as Dict;
  } catch {
    return null;
  }
}

let current: Dict = injected() ?? {};

/**
 * loadStrings fetches the catalog when the page did not come with one (the
 * Vite dev server). It resolves at once for pages served by ax.
 */
export async function loadStrings(): Promise<void> {
  if (Object.keys(current).length > 0) return;
  try {
    const res = await fetch("/api/i18n");
    if (res.ok) current = (await res.json()) as Dict;
  } catch {
    // Keys are shown as they are; the page still works.
  }
}

/** setLanguage marks the document's language (for screen readers and number formats). */
export function setLanguage(lang: string): void {
  document.documentElement.lang = lang;
}

/** t looks up a string and fills {placeholders}. */
export function t(key: Key, vars: Record<string, string | number> = {}): string {
  return (current[key] ?? key).replace(/\{(\w+)\}/g, (_, k: string) => String(vars[k] ?? ""));
}

export function points(n: number): string {
  const v = Number.isInteger(n) ? String(n) : n.toLocaleString(document.documentElement.lang, { maximumFractionDigits: 2 });
  return t(n === 1 ? "pts_one" : "pts_many", { n: v });
}
