import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const __dirname = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(resolve(__dirname, '../components/DecisionBar.module.css'), 'utf8')

function getRuleBody(source, selector) {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`${esc}\\s*\\{([\\s\\S]*?)\\}`, 'm')
  const match = source.match(re)
  return match ? match[1] : ''
}

function hasDecl(ruleBody, prop, value) {
  const re = new RegExp(`${prop}\\s*:\\s*${value}\\s*;`)
  return re.test(ruleBody)
}

describe('DecisionBar rationale layout contract (SCRUM-163)', () => {
  it('keeps trigger and rationale on one row by default', () => {
    const triggerRow = getRuleBody(css, '.triggerRow')
    expect(triggerRow).toBeTruthy()
    expect(hasDecl(triggerRow, 'display', 'flex')).toBe(true)
    expect(hasDecl(triggerRow, 'flex-wrap', 'nowrap')).toBe(true)
    expect(hasDecl(triggerRow, 'min-width', '0')).toBe(true)
  })

  it('uses a rationale footprint that truncates width before wrapping and keeps multiline support', () => {
    const rationaleBlock = getRuleBody(css, '.rationaleBlock')
    const rationaleInput = getRuleBody(css, '.rationaleInput')
    expect(rationaleBlock).toBeTruthy()
    expect(hasDecl(rationaleBlock, 'flex', '1 1 auto')).toBe(true)
    expect(hasDecl(rationaleBlock, 'min-width', '0')).toBe(true)
    expect(rationaleInput).toBeTruthy()
    expect(hasDecl(rationaleInput, 'flex', '1 1 220px')).toBe(true)
    expect(hasDecl(rationaleInput, 'min-width', '120px')).toBe(true)
    expect(hasDecl(rationaleInput, 'max-width', '360px')).toBe(true)
    expect(hasDecl(rationaleInput, 'min-height', '34px')).toBe(true)
    expect(hasDecl(rationaleInput, 'resize', 'vertical')).toBe(true)
  })
})
