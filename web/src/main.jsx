import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { ErrorBoundary } from './ErrorBoundary'
import './index.css'

const root = document.getElementById('root')
if (!root) {
  console.error('Root element #root not found')
} else {
  try {
    ReactDOM.createRoot(root).render(
      <React.StrictMode>
        <ErrorBoundary>
          <App />
        </ErrorBoundary>
      </React.StrictMode>,
    )
  } catch (err) {
    console.error('Failed to render App:', err)
    root.innerHTML = `<div style="padding:24px;font-family:system-ui;color:#c53030;"><h1>Failed to load</h1><pre>${String(err?.message || err)}</pre></div>`
  }
}
