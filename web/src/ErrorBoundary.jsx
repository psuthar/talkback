import React from 'react'

export class ErrorBoundary extends React.Component {
  state = { error: null, errorInfo: null }

  static getDerivedStateFromError(error) {
    return { error }
  }

  componentDidCatch(error, errorInfo) {
    this.setState({ errorInfo })
    console.error('TalkBack ErrorBoundary:', error, errorInfo)
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{
          padding: '24px',
          maxWidth: '800px',
          margin: '20px auto',
          fontFamily: 'system-ui, sans-serif',
          backgroundColor: '#fff5f5',
          border: '2px solid #c53030',
          borderRadius: '8px',
          color: '#742a2a'
        }}>
          <h1 style={{ margin: '0 0 16px', fontSize: '20px' }}>Something went wrong</h1>
          <pre style={{
            margin: '0 0 16px',
            padding: '12px',
            overflow: 'auto',
            fontSize: '13px',
            backgroundColor: '#fff',
            border: '1px solid #feb2b2',
            borderRadius: '4px',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word'
          }}>
            {this.state.error?.toString?.() ?? String(this.state.error)}
          </pre>
          {this.state.errorInfo?.componentStack && (
            <details style={{ marginTop: '12px' }}>
              <summary style={{ cursor: 'pointer', fontWeight: 'bold' }}>Component stack</summary>
              <pre style={{
                marginTop: '8px',
                padding: '12px',
                fontSize: '12px',
                overflow: 'auto',
                backgroundColor: '#fff',
                border: '1px solid #feb2b2',
                borderRadius: '4px',
                whiteSpace: 'pre-wrap'
              }}>
                {this.state.errorInfo.componentStack}
              </pre>
            </details>
          )}
        </div>
      )
    }
    return this.props.children
  }
}
