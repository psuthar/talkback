import { useEffect, useRef, useState } from 'react'

function dispatchWebSocketPayload(raw, handler) {
  if (!handler) return
  // Backend may batch multiple JSON messages in one frame separated by newlines.
  const text = typeof raw === 'string' ? raw : String(raw ?? '')
  const parts = text.split('\n')
  for (const part of parts) {
    const trimmed = part.trim()
    if (!trimmed) continue
    try {
      const data = JSON.parse(trimmed)
      console.log('WebSocket: Raw message received:', data)
      handler(data)
    } catch (err) {
      console.error('Error parsing WebSocket message chunk:', err, 'Raw chunk:', trimmed)
    }
  }
}

export function useWebSocket(url, onMessage, onError) {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef(null)
  const reconnectTimeoutRef = useRef(null)
  const reconnectAttemptsRef = useRef(0)
  const maxReconnectAttempts = 5
  const reconnectDelay = 3000 // 3 seconds

  // Use refs for callbacks to avoid reconnection loops
  const onMessageRef = useRef(onMessage)
  const onErrorRef = useRef(onError)

  // Update refs when callbacks change
  useEffect(() => {
    onMessageRef.current = onMessage
  }, [onMessage])

  useEffect(() => {
    onErrorRef.current = onError
  }, [onError])

  // Update WebSocket message handlers when callbacks change (without reconnecting)
  useEffect(() => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      const ws = wsRef.current
      
      // Update onmessage handler
      ws.onmessage = (event) => {
        dispatchWebSocketPayload(event.data, onMessageRef.current)
      }
      
      // Update onerror handler
      ws.onerror = (error) => {
        console.error('WebSocket error:', error)
        if (onErrorRef.current) {
          onErrorRef.current(error)
        }
      }
    }
  }, [onMessage, onError])

  useEffect(() => {
    if (!url) {
      // Close connection if URL is cleared
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
        setConnected(false)
      }
      return
    }

    const connect = () => {
      try {
        // Convert http:// to ws:// or https:// to wss://
        let wsUrl = url
        if (url.startsWith('http://')) {
          wsUrl = url.replace('http://', 'ws://')
        } else if (url.startsWith('https://')) {
          wsUrl = url.replace('https://', 'wss://')
        }
        console.log('WebSocket: Attempting to connect to:', wsUrl)
        const ws = new WebSocket(wsUrl)
        wsRef.current = ws

        ws.onopen = () => {
          console.log('WebSocket connected to:', wsUrl)
          setConnected(true)
          reconnectAttemptsRef.current = 0
        }

        ws.onmessage = (event) => {
          dispatchWebSocketPayload(event.data, onMessageRef.current)
        }

        ws.onerror = (error) => {
          console.error('WebSocket error:', error)
          if (onErrorRef.current) {
            onErrorRef.current(error)
          }
        }

        ws.onclose = (event) => {
          console.log('WebSocket disconnected. Code:', event.code, 'Reason:', event.reason, 'WasClean:', event.wasClean)
          setConnected(false)
          wsRef.current = null

          // Attempt to reconnect only if URL is still valid and close was not intentional
          if (url && reconnectAttemptsRef.current < maxReconnectAttempts && event.code !== 1000) {
            reconnectAttemptsRef.current++
            reconnectTimeoutRef.current = setTimeout(() => {
              console.log(`Reconnecting WebSocket (attempt ${reconnectAttemptsRef.current})...`)
              connect()
            }, reconnectDelay)
          } else if (reconnectAttemptsRef.current >= maxReconnectAttempts) {
            console.error('Max WebSocket reconnection attempts reached')
          }
        }
      } catch (err) {
        console.error('Error creating WebSocket:', err)
        setConnected(false)
      }
    }

    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
      setConnected(false)
    }
  }, [url]) // Only depend on url to avoid reconnecting when callbacks change

  return { connected, ws: wsRef.current }
}
