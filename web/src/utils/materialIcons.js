/**
 * Returns a short, human-readable label for a material's type (for display instead of raw MIME type).
 * @param {{ filename?: string, content_type?: string }} material - or just { content_type, filename }
 * @returns {string} e.g. "Word document", "PDF", "Image"
 */
export function getMaterialTypeLabel(material) {
  const fn = (material?.filename || '').toLowerCase()
  const ct = (material?.content_type || '').toLowerCase()
  const ext = fn.includes('.') ? fn.slice(fn.lastIndexOf('.')) : ''

  if (ct.includes('pdf') || ext === '.pdf') return 'PDF'
  if (ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg'].some(e => fn.endsWith(e))) return 'Image'
  if (ct.includes('wordprocessingml') || ct.includes('msword') || ['.docx', '.doc'].some(e => ext === e)) return 'Word document'
  if (ct.includes('spreadsheetml') || ct.includes('ms-excel') || ['.xlsx', '.xls', '.csv'].some(e => ext === e)) return 'Spreadsheet'
  if (ct.includes('presentationml') || ct.includes('ms-powerpoint') || ['.pptx', '.ppt'].some(e => ext === e)) return 'Presentation'
  if (['.txt', '.md'].includes(ext) || ct === 'text/plain' || ct.startsWith('text/')) return 'Text'
  if (ct || ext) return ext ? `Document (${ext})` : 'Document'
  return ''
}

/**
 * Returns an icon (emoji) for a material based on filename and content_type.
 * Used for per-item visual differentiation in material lists.
 * @param {{ filename?: string, content_type?: string }} material
 * @returns {string}
 */
export function getMaterialIcon(material) {
  const fn = (material?.filename || '').toLowerCase()
  const ct = (material?.content_type || '').toLowerCase()
  const ext = fn.includes('.') ? fn.slice(fn.lastIndexOf('.')) : ''

  if (ct.startsWith('image/') || ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.svg'].some(e => fn.endsWith(e))) {
    return '🖼'
  }
  if (ext === '.pdf' || ct.includes('pdf')) return '📄'
  if (['.txt', '.md'].includes(ext) || ct === 'text/plain' || ct.startsWith('text/')) return '📝'
  if (['.pptx', '.ppt'].includes(ext)) return '🖼'
  if (['.xlsx', '.xls', '.csv'].includes(ext)) return '📊'
  return '📄'
}
