// Builds canvas.html from canvas.src.html and _cells/cells.json.
// Run: node internal/tui/design/build.js
const fs = require('fs'), path = require('path');
const cells = JSON.parse(fs.readFileSync(path.join(__dirname, '_cells', 'cells.json'), 'utf8'));
for (const k in cells) {
  if (Array.isArray(cells[k])) {
    // Merge neighbouring runs of the same role to keep the page small.
    cells[k] = cells[k].map(row => row.reduce((out, run) => {
      const last = out[out.length - 1];
      if (last && last.r === run.r) last.t += run.t; else out.push({ ...run });
      return out;
    }, []));
  }
}
const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'en.json'), 'utf8'));
const src = fs.readFileSync(path.join(__dirname, 'canvas.src.html'), 'utf8')
  .replace('/*CELLS*/null', () => JSON.stringify(cells)).replace('/*EN*/{}', () => JSON.stringify(en));
fs.writeFileSync(path.join(__dirname, 'canvas.html'),
  '<!doctype html>\n<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0">\n' + src + '\n</body></html>\n');
console.log('canvas.html', src.length);
