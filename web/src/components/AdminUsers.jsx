import { useState, useEffect, useCallback } from 'react'

export function AdminUsers({
  apiBaseUrl,
  debugMode = false,
  usersExpanded: usersExpandedProp = false,
  onUsersExpandedChange,
  sessionsExpanded: sessionsExpandedProp = false,
  onSessionsExpandedChange
}) {
  const [usersExpandedLocal, setUsersExpandedLocal] = useState(false)
  const [sessionsExpandedLocal, setSessionsExpandedLocal] = useState(false)
  const isControlledUsers = onUsersExpandedChange != null
  const isControlledSessions = onSessionsExpandedChange != null
  const usersExpanded = isControlledUsers ? usersExpandedProp : usersExpandedLocal
  const sessionsExpanded = isControlledSessions ? sessionsExpandedProp : sessionsExpandedLocal
  const setUsersExpanded = isControlledUsers ? onUsersExpandedChange : setUsersExpandedLocal
  const setSessionsExpanded = isControlledSessions ? onSessionsExpandedChange : setSessionsExpandedLocal
  const [users, setUsers] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [addEmail, setAddEmail] = useState('')
  const [addDisplayName, setAddDisplayName] = useState('')
  const [addPassword, setAddPassword] = useState('')
  const [addSubmitting, setAddSubmitting] = useState(false)
  const [addError, setAddError] = useState('')
  const [addRole, setAddRole] = useState('creator')
  const [removeId, setRemoveId] = useState(null)
  const [removingUserId, setRemovingUserId] = useState(null)
  const [removeUserError, setRemoveUserError] = useState('')
  const [roleUpdatingId, setRoleUpdatingId] = useState(null)
  const [showResetConfirm, setShowResetConfirm] = useState(false)
  const [resetConfirmText, setResetConfirmText] = useState('')
  const [resetFeedback, setResetFeedback] = useState({ type: '', message: '' })
  const [resetLoading, setResetLoading] = useState(false)

  const [sessions, setSessions] = useState([])
  const [sessionsLoading, setSessionsLoading] = useState(false)
  const [sessionsError, setSessionsError] = useState('')
  const [deleteConfirmSessionId, setDeleteConfirmSessionId] = useState(null)
  const [deletingSessionId, setDeletingSessionId] = useState(null)
  const [sessionDeleteError, setSessionDeleteError] = useState('')
  const [renameSessionId, setRenameSessionId] = useState(null)
  const [renameSessionTitle, setRenameSessionTitle] = useState('')
  const [renameSaving, setRenameSaving] = useState(false)
  const [renameError, setRenameError] = useState('')

  const fetchUsers = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/admin/users`, { credentials: 'include' })
      if (!res.ok) {
        if (res.status === 403) {
          setError('Forbidden: admin access required')
          return
        }
        setError(`Failed to load users: ${res.status}`)
        return
      }
      const data = await res.json()
      setUsers(Array.isArray(data) ? data : (data.users || []))
    } catch (e) {
      setError(e.message || 'Network error')
    } finally {
      setLoading(false)
    }
  }, [apiBaseUrl])

  const fetchSessions = useCallback(async () => {
    setSessionsLoading(true)
    setSessionsError('')
    try {
      const res = await fetch(`${apiBaseUrl.replace(/\/$/, '')}/api/sessions`, { credentials: 'include' })
      if (!res.ok) {
        if (res.status === 403) setSessionsError('Forbidden: admin access required')
        else setSessionsError(`Failed to load sessions: ${res.status}`)
        return
      }
      const data = await res.json()
      setSessions(Array.isArray(data) ? data : [])
    } catch (e) {
      setSessionsError(e.message || 'Network error')
    } finally {
      setSessionsLoading(false)
    }
  }, [apiBaseUrl])

  useEffect(() => {
    fetchUsers()
  }, [fetchUsers])

  useEffect(() => {
    // Empty-string apiBaseUrl is valid (relative /api); only null/undefined should block fetch.
    if (sessionsExpanded && apiBaseUrl != null) fetchSessions()
  }, [sessionsExpanded, apiBaseUrl, fetchSessions])

  const handleAddUser = async (e) => {
    e.preventDefault()
    setAddSubmitting(true)
    setAddError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/admin/users`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          email: addEmail.trim().toLowerCase(),
          display_name: addDisplayName.trim() || addEmail.trim(),
          password: addPassword,
          global_role: addRole
        })
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setAddError(data.error || data.message || `Failed: ${res.status}`)
        return
      }
      setAddEmail('')
      setAddDisplayName('')
      setAddPassword('')
      setAddRole('creator')
      fetchUsers()
    } catch (e) {
      setAddError(e.message || 'Network error')
    } finally {
      setAddSubmitting(false)
    }
  }

  const ROLE_OPTIONS = [
    { value: 'admin', label: 'Admin' },
    { value: 'creator', label: 'Can create sessions' },
    { value: 'participant', label: 'Join existing sessions only' }
  ]
  const roleLabel = (role) => ROLE_OPTIONS.find((o) => o.value === role)?.label || role || '—'

  const handleRoleChange = async (userId, newRole) => {
    if (newRole === '') return
    setRoleUpdatingId(userId)
    setAddError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/admin/users/${userId}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ global_role: newRole })
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setAddError(data.error || `Failed to update role: ${res.status}`)
        return
      }
      fetchUsers()
    } catch (e) {
      setAddError(e.message || 'Network error')
    } finally {
      setRoleUpdatingId(null)
    }
  }

  const handleRemoveUser = async (id) => {
    if (!id) return
    setRemoveUserError('')
    setRemovingUserId(id)
    try {
      const res = await fetch(`${apiBaseUrl}/api/admin/users/${id}`, {
        method: 'DELETE',
        credentials: 'include'
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRemoveUserError(data.error || `Failed to remove user: ${res.status}`)
        return
      }
      setRemoveId(null)
      fetchUsers()
    } catch (e) {
      setRemoveUserError(e.message || 'Network error')
    } finally {
      setRemovingUserId(null)
    }
  }

  const handleDeleteSessionConfirm = async (sessionId) => {
    if (!sessionId) return
    setSessionDeleteError('')
    setDeletingSessionId(sessionId)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const res = await fetch(`${base}/api/sessions/${sessionId}`, {
        method: 'DELETE',
        credentials: 'include'
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setSessionDeleteError(data.error || `Could not delete session: ${res.status}`)
        return
      }
      setSessions(prev => prev.filter(item => (item.session || item).id !== sessionId))
      setDeleteConfirmSessionId(null)
    } catch (e) {
      setSessionDeleteError(e.message || 'Network error')
    } finally {
      setDeletingSessionId(null)
    }
  }

  const handleOpenRenameSession = (session) => {
    if (!session) return
    setRenameSessionId(session.id)
    setRenameSessionTitle(session.title || '')
    setRenameError('')
  }

  const handleSaveRenameSession = async () => {
    const title = (renameSessionTitle || '').trim()
    if (!renameSessionId || !title) {
      setRenameError('Title cannot be empty')
      return
    }
    setRenameSaving(true)
    setRenameError('')
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const res = await fetch(`${base}/api/sessions/${renameSessionId}`, {
        method: 'PATCH',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        const msg = data.error || data.message || (res.status === 409
          ? 'A session with this name already exists. Please use a unique name.'
          : `Failed to rename: ${res.status}`)
        setRenameError(msg)
        return
      }
      setSessions(prev => prev.map(item => {
        const s = item.session || item
        if (s.id !== renameSessionId) return item
        const updated = { ...s, title }
        return item.session ? { ...item, session: updated } : updated
      }))
      setRenameSessionId(null)
      setRenameSessionTitle('')
      setRenameError('')
    } catch (e) {
      setRenameError(e.message || 'Failed to rename session')
    } finally {
      setRenameSaving(false)
    }
  }

  const handleResetAllData = async () => {
    if (resetConfirmText !== 'RESET') {
      setResetFeedback({ type: 'error', message: 'Please type RESET exactly to confirm' })
      return
    }
    setResetFeedback({ type: '', message: '' })
    setResetLoading(true)
    try {
      const response = await fetch(`${apiBaseUrl}/admin/reset`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include'
      })
      const data = await response.json().catch(() => ({}))
      if (!response.ok) {
        setResetFeedback({ type: 'error', message: `Reset failed: ${JSON.stringify(data)}` })
        return
      }
      setResetFeedback({ type: 'success', message: `Reset successful. ${JSON.stringify(data)}` })
      setResetConfirmText('')
      setShowResetConfirm(false)
      fetchUsers()
    } catch (err) {
      setResetFeedback({ type: 'error', message: `Failed to reset: ${err.message}` })
    } finally {
      setResetLoading(false)
    }
  }

  return (
    <div className="section" style={{ width: '100%' }}>
      <h2>Admin</h2>

      <div style={{ maxHeight: 'calc(100vh - 180px)', overflowY: 'auto', paddingRight: '8px' }}>
      {/* Expandable: Users */}
      <div style={{ marginBottom: '20px', border: '1px solid #bbb', borderRadius: '8px', overflow: 'hidden', boxShadow: '0 1px 3px rgba(0,0,0,0.08)' }}>
        <button
          type="button"
          onClick={() => setUsersExpanded(!usersExpanded)}
          style={{ width: '100%', padding: '16px 20px', textAlign: 'left', fontSize: '18px', fontWeight: 700, border: 'none', background: '#e8e8e8', color: '#1a1a1a', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '10px' }}
        >
          <span style={{ transform: usersExpanded ? 'rotate(90deg)' : 'none', display: 'inline-block', fontSize: '14px' }}>▶</span>
          Users
        </button>
        {usersExpanded && (
          <div style={{ padding: '16px', borderTop: '1px solid #ddd' }}>
      {loading && <p>Loading users…</p>}
      {error && <p className="error" style={{ marginBottom: '12px' }}>{error}</p>}
      {addError && <p className="error" style={{ marginBottom: '12px' }}>{addError}</p>}

      <form onSubmit={handleAddUser} style={{ marginBottom: '24px', padding: '16px', border: '1px solid #ddd', borderRadius: '8px', backgroundColor: '#f9f9f9' }}>
        <h3 style={{ marginTop: 0 }}>Add user</h3>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px', alignItems: 'flex-end' }}>
          <div className="form-group" style={{ marginBottom: 0, minWidth: '200px' }}>
            <label>Email</label>
            <input
              type="email"
              value={addEmail}
              onChange={(e) => setAddEmail(e.target.value)}
              required
              placeholder="user@example.com"
            />
          </div>
          <div className="form-group" style={{ marginBottom: 0, minWidth: '160px' }}>
            <label>Display name</label>
            <input
              type="text"
              value={addDisplayName}
              onChange={(e) => setAddDisplayName(e.target.value)}
              placeholder="Optional"
            />
          </div>
          <div className="form-group" style={{ marginBottom: 0, minWidth: '140px' }}>
            <label>Password</label>
            <input
              type="password"
              value={addPassword}
              onChange={(e) => setAddPassword(e.target.value)}
              required
              placeholder="Required"
            />
          </div>
          <div className="form-group" style={{ marginBottom: 0, minWidth: '200px' }}>
            <label>Role</label>
            <select
              value={addRole}
              onChange={(e) => setAddRole(e.target.value)}
              style={{ display: 'block', padding: '6px 8px', width: '100%' }}
            >
              {ROLE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
          <button type="submit" disabled={addSubmitting} style={{ marginTop: 0 }}>
            {addSubmitting ? 'Adding…' : 'Add user'}
          </button>
        </div>
      </form>

      {/* User list */}
      <h3>Users</h3>
      {!loading && users.length === 0 && <p className="info">No users yet.</p>}
      {!loading && users.length > 0 && (() => {
        const adminCount = users.filter((u) => u.global_role === 'admin').length
        return (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '14px' }}>
          <thead>
            <tr style={{ borderBottom: '2px solid #ddd', textAlign: 'left' }}>
              <th style={{ padding: '8px 12px' }}>Email</th>
              <th style={{ padding: '8px 12px' }}>Display name</th>
              <th style={{ padding: '8px 12px' }}>Status</th>
              <th style={{ padding: '8px 12px' }}>Role</th>
              <th style={{ padding: '8px 12px' }}>Sessions</th>
              <th style={{ padding: '8px 12px' }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => {
              const isLastAdmin = u.global_role === 'admin' && adminCount <= 1
              return (
              <tr key={u.id} style={{ borderBottom: '1px solid #eee' }}>
                <td style={{ padding: '8px 12px' }}>{u.email}</td>
                <td style={{ padding: '8px 12px' }}>{u.display_name || '—'}</td>
                <td style={{ padding: '8px 12px' }}>{u.status || '—'}</td>
                <td style={{ padding: '8px 12px' }}>
                  <span title={u.global_role}>{roleLabel(u.global_role)}</span>
                  <select
                    value={u.global_role}
                    onChange={(e) => handleRoleChange(u.id, e.target.value)}
                    disabled={isLastAdmin || roleUpdatingId === u.id}
                    title={isLastAdmin ? 'Cannot demote the last admin' : 'Change role'}
                    style={{ marginLeft: '8px', fontSize: '12px', padding: '2px 6px', maxWidth: '160px' }}
                  >
                    {ROLE_OPTIONS.map((o) => (
                      <option key={o.value} value={o.value}>{o.label}</option>
                    ))}
                  </select>
                </td>
                <td style={{ padding: '8px 12px' }}>{(u.session_ids && u.session_ids.length) || 0}</td>
                <td style={{ padding: '8px 12px' }}>
                  <button
                    type="button"
                    onClick={() => !isLastAdmin && setRemoveId(u.id)}
                    disabled={isLastAdmin || removingUserId === u.id}
                    title={isLastAdmin ? 'Cannot remove the last admin' : ''}
                    style={{ fontSize: '12px', padding: '4px 10px', opacity: isLastAdmin ? 0.6 : 1 }}
                  >
                    Remove
                  </button>
                </td>
              </tr>
              )
            })}
          </tbody>
        </table>
        )
      })()}
          </div>
        )}
      </div>

      {/* Expandable: Sessions */}
      <div style={{ marginBottom: '20px', border: '1px solid #bbb', borderRadius: '8px', overflow: 'hidden', boxShadow: '0 1px 3px rgba(0,0,0,0.08)' }}>
        <button
          type="button"
          onClick={() => setSessionsExpanded(!sessionsExpanded)}
          style={{ width: '100%', padding: '16px 20px', textAlign: 'left', fontSize: '18px', fontWeight: 700, border: 'none', background: '#e8e8e8', color: '#1a1a1a', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '10px' }}
        >
          <span style={{ transform: sessionsExpanded ? 'rotate(90deg)' : 'none', display: 'inline-block', fontSize: '14px' }}>▶</span>
          Sessions
        </button>
        {sessionsExpanded && (
          <div style={{ padding: '16px', borderTop: '1px solid #ddd' }}>
            {deletingSessionId && (
              <div style={{ marginBottom: '12px', padding: '12px', background: '#e3f2fd', borderRadius: '6px' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '8px' }}>
                  <span className="spinner" style={{ flexShrink: 0 }} />
                  <span style={{ fontSize: '14px' }}>Deleting session…</span>
                </div>
                <div style={{ width: '100%', height: '8px', background: '#bbdefb', borderRadius: '4px', overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: '30%', background: '#1976d2', borderRadius: '4px', animation: 'progress-indeterminate 1.2s ease-in-out infinite' }} />
                </div>
              </div>
            )}
            {sessionDeleteError && <p className="error" style={{ marginBottom: '12px' }}>{sessionDeleteError}</p>}
            {sessionsLoading && <p>Loading sessions…</p>}
            {!sessionsLoading && sessionsError && <p className="error">{sessionsError}</p>}
            {!sessionsLoading && !sessionsError && sessions.length === 0 && <p className="info">No sessions.</p>}
            {!sessionsLoading && !sessionsError && sessions.length > 0 && (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '14px' }}>
                <thead>
                  <tr style={{ borderBottom: '2px solid #ddd', textAlign: 'left' }}>
                    <th style={{ padding: '8px 12px' }}>Title</th>
                    <th style={{ padding: '8px 12px' }}>ID</th>
                    <th style={{ padding: '8px 12px' }}>Created by</th>
                    <th style={{ padding: '8px 12px' }}>Status</th>
                    <th style={{ padding: '8px 12px' }}>Updated</th>
                    <th style={{ padding: '8px 12px' }}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {sessions.map((item) => {
                    const session = item.session || item
                    const isDeleting = deletingSessionId === session.id
                    return (
                      <tr key={session.id} style={{ borderBottom: '1px solid #eee' }}>
                        <td style={{ padding: '8px 12px' }}>{session.title || '—'}</td>
                        <td style={{ padding: '8px 12px', fontFamily: 'monospace', fontSize: '12px' }}>{session.id}</td>
                        <td style={{ padding: '8px 12px' }}>{session.created_by || '—'}</td>
                        <td style={{ padding: '8px 12px' }}>{session.status || '—'}</td>
                        <td style={{ padding: '8px 12px' }}>{session.updated_at ? new Date(session.updated_at).toLocaleString() : '—'}</td>
                        <td style={{ padding: '8px 12px' }}>
                          <button
                            type="button"
                            disabled={isDeleting}
                            onClick={() => handleOpenRenameSession(session)}
                            style={{ fontSize: '12px', padding: '4px 10px', marginRight: '8px', backgroundColor: '#1976d2', color: '#fff', border: 'none', borderRadius: '4px', cursor: isDeleting ? 'not-allowed' : 'pointer', opacity: isDeleting ? 0.7 : 1 }}
                          >
                            Rename
                          </button>
                          <button
                            type="button"
                            disabled={isDeleting}
                            onClick={() => setDeleteConfirmSessionId(session.id)}
                            style={{ fontSize: '12px', padding: '4px 10px', backgroundColor: '#d32f2f', color: '#fff', border: 'none', borderRadius: '4px', cursor: isDeleting ? 'not-allowed' : 'pointer', opacity: isDeleting ? 0.7 : 1 }}
                          >
                            Delete
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>

      </div>

      {/* Delete session confirmation modal */}
      {deleteConfirmSessionId && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="delete-session-modal-title"
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 1000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'rgba(0,0,0,0.5)'
          }}
          onClick={() => { if (deletingSessionId !== deleteConfirmSessionId) { setDeleteConfirmSessionId(null); setSessionDeleteError('') } }}
        >
          <div
            style={{
              background: '#fff',
              padding: '24px',
              borderRadius: '8px',
              boxShadow: '0 4px 20px rgba(0,0,0,0.2)',
              maxWidth: '400px',
              width: '90%'
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="delete-session-modal-title" style={{ marginTop: 0, marginBottom: '16px', fontSize: '18px' }}>
              Delete this session?
            </h3>
            {deletingSessionId === deleteConfirmSessionId ? (
              <>
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '12px' }}>
                  <span className="spinner" style={{ flexShrink: 0 }} />
                  <span style={{ fontSize: '14px', color: '#555' }}>Deleting session…</span>
                </div>
                <div style={{ width: '100%', height: '8px', background: '#e0e0e0', borderRadius: '4px', overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: '30%', background: '#1976d2', borderRadius: '4px', animation: 'progress-indeterminate 1.2s ease-in-out infinite' }} />
                </div>
              </>
            ) : (
              <>
                <p style={{ marginBottom: '20px', color: '#555' }}>This cannot be undone. Continue or cancel?</p>
                {sessionDeleteError && <p className="error" style={{ marginBottom: '16px', fontSize: '14px' }}>{sessionDeleteError}</p>}
                <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); setDeleteConfirmSessionId(null); setSessionDeleteError('') }}
                    style={{ padding: '8px 16px', border: '1px solid #666', borderRadius: '4px', cursor: 'pointer', background: '#f0f0f0', color: '#333', fontWeight: 500 }}
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); handleDeleteSessionConfirm(deleteConfirmSessionId) }}
                    style={{ padding: '8px 16px', backgroundColor: '#d32f2f', color: '#fff', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                  >
                    Continue
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {/* Rename session modal */}
      {renameSessionId && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="rename-session-modal-title"
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 1000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'rgba(0,0,0,0.5)'
          }}
          onClick={() => { if (!renameSaving) { setRenameSessionId(null); setRenameSessionTitle(''); setRenameError('') } }}
        >
          <div
            style={{ background: '#fff', padding: '24px', borderRadius: '8px', boxShadow: '0 4px 20px rgba(0,0,0,0.2)', maxWidth: '420px', width: '90%' }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="rename-session-modal-title" style={{ marginTop: 0, marginBottom: '12px' }}>Rename session</h3>
            <label style={{ display: 'block', marginBottom: '6px', fontSize: '14px' }}>New title</label>
            <input
              type="text"
              value={renameSessionTitle}
              onChange={(e) => { setRenameSessionTitle(e.target.value); setRenameError('') }}
              placeholder="Session title"
              style={{ width: '100%', padding: '8px', marginBottom: '10px', boxSizing: 'border-box' }}
            />
            {renameError && (
              <div className="error" style={{ marginBottom: '10px', fontSize: '13px' }}>
                {renameError}
              </div>
            )}
            <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
              <button
                type="button"
                onClick={() => { if (!renameSaving) { setRenameSessionId(null); setRenameSessionTitle(''); setRenameError('') } }}
                disabled={renameSaving}
                style={{ padding: '8px 16px' }}
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleSaveRenameSession}
                disabled={renameSaving}
                style={{ padding: '8px 16px', cursor: renameSaving ? 'not-allowed' : 'pointer' }}
              >
                {renameSaving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Remove user confirmation modal */}
      {removeId && (
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="remove-user-modal-title"
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 1000,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'rgba(0,0,0,0.5)'
          }}
          onClick={() => { if (removingUserId !== removeId) { setRemoveId(null); setRemoveUserError('') } }}
        >
          <div
            style={{
              background: '#fff',
              padding: '24px',
              borderRadius: '8px',
              boxShadow: '0 4px 20px rgba(0,0,0,0.2)',
              maxWidth: '400px',
              width: '90%'
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 id="remove-user-modal-title" style={{ marginTop: 0, marginBottom: '16px', fontSize: '18px' }}>
              Remove this user?
            </h3>
            {removingUserId === removeId ? (
              <>
                <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '12px' }}>
                  <span className="spinner" style={{ flexShrink: 0 }} />
                  <span style={{ fontSize: '14px', color: '#555' }}>Removing user…</span>
                </div>
                <div style={{ width: '100%', height: '8px', background: '#e0e0e0', borderRadius: '4px', overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: '30%', background: '#1976d2', borderRadius: '4px', animation: 'progress-indeterminate 1.2s ease-in-out infinite' }} />
                </div>
              </>
            ) : (
              <>
                <p style={{ marginBottom: '20px', color: '#555' }}>This cannot be undone. Continue or cancel?</p>
                {removeUserError && <p className="error" style={{ marginBottom: '16px', fontSize: '14px' }}>{removeUserError}</p>}
                <div style={{ display: 'flex', gap: '12px', justifyContent: 'flex-end' }}>
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); setRemoveId(null); setRemoveUserError('') }}
                    style={{ padding: '8px 16px', border: '1px solid #666', borderRadius: '4px', cursor: 'pointer', background: '#f0f0f0', color: '#333', fontWeight: 500 }}
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); handleRemoveUser(removeId) }}
                    style={{ padding: '8px 16px', backgroundColor: '#d32f2f', color: '#fff', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                  >
                    Continue
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {/* Reset All Data – admin only, and only when debug mode is on */}
      {debugMode && (
        <div style={{ marginTop: '32px', padding: '16px', border: '2px solid #fcc', borderRadius: '8px', backgroundColor: '#fff5f5' }}>
          <h3 style={{ marginTop: 0, color: '#c33' }}>⚠ Reset All Data</h3>
          <p style={{ color: '#c33', fontWeight: 600, marginBottom: '12px' }}>
            WARNING: This will delete ALL artifacts, materials, videos, questions, answers, and related data. This action cannot be undone.
          </p>
          {!showResetConfirm ? (
            <button
              type="button"
              onClick={() => setShowResetConfirm(true)}
              style={{ backgroundColor: '#dc3545', color: 'white', padding: '8px 16px', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
            >
              Show Reset Confirmation
            </button>
          ) : (
            <div>
              <label style={{ display: 'block', marginBottom: '8px' }}>
                Type <strong>RESET</strong> to confirm:
              </label>
              <input
                type="text"
                value={resetConfirmText}
                onChange={(e) => setResetConfirmText(e.target.value)}
                placeholder="Type RESET to confirm"
                style={{ display: 'block', marginBottom: '12px', padding: '8px', border: '2px solid #dc3545', borderRadius: '4px', width: '220px' }}
              />
              <div style={{ display: 'flex', gap: '10px', alignItems: 'center' }}>
                <button
                  type="button"
                  onClick={handleResetAllData}
                  disabled={resetConfirmText !== 'RESET' || resetLoading}
                  style={{ backgroundColor: '#dc3545', color: 'white', padding: '8px 16px', border: 'none', borderRadius: '4px', cursor: resetConfirmText !== 'RESET' || resetLoading ? 'not-allowed' : 'pointer' }}
                >
                  {resetLoading ? 'Resetting…' : '⚠ Reset All Data'}
                </button>
                <button
                  type="button"
                  onClick={() => { setShowResetConfirm(false); setResetConfirmText(''); setResetFeedback({ type: '', message: '' }) }}
                  disabled={resetLoading}
                  style={{ backgroundColor: '#6c757d', color: 'white', padding: '8px 16px', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
                >
                  Cancel
                </button>
              </div>
              {resetFeedback.message && (
                <p className={resetFeedback.type} style={{ marginTop: '12px' }}>{resetFeedback.message}</p>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
