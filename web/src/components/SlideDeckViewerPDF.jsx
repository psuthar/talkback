import { useEffect, useRef, useState } from 'react'
import * as pdfjsLib from 'pdfjs-dist'
import pdfWorkerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'

// SCRUM-444/446: configure the pdfjs worker URL once at module load. Vite's
// ?url import emits the worker as a same-origin asset so we avoid the CDN
// fallback (which would require a CSP exception) and the very-slow main-thread
// fallback that fires silently when workerSrc is empty.
if (pdfjsLib.GlobalWorkerOptions && !pdfjsLib.GlobalWorkerOptions.workerSrc) {
  pdfjsLib.GlobalWorkerOptions.workerSrc = pdfWorkerUrl
}

/**
 * SlideDeckViewerPDF renders a single PDF page on a canvas plus a transparent
 * text-layer div so users can select and copy slide text — the headline
 * upside of the SCRUM-444 pipeline migration. Loaded lazily by SlideDeckViewer
 * so legacy PNG sessions never pay the ~430 KB gzipped pdfjs cost.
 *
 * Refetch contract: when getDocument or getPage rejects with an URL/auth-like
 * error (typically expired presigned URL after a long session), call
 * onRefetch() once and re-load with the fresh URL. The parent's refetch
 * callback is responsible for getting a new pdf_url via getMaterialSlides;
 * this component never holds a stale URL across retries.
 *
 * @param {object} props
 * @param {string} props.pdfUrl
 * @param {number} props.slideCount
 * @param {number} [props.initialSlide] 1-based; clamped to [1, slideCount].
 * @param {() => Promise<string|null>} [props.onRefetch] Returns a fresh pdf_url, or null/undefined.
 */
export function SlideDeckViewerPDF({ pdfUrl, slideCount, initialSlide, onRefetch }) {
  const canvasRef = useRef(null)
  const textLayerRef = useRef(null)
  const pdfDocRef = useRef(null)
  const renderTaskRef = useRef(null)
  const hasRefetchedRef = useRef(false)

  const safeInitial = Math.max(1, Math.min(slideCount || 1, initialSlide ?? 1))
  const [currentPage, setCurrentPage] = useState(safeInitial)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  // SCRUM-449: do NOT reset hasRefetchedRef on pdfUrl change. The previous
  // reset turned the one-shot latch into a no-op because onRefetch produces a
  // fresh presigned pdf_url, which would immediately re-arm the latch and let
  // a permanently-broken deck (e.g. R2 CORS blocking the fetch) drive an
  // infinite refetch loop. Latch reset happens naturally on component
  // remount, which the parent dispatcher already does whenever materialId
  // changes (it sets loading=true → SlideDeckViewerPDF unmounts → next mount
  // gets a fresh ref).
  //
  // Citation jumps for the same deck still need to update currentPage when
  // the parent passes a new initialSlide; do that explicitly, without
  // touching the retry latch.
  useEffect(() => {
    if (initialSlide === undefined || initialSlide === null) return
    setCurrentPage(Math.max(1, Math.min(slideCount || 1, initialSlide)))
  }, [initialSlide, slideCount])

  // Load the document. Re-runs only when pdfUrl changes (e.g. after a refetch).
  useEffect(() => {
    if (!pdfUrl) return
    let cancelled = false
    setLoading(true)
    setError(null)

    const loadingTask = pdfjsLib.getDocument({ url: pdfUrl })
    loadingTask.promise.then(
      (pdf) => {
        if (cancelled) {
          pdf.destroy && pdf.destroy()
          return
        }
        pdfDocRef.current = pdf
        setLoading(false)
      },
      async (err) => {
        if (cancelled) return
        if (!hasRefetchedRef.current && onRefetch) {
          hasRefetchedRef.current = true
          try {
            const fresh = await onRefetch()
            if (cancelled) return
            // Parent updates pdfUrl -> this effect re-runs with the new URL.
            if (fresh) return
          } catch {
            /* fall through to error */
          }
        }
        setError(err?.message || 'Unable to load slide PDF.')
        setLoading(false)
      }
    )

    return () => {
      cancelled = true
      if (renderTaskRef.current) {
        try { renderTaskRef.current.cancel() } catch { /* ignore */ }
        renderTaskRef.current = null
      }
      if (pdfDocRef.current) {
        try { pdfDocRef.current.destroy && pdfDocRef.current.destroy() } catch { /* ignore */ }
        pdfDocRef.current = null
      }
    }
  }, [pdfUrl, onRefetch])

  // Render the current page whenever it changes (or after the doc loads).
  useEffect(() => {
    const pdf = pdfDocRef.current
    if (!pdf || loading) return
    let cancelled = false

    const run = async () => {
      // Cancel any in-flight render before starting a new one — switching
      // pages mid-render would otherwise leave two RenderTasks fighting for
      // the same canvas context.
      if (renderTaskRef.current) {
        try { renderTaskRef.current.cancel() } catch { /* ignore */ }
        renderTaskRef.current = null
      }
      try {
        const page = await pdf.getPage(currentPage)
        if (cancelled) return
        const viewport = page.getViewport({ scale: 1.5 })
        const canvas = canvasRef.current
        if (!canvas) return
        canvas.width = viewport.width
        canvas.height = viewport.height
        const ctx = canvas.getContext('2d')
        const renderTask = page.render({ canvasContext: ctx, viewport })
        renderTaskRef.current = renderTask
        await renderTask.promise
        if (cancelled) return

        // Text layer renders behind/over the canvas so users can select text.
        const textLayerDiv = textLayerRef.current
        if (textLayerDiv) {
          textLayerDiv.innerHTML = ''
          textLayerDiv.style.width = `${viewport.width}px`
          textLayerDiv.style.height = `${viewport.height}px`
          const textContent = await page.getTextContent()
          if (cancelled) return
          if (pdfjsLib.TextLayer) {
            const textLayer = new pdfjsLib.TextLayer({
              textContentSource: textContent,
              container: textLayerDiv,
              viewport,
            })
            await textLayer.render()
          }
        }
      } catch (err) {
        if (cancelled) return
        if (err && err.name === 'RenderingCancelledException') return
        if (!hasRefetchedRef.current && onRefetch) {
          hasRefetchedRef.current = true
          try {
            const fresh = await onRefetch()
            if (cancelled || fresh) return
          } catch {
            /* fall through */
          }
        }
        setError(err?.message || 'Unable to render slide.')
      }
    }
    run()

    return () => {
      cancelled = true
      if (renderTaskRef.current) {
        try { renderTaskRef.current.cancel() } catch { /* ignore */ }
        renderTaskRef.current = null
      }
    }
  }, [currentPage, loading, onRefetch])

  const handlePrev = () => setCurrentPage((p) => Math.max(1, p - 1))
  const handleNext = () => setCurrentPage((p) => Math.min(slideCount, p + 1))

  if (loading) {
    return (
      <div data-testid="slide-deck-viewer-pdf" style={{ padding: '24px', color: '#666' }}>
        Loading slides…
      </div>
    )
  }
  if (error) {
    return (
      <div data-testid="slide-deck-viewer-pdf" style={{ padding: '24px', color: 'var(--color-danger-dark)' }}>
        Unable to load slides.
      </div>
    )
  }

  return (
    <div data-testid="slide-deck-viewer-pdf" className="slide-deck-viewer" style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <div
        className="slide-deck-toolbar"
        style={{
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '12px',
          padding: '8px 0 12px',
          borderBottom: '1px solid #e0e0e0',
          marginBottom: '12px',
        }}
      >
        <button
          type="button"
          onClick={handlePrev}
          disabled={currentPage <= 1}
          style={{
            padding: '6px 14px',
            fontSize: '13px',
            cursor: currentPage <= 1 ? 'not-allowed' : 'pointer',
            opacity: currentPage <= 1 ? 0.6 : 1,
          }}
        >
          Previous
        </button>
        <div style={{ fontSize: '13px', color: '#333' }}>
          Slide {currentPage} of {slideCount}
        </div>
        <button
          type="button"
          onClick={handleNext}
          disabled={currentPage >= slideCount}
          style={{
            padding: '6px 14px',
            fontSize: '13px',
            cursor: currentPage >= slideCount ? 'not-allowed' : 'pointer',
            opacity: currentPage >= slideCount ? 0.6 : 1,
          }}
        >
          Next
        </button>
      </div>
      <div
        className="slide-deck-stage"
        style={{
          flex: 1,
          minHeight: 0,
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'flex-start',
          overflow: 'auto',
        }}
      >
        <div style={{ position: 'relative' }}>
          <canvas ref={canvasRef} data-testid="slide-deck-viewer-pdf-canvas" style={{ display: 'block', maxWidth: '100%', height: 'auto' }} />
          <div
            ref={textLayerRef}
            data-testid="slide-deck-viewer-pdf-text-layer"
            className="textLayer"
            style={{
              position: 'absolute',
              left: 0,
              top: 0,
              right: 0,
              bottom: 0,
              overflow: 'hidden',
              opacity: 0.2,
              lineHeight: 1.0,
              userSelect: 'text',
            }}
          />
        </div>
      </div>
    </div>
  )
}

export default SlideDeckViewerPDF
