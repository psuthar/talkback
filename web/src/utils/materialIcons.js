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
