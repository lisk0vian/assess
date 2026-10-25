import type { ExamView } from "./api/types";

let logo = "";

/**
 * applyBrand takes the exam's accent and logo from the manifest. The accent
 * replaces the magenta everywhere it marks "yours"; its text and wash tones
 * are derived so contrast holds in light and dark themes.
 */
export function applyBrand(exam: ExamView): void {
  const root = document.documentElement;
  if (exam.accent && /^#[0-9a-f]{3}([0-9a-f]{3})?$/i.test(exam.accent)) {
    root.style.setProperty("--brand", exam.accent);
    root.dataset.brand = "";
  } else {
    root.style.removeProperty("--brand");
    delete root.dataset.brand;
  }
  logo = exam.logo ? `/api/${exam.logo}` : "";
}

/** URL of the exam's logo, or "" when it has none. */
export const brandLogo = () => logo;
