// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'

// SCRUM-444/446: vitest matrix for the SlideDeckViewer dispatcher.
//
// pdfjs-dist + its worker URL are mocked at module load — we never decode a
// real PDF inside unit tests. The mocks track which page numbers were
// requested so the prev/next + initial-slide tests can assert without
// inspecting canvas pixels (jsdom does not implement Canvas 2D).

vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?url', () => ({ default: '/__test__/pdf.worker.mjs' }))

let mockGetDocumentFailures = 0
const mockGetPage = vi.fn()
const mockGetDocument = vi.fn()
const mockTextLayerRender = vi.fn(() => Promise.resolve())

vi.mock('pdfjs-dist', () => {
  const GlobalWorkerOptions = { workerSrc: '' }
  return {
    GlobalWorkerOptions,
    getDocument: (opts) => {
      mockGetDocument(opts)
      if (mockGetDocumentFailures > 0) {
        mockGetDocumentFailures -= 1
        return { promise: Promise.reject(new Error('UnexpectedResponseException: 403 from R2')) }
      }
      return {
        promise: Promise.resolve({
          numPages: 5,
          getPage: (n) => {
            mockGetPage(n)
            return Promise.resolve({
              getViewport: () => ({ width: 600, height: 800 }),
              render: () => ({ promise: Promise.resolve(), cancel: () => {} }),
              getTextContent: () => Promise.resolve({ items: [{ str: 'mocked slide text' }] }),
              streamTextContent: () => ({}),
              cleanup: () => {},
            })
          },
          destroy: () => {},
        }),
      }
    },
    TextLayer: class {
      constructor(opts) {
        this.opts = opts
      }
      render() { return mockTextLayerRender() }
    },
  }
})

import { SlideDeckViewer } from '../components/SlideDeckViewer'

const SESSION_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const MATERIAL_ID = 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb'

function jsonResponse(body, status = 200) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  })
}

beforeEach(() => {
  mockGetDocument.mockClear()
  mockGetPage.mockClear()
  mockTextLayerRender.mockClear()
  mockGetDocumentFailures = 0
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('SlideDeckViewer dispatcher — SCRUM-444/446', () => {
  it('SlideDeckViewer_RendersImgTag_OnLegacySlidesShape', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        slides: [
          { index: 1, image_url: 'https://example.com/s1.png' },
          { index: 2, image_url: 'https://example.com/s2.png' },
        ],
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    const img = await screen.findByAltText(/Slide 1/)
    expect(img).toBeInTheDocument()
    expect(img.tagName).toBe('IMG')
    expect(img.getAttribute('src')).toBe('https://example.com/s1.png')
    // PDF canvas must NOT have rendered.
    expect(screen.queryByTestId('slide-deck-viewer-pdf-canvas')).not.toBeInTheDocument()
    fetchMock.mockRestore()
  })

  it('SlideDeckViewer_RendersCanvas_OnPDFShape', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        format: 'pdf',
        slide_count: 5,
        pdf_url: 'https://example.com/deck.pdf?sig=abc',
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    const canvas = await screen.findByTestId('slide-deck-viewer-pdf-canvas')
    expect(canvas).toBeInTheDocument()
    expect(canvas.tagName).toBe('CANVAS')
    expect(screen.queryByAltText(/Slide \d/)).not.toBeInTheDocument()
    fetchMock.mockRestore()
  })

  it('SlideDeckViewer_PrevNext_LegacyPath', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        slides: [
          { index: 1, image_url: 'https://example.com/s1.png' },
          { index: 2, image_url: 'https://example.com/s2.png' },
          { index: 3, image_url: 'https://example.com/s3.png' },
        ],
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    const img = await screen.findByAltText(/Slide 1/)
    expect(img.getAttribute('src')).toBe('https://example.com/s1.png')

    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    await waitFor(() => {
      expect(screen.getByAltText(/Slide 2/).getAttribute('src')).toBe('https://example.com/s2.png')
    })

    fireEvent.click(screen.getByRole('button', { name: /previous/i }))
    await waitFor(() => {
      expect(screen.getByAltText(/Slide 1/).getAttribute('src')).toBe('https://example.com/s1.png')
    })
    fetchMock.mockRestore()
  })

  it('SlideDeckViewer_PrevNext_PDFPath', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        format: 'pdf',
        slide_count: 5,
        pdf_url: 'https://example.com/deck.pdf',
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    await screen.findByTestId('slide-deck-viewer-pdf-canvas')
    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(1))

    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(2))

    fireEvent.click(screen.getByRole('button', { name: /previous/i }))
    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(1))
    fetchMock.mockRestore()
  })

  it('SlideDeckViewer_InitialSlideProp_Honored_BothPaths', async () => {
    // Legacy: initialSlide=3 should render slide-3.png on first mount.
    const legacyFetch = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        slides: [
          { index: 1, image_url: 'https://example.com/s1.png' },
          { index: 2, image_url: 'https://example.com/s2.png' },
          { index: 3, image_url: 'https://example.com/s3.png' },
          { index: 4, image_url: 'https://example.com/s4.png' },
        ],
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} initialSlide={3} />)
    const img = await screen.findByAltText(/Slide 3/)
    expect(img.getAttribute('src')).toBe('https://example.com/s3.png')
    cleanup()
    legacyFetch.mockRestore()

    // PDF: initialSlide=3 should call getPage(3) first.
    mockGetPage.mockClear()
    const pdfFetch = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        format: 'pdf',
        slide_count: 5,
        pdf_url: 'https://example.com/deck.pdf',
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} initialSlide={3} />)
    await screen.findByTestId('slide-deck-viewer-pdf-canvas')
    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(3))
    pdfFetch.mockRestore()
  })

  it('SlideDeckViewer_RefetchOnPDFLoadError', async () => {
    // First getDocument call fails (simulating expired pdf_url) → refetch
    // slides API once → next getDocument call (with fresh URL) succeeds.
    mockGetDocumentFailures = 1
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        format: 'pdf',
        slide_count: 5,
        pdf_url: 'https://example.com/deck.pdf?retry=' + Math.random(),
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    await screen.findByTestId('slide-deck-viewer-pdf-canvas')
    // fetch was called at least twice: initial load + refetch after expiry.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    // getDocument was called at least twice (first failed, second with fresh URL).
    expect(mockGetDocument).toHaveBeenCalledTimes(2)
    fetchMock.mockRestore()
  })

  it('SlideDeckViewer_TextLayerRendered_PDFPath', async () => {
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        format: 'pdf',
        slide_count: 5,
        pdf_url: 'https://example.com/deck.pdf',
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    const canvas = await screen.findByTestId('slide-deck-viewer-pdf-canvas')
    const textLayer = await screen.findByTestId('slide-deck-viewer-pdf-text-layer')
    expect(canvas).toBeInTheDocument()
    expect(textLayer).toBeInTheDocument()
    // text-layer's render() ran (guards against silent omission).
    await waitFor(() => expect(mockTextLayerRender).toHaveBeenCalled())
    fetchMock.mockRestore()
  })

  it('SlideDeckViewer_WorkerURLConfigured', async () => {
    // Importing SlideDeckViewerPDF transitively sets GlobalWorkerOptions.workerSrc.
    // We force the lazy import to resolve here.
    const fetchMock = vi.spyOn(global, 'fetch').mockImplementation(() =>
      jsonResponse({
        material_id: MATERIAL_ID,
        format: 'pdf',
        slide_count: 5,
        pdf_url: 'https://example.com/deck.pdf',
      }),
    )
    render(<SlideDeckViewer apiBaseUrl="/api" sessionId={SESSION_ID} materialId={MATERIAL_ID} />)
    await screen.findByTestId('slide-deck-viewer-pdf-canvas')
    const pdfjs = await import('pdfjs-dist')
    expect(pdfjs.GlobalWorkerOptions.workerSrc).toBe('/__test__/pdf.worker.mjs')
    fetchMock.mockRestore()
  })
})
