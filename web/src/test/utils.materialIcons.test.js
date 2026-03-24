import { describe, it, expect } from 'vitest'
import { getMaterialTypeLabel, getMaterialIcon } from '../utils/materialIcons'

describe('getMaterialTypeLabel', () => {
  it('returns PDF for pdf content type', () => {
    expect(getMaterialTypeLabel({ content_type: 'application/pdf' })).toBe('PDF')
  })

  it('returns PDF for .pdf filename', () => {
    expect(getMaterialTypeLabel({ filename: 'report.pdf', content_type: '' })).toBe('PDF')
  })

  it('returns Image for image content type', () => {
    expect(getMaterialTypeLabel({ content_type: 'image/png', filename: '' })).toBe('Image')
  })

  it('returns Image for .jpg filename', () => {
    expect(getMaterialTypeLabel({ filename: 'photo.jpg', content_type: '' })).toBe('Image')
  })

  it('returns Image for .svg filename', () => {
    expect(getMaterialTypeLabel({ filename: 'icon.svg', content_type: '' })).toBe('Image')
  })

  it('returns Word document for wordprocessingml content type', () => {
    expect(getMaterialTypeLabel({ content_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' })).toBe('Word document')
  })

  it('returns Word document for .docx extension', () => {
    expect(getMaterialTypeLabel({ filename: 'report.docx', content_type: '' })).toBe('Word document')
  })

  it('returns Spreadsheet for .xlsx extension', () => {
    expect(getMaterialTypeLabel({ filename: 'data.xlsx', content_type: '' })).toBe('Spreadsheet')
  })

  it('returns Presentation for .pptx extension', () => {
    expect(getMaterialTypeLabel({ filename: 'slides.pptx', content_type: '' })).toBe('Presentation')
  })

  it('returns Text for .md extension', () => {
    expect(getMaterialTypeLabel({ filename: 'readme.md', content_type: '' })).toBe('Text')
  })

  it('returns Text for text/plain content type', () => {
    expect(getMaterialTypeLabel({ content_type: 'text/plain', filename: '' })).toBe('Text')
  })

  it('returns Document with extension for unknown extension', () => {
    expect(getMaterialTypeLabel({ filename: 'file.xyz', content_type: '' })).toBe('Document (.xyz)')
  })

  it('returns empty string for no type info', () => {
    expect(getMaterialTypeLabel({ filename: '', content_type: '' })).toBe('')
  })

  it('handles null/undefined material gracefully', () => {
    expect(getMaterialTypeLabel(null)).toBe('')
    expect(getMaterialTypeLabel(undefined)).toBe('')
  })
})

describe('getMaterialIcon', () => {
  it('returns image icon for image content type', () => {
    expect(getMaterialIcon({ content_type: 'image/jpeg', filename: '' })).toBe('🖼')
  })

  it('returns pdf icon for pdf content type', () => {
    expect(getMaterialIcon({ content_type: 'application/pdf', filename: '' })).toBe('📄')
  })

  it('returns pdf icon for .pdf filename', () => {
    expect(getMaterialIcon({ filename: 'doc.pdf', content_type: '' })).toBe('📄')
  })

  it('returns text icon for .txt filename', () => {
    expect(getMaterialIcon({ filename: 'notes.txt', content_type: '' })).toBe('📝')
  })

  it('returns text icon for .md filename', () => {
    expect(getMaterialIcon({ filename: 'readme.md', content_type: '' })).toBe('📝')
  })

  it('returns image icon for .pptx filename', () => {
    expect(getMaterialIcon({ filename: 'slides.pptx', content_type: '' })).toBe('🖼')
  })

  it('returns spreadsheet icon for .xlsx filename', () => {
    expect(getMaterialIcon({ filename: 'data.xlsx', content_type: '' })).toBe('📊')
  })

  it('returns default icon for unknown type', () => {
    expect(getMaterialIcon({ filename: 'file.bin', content_type: 'application/octet-stream' })).toBe('📄')
  })

  it('handles null material gracefully', () => {
    expect(getMaterialIcon(null)).toBe('📄')
  })
})
