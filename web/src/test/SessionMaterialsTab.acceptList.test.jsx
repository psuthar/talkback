import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const MATERIALS_TAB = path.resolve(__dirname, '../components/SessionMaterialsTab.jsx')
const CREATOR_MODE = path.resolve(__dirname, '../modes/CreatorMode.jsx')

// SCRUM-330 — the upload `<input accept=...>` allowlist must include CSV and
// XLS (legacy binary Excel) extensions and their MIME types so the OS file
// picker does not grey out customers-100.csv-style attachments. We assert by
// reading the source files because the input is rendered deep inside complex
// containers (CreatorMode is several thousand lines and SessionMaterialsTab
// has its own dependency graph). A plain text-search keeps the test fast and
// scoped to the bug we are guarding against.

const REQUIRED_TOKENS = [
  '.csv',                          // CSV extension (OS picker)
  '.xls',                          // legacy Excel extension
  'text/csv',                      // CSV MIME (OS picker on some platforms)
  'application/vnd.ms-excel',      // legacy Excel MIME
]

function readAcceptValue(file, anchor) {
  const src = fs.readFileSync(file, 'utf8')
  const idx = src.indexOf(anchor)
  if (idx === -1) throw new Error(`Anchor not found in ${file}: ${anchor}`)
  // Find the accept attribute on the same line as / after the anchor
  const tail = src.slice(idx)
  const m = /accept="([^"]+)"/.exec(tail)
  if (!m) throw new Error(`No accept attribute found near anchor in ${file}`)
  return m[1]
}

describe('SCRUM-330 — upload picker accepts CSV and XLS', () => {
  it('SessionMaterialsTab.jsx accept list contains CSV and XLS extensions and MIME types', () => {
    const accept = readAcceptValue(MATERIALS_TAB, 'fileInputRef')
    for (const token of REQUIRED_TOKENS) {
      expect(accept, `expected accept to include "${token}"`).toContain(token)
    }
  })

  it('CreatorMode.jsx materialFileInputRef accept list contains CSV and XLS extensions and MIME types', () => {
    const accept = readAcceptValue(CREATOR_MODE, 'materialFileInputRef')
    for (const token of REQUIRED_TOKENS) {
      expect(accept, `expected accept to include "${token}"`).toContain(token)
    }
  })

  it('does not regress XLSX (already-supported sibling) in either accept list', () => {
    const sib = ['.xlsx', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet']
    const a1 = readAcceptValue(MATERIALS_TAB, 'fileInputRef')
    const a2 = readAcceptValue(CREATOR_MODE, 'materialFileInputRef')
    for (const token of sib) {
      expect(a1).toContain(token)
      expect(a2).toContain(token)
    }
  })
})
