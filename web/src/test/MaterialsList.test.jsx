import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MaterialsList } from '../components/MaterialsList'

const sampleMaterials = [
  { id: 'mat-1', filename: 'report.pdf', kind: 'document', content_type: 'application/pdf', text_status: 'ready' },
  { id: 'mat-2', filename: 'notes.txt', kind: 'document', content_type: 'text/plain', text_status: 'pending' },
]

describe('MaterialsList', () => {
  it('renders nothing when materials is empty', () => {
    const { container } = render(<MaterialsList materials={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders nothing when materials is null', () => {
    const { container } = render(<MaterialsList materials={null} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders materials count in heading', () => {
    render(<MaterialsList materials={sampleMaterials} />)
    expect(screen.getByText('Materials (2)')).toBeTruthy()
  })

  it('renders each material filename', () => {
    render(<MaterialsList materials={sampleMaterials} />)
    expect(screen.getByText(/report\.pdf/)).toBeTruthy()
    expect(screen.getByText(/notes\.txt/)).toBeTruthy()
  })

  it('does not render delete button when no sessionId/apiBaseUrl/refetchSession', () => {
    render(<MaterialsList materials={sampleMaterials} />)
    expect(screen.queryByRole('button', { name: /delete/i })).toBeNull()
  })

  it('renders delete buttons when canDelete is true', () => {
    render(
      <MaterialsList
        materials={sampleMaterials}
        sessionId="sess-1"
        apiBaseUrl=""
        refetchSession={() => {}}
      />
    )
    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    expect(deleteButtons).toHaveLength(2)
  })

  it('shows N/A status for image materials', () => {
    const imageMaterials = [
      { id: 'img-1', filename: 'photo.jpg', kind: 'document', content_type: 'image/jpeg', text_status: 'ready' }
    ]
    render(<MaterialsList materials={imageMaterials} />)
    expect(screen.getByText('N/A')).toBeTruthy()
  })

  it('renders extracted text preview when present', () => {
    const materials = [
      { id: 'm1', filename: 'doc.pdf', kind: 'document', content_type: 'application/pdf', text_status: 'ready', extracted_text: 'Hello from the document.' }
    ]
    render(<MaterialsList materials={materials} />)
    expect(screen.getByText(/Hello from the document\./)).toBeTruthy()
  })

  it('truncates extracted text longer than 500 chars', () => {
    const longText = 'A'.repeat(600)
    const materials = [
      { id: 'm1', filename: 'doc.pdf', kind: 'document', content_type: 'application/pdf', text_status: 'ready', extracted_text: longText }
    ]
    render(<MaterialsList materials={materials} />)
    expect(screen.getByText(/A{500}\.\.\./)).toBeTruthy()
  })
})
