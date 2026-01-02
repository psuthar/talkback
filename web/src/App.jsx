import { useState, useEffect } from 'react'

const API_BASE_URL_DEFAULT = 'http://localhost:8080'

function App() {
  const [apiBaseUrl, setApiBaseUrl] = useState(API_BASE_URL_DEFAULT)
  const [artifactId, setArtifactId] = useState('')
  const [videoId, setVideoId] = useState('')
  
  // Form states
  const [artifactTitle, setArtifactTitle] = useState('')
  const [artifactDescription, setArtifactDescription] = useState('')
  const [materialFile, setMaterialFile] = useState(null)
  const [materialKind, setMaterialKind] = useState('document')
  const [videoProvider, setVideoProvider] = useState('loom')
  const [videoUrl, setVideoUrl] = useState('')
  const [transcriptText, setTranscriptText] = useState('')
  const [questionText, setQuestionText] = useState('')
  
  // Response states - per-section feedback
  const [createArtifactFeedback, setCreateArtifactFeedback] = useState({ type: '', message: '' })
  const [uploadMaterialFeedback, setUploadMaterialFeedback] = useState({ type: '', message: '' })
  const [attachVideoFeedback, setAttachVideoFeedback] = useState({ type: '', message: '' })
  const [submitTranscriptFeedback, setSubmitTranscriptFeedback] = useState({ type: '', message: '' })
  const [askQuestionFeedback, setAskQuestionFeedback] = useState({ type: '', message: '' })
  const [questionHistoryFeedback, setQuestionHistoryFeedback] = useState({ type: '', message: '' })
  const [resetFeedback, setResetFeedback] = useState({ type: '', message: '' })
  
  // Global states
  const [loading, setLoading] = useState(false)
  const [currentAnswer, setCurrentAnswer] = useState(null)
  const [questions, setQuestions] = useState([])
  const [apiHealth, setApiHealth] = useState(null) // null = unknown, true = healthy, false = unhealthy
  const [healthChecking, setHealthChecking] = useState(false)
  const [resetConfirmText, setResetConfirmText] = useState('')
  const [showResetConfirm, setShowResetConfirm] = useState(false)

  const clearFeedback = (setter) => {
    setter({ type: '', message: '' })
  }

  const checkApiHealth = async (signal) => {
    setHealthChecking(true)
    try {
      const response = await fetch(`${apiBaseUrl}/health`, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' },
        signal: signal
      })

      if (response.ok) {
        const data = await response.json()
        if (data.status === 'ok') {
          setApiHealth(true)
          return true
        }
      }
      setApiHealth(false)
      return false
    } catch (err) {
      // Don't update state if the request was aborted
      if (err.name === 'AbortError') {
        return false
      }
      setApiHealth(false)
      return false
    } finally {
      setHealthChecking(false)
    }
  }

  // Check health when API URL changes (with debounce and cleanup)
  useEffect(() => {
    if (!apiBaseUrl) {
      return
    }

    // Reset health status when URL changes
    setApiHealth(null)
    setHealthChecking(true)

    // Create AbortController to cancel previous requests
    const abortController = new AbortController()

    // Debounce the health check by 500ms
    const timeoutId = setTimeout(() => {
      checkApiHealth(abortController.signal)
    }, 500)

    // Cleanup: cancel the request if URL changes again or component unmounts
    return () => {
      clearTimeout(timeoutId)
      abortController.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiBaseUrl])


  const createArtifact = async () => {
    clearFeedback(setCreateArtifactFeedback)
    setLoading(true)
    
    try {
      const response = await fetch(`${apiBaseUrl}/artifacts`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: artifactTitle,
          description: artifactDescription || undefined
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setCreateArtifactFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setCreateArtifactFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setArtifactId(data.id)
      setCreateArtifactFeedback({ type: 'success', message: `Artifact created! ID: ${data.id}` })
      setArtifactTitle('')
      setArtifactDescription('')
    } catch (err) {
      setCreateArtifactFeedback({ type: 'error', message: `Failed to create artifact: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const uploadMaterial = async () => {
    if (!artifactId) {
      setUploadMaterialFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }
    if (!materialFile) {
      setUploadMaterialFeedback({ type: 'error', message: 'Please select a file' })
      return
    }

    clearFeedback(setUploadMaterialFeedback)
    setLoading(true)

    try {
      const formData = new FormData()
      formData.append('file', materialFile)
      formData.append('kind', materialKind)

      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/materials`, {
        method: 'POST',
        body: formData
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setUploadMaterialFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setUploadMaterialFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setUploadMaterialFeedback({ type: 'success', message: `Material uploaded! Filename: ${data.filename}, Text Status: ${data.text_status}` })
      setMaterialFile(null)
    } catch (err) {
      setUploadMaterialFeedback({ type: 'error', message: `Failed to upload material: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const attachVideo = async () => {
    if (!artifactId) {
      setAttachVideoFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }
    if (!videoUrl) {
      setAttachVideoFeedback({ type: 'error', message: 'Please enter a video URL' })
      return
    }

    clearFeedback(setAttachVideoFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/video`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: videoProvider,
          video_url: videoUrl
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAttachVideoFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setVideoId(data.id)
      setAttachVideoFeedback({ type: 'success', message: `Video attached! Video ID: ${data.id}` })
      setVideoUrl('')
    } catch (err) {
      setAttachVideoFeedback({ type: 'error', message: `Failed to attach video: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const submitTranscript = async () => {
    if (!artifactId || !videoId) {
      setSubmitTranscriptFeedback({ type: 'error', message: 'Please attach a video first' })
      return
    }
    if (!transcriptText) {
      setSubmitTranscriptFeedback({ type: 'error', message: 'Please enter transcript text' })
      return
    }

    clearFeedback(setSubmitTranscriptFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/video/${videoId}/transcript`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          transcript_text: transcriptText
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setSubmitTranscriptFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setSubmitTranscriptFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setSubmitTranscriptFeedback({ type: 'success', message: `Transcript submitted! Status: ${data.transcript_status}` })
      setTranscriptText('')
    } catch (err) {
      setSubmitTranscriptFeedback({ type: 'error', message: `Failed to submit transcript: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const askQuestion = async () => {
    if (!artifactId) {
      setAskQuestionFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }
    if (!questionText) {
      setAskQuestionFeedback({ type: 'error', message: 'Please enter a question' })
      return
    }

    clearFeedback(setAskQuestionFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/questions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          question_text: questionText
        })
      })

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setAskQuestionFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setAskQuestionFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      setCurrentAnswer(data)
      // Indicate if response was cached (200) or new (201)
      const isCached = response.status === 200
      const statusMsg = isCached 
        ? `Question answered (cached)! Status: ${data.answer.answer_status}`
        : `Question answered! Status: ${data.answer.answer_status}`
      setAskQuestionFeedback({ type: 'success', message: statusMsg })
      setQuestionText('')
      // Refresh questions list
      fetchQuestions()
    } catch (err) {
      setAskQuestionFeedback({ type: 'error', message: `Failed to ask question: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  const fetchQuestions = async () => {
    if (!artifactId) {
      setQuestionHistoryFeedback({ type: 'error', message: 'Please create an artifact first' })
      return
    }

    clearFeedback(setQuestionHistoryFeedback)
    setLoading(true)

    try {
      const response = await fetch(`${apiBaseUrl}/artifacts/${artifactId}/questions`)

      if (!response.ok) {
        const text = await response.text()
        try {
          const json = JSON.parse(text)
          setQuestionHistoryFeedback({ type: 'error', message: `Error ${response.status}: ${JSON.stringify(json, null, 2)}` })
        } catch {
          setQuestionHistoryFeedback({ type: 'error', message: `Error ${response.status}: ${text}` })
        }
        return
      }

      const data = await response.json()
      // The API returns separate arrays, but answers are already matched by index
      // Create a map of question ID to answer for easy lookup
      const answerMap = new Map()
      if (data.answers && Array.isArray(data.answers)) {
        data.answers.forEach(answer => {
          if (answer && answer.question_id) {
            answerMap.set(answer.question_id, answer)
          }
        })
      }
      // Combine questions with their answers
      const questionsWithAnswers = (data.questions || []).map(q => {
        const answer = answerMap.get(q.id) || null
        return {
          ...q,
          answer: answer
        }
      })
      setQuestions(questionsWithAnswers)
      setQuestionHistoryFeedback({ type: 'info', message: `Loaded ${questionsWithAnswers.length} questions` })
    } catch (err) {
      setQuestionHistoryFeedback({ type: 'error', message: `Failed to fetch questions: ${err.message}` })
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="container">
      <h1>TalkBack Phase 2 - Web UI</h1>
      
      <div className="section">
        <div className="form-group">
          <label>API Base URL:</label>
          <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
            <input
              type="text"
              value={apiBaseUrl}
              onChange={(e) => setApiBaseUrl(e.target.value)}
              placeholder="http://localhost:8080"
              style={{ flex: 1 }}
            />
            <button 
              onClick={() => checkApiHealth(new AbortController().signal)} 
              disabled={healthChecking}
              style={{ marginTop: 0 }}
            >
              {healthChecking ? 'Checking...' : 'Check Health'}
            </button>
          </div>
        </div>
        <div style={{ marginTop: '10px' }}>
          {apiHealth === null && !healthChecking && (
            <div className="info">API status: Unknown - Click "Check Health" to verify</div>
          )}
          {apiHealth === true && (
            <div className="success" style={{ marginTop: 0 }}>
              ✓ API is healthy and reachable
            </div>
          )}
          {apiHealth === false && (
            <div className="error" style={{ marginTop: 0 }}>
              ✗ API is not reachable - Check if the server is running on {apiBaseUrl}
            </div>
          )}
        </div>
        {artifactId && (
          <div className="info" style={{ marginTop: '10px' }}>
            Current Artifact ID: <span className="artifact-id">{artifactId}</span>
          </div>
        )}
        {videoId && (
          <div className="info" style={{ marginTop: '10px' }}>
            Current Video ID: <span className="artifact-id">{videoId}</span>
          </div>
        )}
      </div>

      <h2>1. Create Artifact</h2>
      <div className="section">
        <div className="form-group">
          <label>Title:</label>
          <input
            type="text"
            value={artifactTitle}
            onChange={(e) => setArtifactTitle(e.target.value)}
            placeholder="My Artifact Title"
          />
        </div>
        <div className="form-group">
          <label>Description (optional):</label>
          <textarea
            value={artifactDescription}
            onChange={(e) => setArtifactDescription(e.target.value)}
            placeholder="Optional description"
          />
        </div>
        <button onClick={createArtifact} disabled={!artifactTitle || loading}>
          Create Artifact
        </button>
        {createArtifactFeedback.message && (
          <div className={createArtifactFeedback.type} style={{ marginTop: '10px' }}>
            {createArtifactFeedback.message}
          </div>
        )}
        {loading && <div className="loading" style={{ marginTop: '10px' }}>Loading...</div>}
      </div>

      <h2>2. Upload Material</h2>
      <div className="section">
        <div className="form-group">
          <label>File:</label>
          <input
            type="file"
            onChange={(e) => setMaterialFile(e.target.files[0])}
          />
        </div>
        <div className="form-group">
          <label>Kind:</label>
          <select value={materialKind} onChange={(e) => setMaterialKind(e.target.value)}>
            <option value="document">Document</option>
            <option value="slides">Slides</option>
            <option value="diagram">Diagram</option>
            <option value="other">Other</option>
          </select>
        </div>
        <button onClick={uploadMaterial} disabled={!artifactId || !materialFile || loading}>
          Upload Material
        </button>
        {uploadMaterialFeedback.message && (
          <div className={uploadMaterialFeedback.type} style={{ marginTop: '10px' }}>
            {uploadMaterialFeedback.message}
          </div>
        )}
      </div>

      <h2>3. Attach Video URL</h2>
      <div className="section">
        <div className="form-group">
          <label>Provider:</label>
          <select value={videoProvider} onChange={(e) => setVideoProvider(e.target.value)}>
            <option value="loom">Loom</option>
            <option value="zoom">Zoom</option>
            <option value="other">Other</option>
          </select>
        </div>
        <div className="form-group">
          <label>Video URL:</label>
          <input
            type="url"
            value={videoUrl}
            onChange={(e) => setVideoUrl(e.target.value)}
            placeholder="https://www.loom.com/share/..."
          />
        </div>
        <button onClick={attachVideo} disabled={!artifactId || !videoUrl || loading}>
          Attach Video
        </button>
        {attachVideoFeedback.message && (
          <div className={attachVideoFeedback.type} style={{ marginTop: '10px' }}>
            {attachVideoFeedback.message}
          </div>
        )}
      </div>

      <h2>4. Submit Transcript</h2>
      <div className="section">
        <div className="form-group">
          <label>Transcript Text:</label>
          <textarea
            value={transcriptText}
            onChange={(e) => setTranscriptText(e.target.value)}
            placeholder="Paste transcript text here..."
          />
        </div>
        <button onClick={submitTranscript} disabled={!artifactId || !videoId || !transcriptText || loading}>
          Submit Transcript
        </button>
        {submitTranscriptFeedback.message && (
          <div className={submitTranscriptFeedback.type} style={{ marginTop: '10px' }}>
            {submitTranscriptFeedback.message}
          </div>
        )}
      </div>

      <h2>5. Ask Question</h2>
      <div className="section">
        <div className="form-group">
          <label>Question:</label>
          <textarea
            value={questionText}
            onChange={(e) => setQuestionText(e.target.value)}
            placeholder="What is the main topic discussed?"
          />
        </div>
        <button onClick={askQuestion} disabled={!artifactId || !questionText || loading}>
          Ask Question
        </button>
        {askQuestionFeedback.message && (
          <div className={askQuestionFeedback.type} style={{ marginTop: '10px' }}>
            {askQuestionFeedback.message}
          </div>
        )}
      </div>

      {currentAnswer && (
        <div className="section">
          <h3>Latest Answer</h3>
          <div className="question-item">
            <div className="question-text">Q: {currentAnswer.question.question_text}</div>
            <div>
              <span className={`answer-status ${currentAnswer.answer.answer_status}`}>
                {currentAnswer.answer.answer_status}
              </span>
              <span className="confidence">Confidence: {(currentAnswer.answer.confidence * 100).toFixed(1)}%</span>
            </div>
            <div className="answer-text">{currentAnswer.answer.answer_text}</div>
            {currentAnswer.answer.citations && currentAnswer.answer.citations.length > 0 && (
              <div className="citations">
                <strong>Citations:</strong>
                {currentAnswer.answer.citations.map((citation, idx) => (
                  <div key={idx} className="citation">
                    <div className="citation-source">
                      {citation.source_type} - {citation.source_id}
                      {citation.chunk_id && ` [chunk: ${citation.chunk_id}]`}
                      {citation.locator && ` (${citation.locator})`}
                    </div>
                    <div className="citation-snippet">{citation.snippet}</div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      <h2>6. Question History</h2>
      <div className="section">
        <button onClick={fetchQuestions} disabled={!artifactId || loading}>
          Load Questions
        </button>
        {questionHistoryFeedback.message && (
          <div className={questionHistoryFeedback.type} style={{ marginTop: '10px' }}>
            {questionHistoryFeedback.message}
          </div>
        )}
        {questions.length > 0 && (
          <div style={{ marginTop: '20px' }}>
            {questions.map((q) => (
              <div key={q.id} className="question-item">
                <div className="question-text">Q: {q.question_text}</div>
                {q.answer ? (
                  <>
                    <div>
                      <span className={`answer-status ${q.answer.answer_status}`}>
                        {q.answer.answer_status}
                      </span>
                      <span className="confidence">Confidence: {(q.answer.confidence * 100).toFixed(1)}%</span>
                    </div>
                    <div className="answer-text">{q.answer.answer_text}</div>
                    {q.answer.citations && q.answer.citations.length > 0 && (
                      <div className="citations">
                        <strong>Citations:</strong>
                        {q.answer.citations.map((citation, cidx) => (
                          <div key={cidx} className="citation">
                            <div className="citation-source">
                              {citation.source_type} - {citation.source_id}
                              {citation.locator && ` (${citation.locator})`}
                            </div>
                            <div className="citation-snippet">{citation.snippet}</div>
                          </div>
                        ))}
                      </div>
                    )}
                  </>
                ) : (
                  <div className="loading">No answer yet</div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      <h2>⚠ Reset All Data (Dev Only)</h2>
      <div className="section" style={{ border: '2px solid #fcc', backgroundColor: '#fff5f5' }}>
        <div style={{ marginBottom: '15px', color: '#c33', fontWeight: 600 }}>
          ⚠️ WARNING: This will delete ALL artifacts, materials, videos, questions, and answers!
        </div>
        
        {!showResetConfirm ? (
          <>
            <div className="form-group">
              <label>This action cannot be undone. Click below to confirm:</label>
            </div>
            <button 
              onClick={() => setShowResetConfirm(true)}
              style={{ backgroundColor: '#dc3545', marginTop: 0 }}
            >
              Show Reset Confirmation
            </button>
          </>
        ) : (
          <>
            <div className="form-group">
              <label>
                Type <strong>RESET</strong> to confirm deletion:
              </label>
              <input
                type="text"
                value={resetConfirmText}
                onChange={(e) => setResetConfirmText(e.target.value)}
                placeholder="Type RESET to confirm"
                style={{ border: '2px solid #dc3545' }}
              />
            </div>
            <div style={{ display: 'flex', gap: '10px' }}>
              <button
                onClick={async () => {
                  if (resetConfirmText !== 'RESET') {
                    setResetFeedback({ type: 'error', message: 'Please type RESET exactly to confirm' })
                    return
                  }

                  clearFeedback(setResetFeedback)
                  setLoading(true)

                  try {
                    const response = await fetch(`${apiBaseUrl}/admin/reset`, {
                      method: 'POST',
                      headers: { 'Content-Type': 'application/json' }
                    })

                    const data = await response.json()

                    if (!response.ok) {
                      setResetFeedback({ type: 'error', message: `Reset failed: ${JSON.stringify(data, null, 2)}` })
                      return
                    }

                    setResetFeedback({ type: 'success', message: `Reset successful! ${JSON.stringify(data, null, 2)}` })
                    setArtifactId('')
                    setVideoId('')
                    setQuestions([])
                    setCurrentAnswer(null)
                    setResetConfirmText('')
                    setShowResetConfirm(false)
                    // Clear all other feedback
                    clearFeedback(setCreateArtifactFeedback)
                    clearFeedback(setUploadMaterialFeedback)
                    clearFeedback(setAttachVideoFeedback)
                    clearFeedback(setSubmitTranscriptFeedback)
                    clearFeedback(setAskQuestionFeedback)
                    clearFeedback(setQuestionHistoryFeedback)
                  } catch (err) {
                    setResetFeedback({ type: 'error', message: `Failed to reset: ${err.message}` })
                  } finally {
                    setLoading(false)
                  }
                }}
                disabled={resetConfirmText !== 'RESET' || loading}
                style={{ 
                  backgroundColor: '#dc3545',
                  marginTop: 0
                }}
              >
                {loading ? 'Resetting...' : '⚠ Reset All Data'}
              </button>
              <button
                onClick={() => {
                  setShowResetConfirm(false)
                  setResetConfirmText('')
                  clearFeedback(setResetFeedback)
                }}
                disabled={loading}
                style={{ 
                  backgroundColor: '#6c757d',
                  marginTop: 0
                }}
              >
                Cancel
              </button>
            </div>
            {resetFeedback.message && (
              <div className={resetFeedback.type} style={{ marginTop: '10px' }}>
                {resetFeedback.message}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

export default App
