#!/usr/bin/env python3
"""Finalise the generated deck: the mockups use the real CLI and a teacher role."""
import os
import re
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SKILL = os.path.join(HERE, "..", "..", ".agents", "skills", "logo-design", "scripts")
sys.path.insert(0, SKILL)
import render_png  # noqa: E402

HTML = os.path.join(HERE, "presentation.html")
SLIDES = os.path.join(HERE, "slides")

REPL = {
    "Event badge": "Teacher badge",
    "Speaker": "Teacher",
    "$ assess --version": "$ ax --version",
    '$ assess search "error"': '$ ax search "error"',
    "https://assess.com": "http://ax.local",
}

html = open(HTML, encoding="utf-8").read()
for a, b in REPL.items():
    html = html.replace(a, b)
open(HTML, "w", encoding="utf-8").write(html)

css = re.search(r"<style>(.*?)</style>", html, re.S).group(1)
slides = re.findall(r'<section class="slide">.*?</section>', html, re.S)
solo = css + "body{background:#fff}.slide{margin:0;box-shadow:none;min-height:800px}"
os.makedirs(SLIDES, exist_ok=True)
for i, sl in enumerate(slides, 1):
    with tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8") as fh:
        fh.write(f"<!doctype html><html><head><meta charset='utf-8'><style>{solo}</style></head>"
                 f"<body>{sl}</body></html>")
        tmp = fh.name
    out = os.path.join(SLIDES, f"slide-{i:02d}.png")
    render_png.screenshot_html(tmp, out, 1280, 800)
    os.unlink(tmp)
    print("wrote", out)
