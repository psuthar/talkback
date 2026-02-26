import { useState, useEffect, useCallback } from 'react'

export function AdminUsers({ apiBaseUrl }) {
  const [users, setUsers] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [addEmail, setAddEmail] = useState('')
  const [addDisplayName, setAddDisplayName] = useState('')
  const [addPassword, setAddPassword] = useState('')
  const [addSubmitting, setAddSubmitting] = useState(false)
  const [addError, setAddError] = useState('')
  const [removeId, setRemoveId] = useState(null)
  const [removeConfirm, setRemoveConfirm] = useState('')
  const [showResetConfirm, setShowResetConfirm] = useState(false)
  const [resetConfirmText, setResetConfirmText] = useState('')
  const [resetFeedback, setResetFeedback] = useState({ type: '', message: '' })
  const [resetLoading, setResetLoading] = useState(false)

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

  useEffect(() => {
    fetchUsers()
  }, [fetchUsers])

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
          password: addPassword
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
      fetchUsers()
    } catch (e) {
      setAddError(e.message || 'Network error')
    } finally {
      setAddSubmitting(false)
    }
  }

  const handleRemoveUser = async (id) => {
    if (removeConfirm !== 'remove') return
    setAddError('')
    try {
      const res = await fetch(`${apiBaseUrl}/api/admin/users/${id}`, {
        method: 'DELETE',
        credentials: 'include'
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setAddError(data.error || `Failed to remove user: ${res.status}`)
        return
      }
      setRemoveId(null)
      setRemoveConfirm('')
      fetchUsers()
    } catch (e) {
      setAddError(e.message || 'Network error')
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
    <div className="section" style={{ maxWidth: '900px' }}>
      <h2>Admin – Users</h2>
      {loading && <p>Loading users…</p>}
      {error && <p className="error" style={{ marginBottom: '12px' }}>{error}</p>}
      {addError && <p className="error" style={{ marginBottom: '12px' }}>{addError}</p>}

      {/* Add user form */}
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
                <td style={{ padding: '8px 12px' }}>{u.global_role || '—'}</td>
                <td style={{ padding: '8px 12px' }}>{(u.session_ids && u.session_ids.length) || 0}</td>
                <td style={{ padding: '8px 12px' }}>
                  {removeId === u.id ? (
                    <span style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                      <input
                        type="text"
                        placeholder="Type 'remove' to confirm"
                        value={removeConfirm}
                        onChange={(e) => setRemoveConfirm(e.target.value)}
                        style={{ width: '160px', fontSize: '12px' }}
                      />
                      <button
                        type="button"
                        onClick={() => handleRemoveUser(u.id)}
                        disabled={removeConfirm !== 'remove' || isLastAdmin}
                        title={isLastAdmin ? 'Cannot remove the last admin' : ''}
                        style={{ fontSize: '12px', padding: '4px 10px' }}
                      >
                        Confirm remove
                      </button>
                      <button type="button" onClick={() => { setRemoveId(null); setRemoveConfirm('') }} style={{ fontSize: '12px', padding: '4px 10px' }}>
                        Cancel
                      </button>
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={() => !isLastAdmin && setRemoveId(u.id)}
                      disabled={isLastAdmin}
                      title={isLastAdmin ? 'Cannot remove the last admin' : ''}
                      style={{ fontSize: '12px', padding: '4px 10px', opacity: isLastAdmin ? 0.6 : 1 }}
                    >
                      Remove
                    </button>
                  )}
                </td>
              </tr>
              )
            })}
          </tbody>
        </table>
        )
      })()}

      {/* Reset All Data – admin only */}
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
    </div>
  )
}
