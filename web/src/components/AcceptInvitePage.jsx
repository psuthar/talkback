import { useState, useEffect, useRef } from 'react'
import { buildCanonicalSessionUrl } from '../sessionNavigation'

function normalizeEmail(e) {
  return (e || '').trim().toLowerCase()
}

function AcceptInvitePageAlreadyAcceptedRedirect({ sessionId, onGoToSession, containerStyle }) {
  useEffect(() => {
    if (sessionId && typeof onGoToSession === 'function') onGoToSession(sessionId)
  }, [sessionId, onGoToSession])
  return (
    <div style={containerStyle}>
      <p style={{ textAlign: 'center', color: '#666' }}>Opening session…</p>
    </div>
  )
}

export function AcceptInvitePage({ apiBaseUrl, token, authUser, authChecked, onLoginSuccess, onRegisterSuccess, onSignOut, onGoToSession }) {
  const [resolveResult, setResolveResult] = useState(null)
  const [resolveError, setResolveError] = useState(null)
  const [resolveLoading, setResolveLoading] = useState(true)
  const [signupName, setSignupName] = useState('')
  const [signupPassword, setSignupPassword] = useState('')
  const [signupSubmitting, setSignupSubmitting] = useState(false)
  const [signupError, setSignupError] = useState('')
  const [loginEmail, setLoginEmail] = useState('')
  const [loginPassword, setLoginPassword] = useState('')
  const [loginSubmitting, setLoginSubmitting] = useState(false)
  const [loginError, setLoginError] = useState('')
  const [acceptLoading, setAcceptLoading] = useState(false)
  const [acceptError, setAcceptError] = useState('')
  const [acceptAuthToken, setAcceptAuthToken] = useState(null) // short-lived token from login (for_accept_invite) so accept works in incognito

  const base = (apiBaseUrl || '').replace(/\/$/, '')

  useEffect(() => {
    if (!token) {
      setResolveLoading(false)
      if (!token) setResolveError('invalid link: missing token')
      return
    }
    setResolveLoading(true)
    setResolveError(null)
    fetch(`${base}/api/invitations/resolve?token=${encodeURIComponent(token)}`, { credentials: 'include' })
      .then((res) => res.json().catch(() => ({})))
      .then((data) => {
        if (data.invitation) {
          setResolveResult(data.invitation)
          setLoginEmail(data.invitation.invited_email || '')
        } else {
          setResolveError(data.error || 'Invalid or expired link')
        }
      })
      .catch(() => setResolveError('Failed to load invitation'))
      .finally(() => setResolveLoading(false))
  }, [token, base])

  const invitedEmail = resolveResult?.invited_email ? normalizeEmail(resolveResult.invited_email) : ''
  const currentEmail = authUser?.email ? normalizeEmail(authUser.email) : ''
  const isCorrectUser = invitedEmail && currentEmail && invitedEmail === currentEmail
  const accountExists = resolveResult?.account_exists === true

  const handleRegisterAndAccept = async (e) => {
    e.preventDefault()
    if (!token || !signupName.trim() || !signupPassword) return
    setSignupSubmitting(true)
    setSignupError('')
    try {
      const res = await fetch(`${base}/api/invitations/register-and-accept`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ token, name: signupName.trim(), password: signupPassword })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setSignupError(data.error || 'Registration failed')
        return
      }
      if (onRegisterSuccess && data.user && data.session_id) {
        onRegisterSuccess({ user: data.user, sessionId: data.session_id, acceptToken: data.accept_token || null })
        return
      }
      const redirectTo = data.redirect_to || (data.session_id ? buildCanonicalSessionUrl(data.session_id, { mode: 'view' }) : '/')
      window.location.href = redirectTo
    } catch (err) {
      setSignupError(err.message || 'Network error')
    } finally {
      setSignupSubmitting(false)
    }
  }

  const handleLogin = async (e) => {
    e.preventDefault()
    setLoginError('')
    setLoginSubmitting(true)
    setAcceptAuthToken(null)
    try {
      const res = await fetch(`${base}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          email: loginEmail.trim().toLowerCase(),
          password: loginPassword,
          for_accept_invite: true
        })
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setLoginError(data.error || 'Login failed')
        return
      }
      if (data.accept_token) setAcceptAuthToken(data.accept_token)
      // Accept the invitation so the user is added as participant (works in incognito with accept_token)
      const sessionId = resolveResult?.session_id
      if (token && sessionId) {
        try {
          const acceptBody = { token }
          if (data.accept_token) acceptBody.accept_auth_token = data.accept_token
          const acceptRes = await fetch(`${base}/api/invitations/accept`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            credentials: 'include',
            body: JSON.stringify(acceptBody)
          })
          const acceptData = await acceptRes.json().catch(() => ({}))
          if (acceptRes.ok && acceptData.session_id) {
            onLoginSuccess(data, { goToSessionId: acceptData.session_id })
            return
          }
        } catch (_) { /* continue to open session anyway */ }
        onLoginSuccess(data, { goToSessionId: sessionId })
      } else {
        onLoginSuccess(data)
      }
    } catch (err) {
      setLoginError(err.message || 'Network error')
    } finally {
      setLoginSubmitting(false)
    }
  }

  const handleAccept = async () => {
    if (!token) return
    setAcceptLoading(true)
    setAcceptError('')
    try {
      const body = { token }
      if (acceptAuthToken) body.accept_auth_token = acceptAuthToken
      const res = await fetch(`${base}/api/invitations/accept`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(body)
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setAcceptError(data.error || 'Failed to join')
        return
      }
      setAcceptAuthToken(null)
      let redirectTo = data.redirect_to
      if (redirectTo?.startsWith('/sessions/')) {
        const id = data.session_id || redirectTo.replace(/^\/sessions\//, '')
        redirectTo = id ? buildCanonicalSessionUrl(id, { mode: 'view' }) : '/'
      } else if (!redirectTo && data.session_id) {
        redirectTo = buildCanonicalSessionUrl(data.session_id, { mode: 'view' })
      } else if (!redirectTo) {
        redirectTo = '/'
      }
      window.location.href = redirectTo
    } catch (err) {
      setAcceptError(err.message || 'Network error')
    } finally {
      setAcceptLoading(false)
    }
  }

  const containerStyle = { maxWidth: '440px', margin: '48px auto', padding: '24px' }
  const cardStyle = { border: '1px solid #ddd', borderRadius: '8px', padding: '24px', backgroundColor: '#fafafa', marginTop: '16px' }
  const headingStyle = { marginBottom: '8px', textAlign: 'center' }
  const subStyle = { color: '#666', textAlign: 'center', marginBottom: '16px', fontSize: '14px' }
  const errorStyle = { marginBottom: '16px', padding: '10px', borderRadius: '6px', fontSize: '14px', backgroundColor: '#ffebee', color: 'var(--color-danger-dark)' }
  const btnPrimary = { padding: '10px 20px', backgroundColor: 'var(--color-primary-mid)', color: 'white', border: 'none', borderRadius: '6px', fontSize: '14px', fontWeight: '600', cursor: 'pointer' }
  const btnSecondary = { padding: '8px 14px', backgroundColor: '#757575', color: 'white', border: 'none', borderRadius: '6px', fontSize: '13px', cursor: 'pointer', marginLeft: '8px' }

  if (!authChecked) {
    return (
      <div style={containerStyle}>
        <p style={{ textAlign: 'center', color: '#666' }}>Loading…</p>
      </div>
    )
  }

  if (!token) {
    return (
      <div style={containerStyle}>
        <h1 style={headingStyle}>Invitation</h1>
        <div style={{ ...cardStyle, ...errorStyle }}>
          This link does not contain an invitation token. Please use the full link from your invitation email (it should include <code>?token=...</code> in the address bar). If you copied a different link (e.g. a session view link), open the invitation email and click the “Accept your invitation” link instead.
        </div>
      </div>
    )
  }

  if (resolveLoading) {
    return (
      <div style={containerStyle}>
        <p style={{ textAlign: 'center', color: '#666' }}>Loading invitation…</p>
      </div>
    )
  }

  if (resolveError || !resolveResult) {
    return (
      <div style={containerStyle}>
        <h1 style={headingStyle}>Invitation</h1>
        <div style={{ ...cardStyle, ...errorStyle }}>
          {resolveError || 'Invalid or expired invitation link.'}
        </div>
      </div>
    )
  }

  // Already accepted (re-use same link): go to session if logged in, else prompt login then go
  if (resolveResult.status === 'accepted' && resolveResult.session_id) {
    if (authUser) {
      // Redirect to session (effect runs once)
      return <AcceptInvitePageAlreadyAcceptedRedirect sessionId={resolveResult.session_id} onGoToSession={onGoToSession} containerStyle={containerStyle} />
    }
    return (
      <div style={containerStyle}>
        <h1 style={headingStyle}>Open session</h1>
        <p style={subStyle}>
          You’ve already accepted this invitation to <strong>{resolveResult.session_title || 'the session'}</strong>.
          Sign in to open the session.
        </p>
        <div style={cardStyle}>
          <form onSubmit={(e) => {
            e.preventDefault()
            setLoginError('')
            setLoginSubmitting(true)
            setAcceptAuthToken(null)
            fetch(`${base}/api/auth/login`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              credentials: 'include',
              body: JSON.stringify({
                email: loginEmail.trim().toLowerCase(),
                password: loginPassword,
                for_accept_invite: true
              })
            })
              .then((res) => res.json().catch(() => ({})))
              .then((data) => {
                if (!data.id) {
                  setLoginError(data.error || 'Login failed')
                  return
                }
                if (data.accept_token) setAcceptAuthToken(data.accept_token)
                onLoginSuccess(data, { goToSessionId: resolveResult.session_id })
              })
              .catch(() => setLoginError('Network error'))
              .finally(() => setLoginSubmitting(false))
          }}>
            <div style={{ marginBottom: '12px' }}>
              <label style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Email</label>
              <input
                type="email"
                value={loginEmail}
                onChange={(e) => setLoginEmail(e.target.value)}
                required
                placeholder="you@example.com"
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box' }}
              />
            </div>
            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Password</label>
              <input
                type="password"
                value={loginPassword}
                onChange={(e) => setLoginPassword(e.target.value)}
                required
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box' }}
              />
            </div>
            {loginError && <div style={errorStyle}>{loginError}</div>}
            <button type="submit" disabled={loginSubmitting} style={{ ...btnPrimary, width: '100%' }}>
              {loginSubmitting ? 'Signing in…' : 'Sign in and open session'}
            </button>
          </form>
        </div>
      </div>
    )
  }

  // A – No account: signup form
  if (!accountExists) {
    return (
      <div style={containerStyle}>
        <h1 style={headingStyle}>Join session</h1>
        <p style={subStyle}>
          You’re invited to <strong>{resolveResult.session_title || 'a session'}</strong>
          {resolveResult.inviter_name && ` by ${resolveResult.inviter_name}`}.
        </p>
        <div style={cardStyle}>
          <p style={{ marginBottom: '16px', fontSize: '14px' }}>Create an account to join (email is fixed from your invitation):</p>
          <form onSubmit={handleRegisterAndAccept}>
            <div style={{ marginBottom: '12px' }}>
              <label style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Email</label>
              <input
                type="email"
                value={resolveResult.invited_email || ''}
                readOnly
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box', backgroundColor: '#eee' }}
              />
            </div>
            <div style={{ marginBottom: '12px' }}>
              <label htmlFor="signup-display-name" style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Display name</label>
              <input
                id="signup-display-name"
                data-testid="signup-display-name"
                type="text"
                value={signupName}
                onChange={(e) => setSignupName(e.target.value)}
                required
                placeholder="Your name"
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box' }}
              />
            </div>
            <div style={{ marginBottom: '16px' }}>
              <label htmlFor="signup-password" style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Password</label>
              <input
                id="signup-password"
                data-testid="signup-password"
                type="password"
                value={signupPassword}
                onChange={(e) => setSignupPassword(e.target.value)}
                required
                placeholder="••••••••"
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box' }}
              />
            </div>
            {signupError && <div style={errorStyle}>{signupError}</div>}
            <button type="submit" disabled={signupSubmitting} style={{ ...btnPrimary, width: '100%' }}>
              {signupSubmitting ? 'Creating account…' : 'Create account & join'}
            </button>
          </form>
        </div>
      </div>
    )
  }

  // B – Account exists, not signed in: sign-in form
  if (!authUser) {
    return (
      <div style={containerStyle}>
        <h1 style={headingStyle}>Join session</h1>
        <p style={subStyle}>
          You’re invited to <strong>{resolveResult.session_title || 'a session'}</strong>
          {resolveResult.inviter_name && ` by ${resolveResult.inviter_name}`}. Sign in to continue.
        </p>
        <div style={cardStyle}>
          <form onSubmit={handleLogin}>
            <div style={{ marginBottom: '12px' }}>
              <label style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Email</label>
              <input
                type="email"
                value={loginEmail}
                onChange={(e) => setLoginEmail(e.target.value)}
                required
                placeholder="you@example.com"
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box' }}
              />
            </div>
            <div style={{ marginBottom: '16px' }}>
              <label style={{ display: 'block', marginBottom: '4px', fontWeight: '500', fontSize: '14px' }}>Password</label>
              <input
                type="password"
                value={loginPassword}
                onChange={(e) => setLoginPassword(e.target.value)}
                required
                style={{ width: '100%', padding: '10px 12px', fontSize: '14px', boxSizing: 'border-box' }}
              />
            </div>
            {loginError && <div style={errorStyle}>{loginError}</div>}
            <button type="submit" disabled={loginSubmitting} style={{ ...btnPrimary, width: '100%' }}>
              {loginSubmitting ? 'Signing in…' : 'Sign in'}
            </button>
          </form>
        </div>
      </div>
    )
  }

  // D – Signed in as wrong user
  if (!isCorrectUser) {
    return (
      <div style={containerStyle}>
        <h1 style={headingStyle}>Wrong account</h1>
        <div style={cardStyle}>
          <p style={{ marginBottom: '16px', fontSize: '14px' }}>
            This invitation was sent to <strong>{resolveResult.invited_email}</strong>, but you’re signed in as <strong>{authUser.email}</strong>.
          </p>
          <p style={{ marginBottom: '16px', fontSize: '14px' }}>Sign out and open the invitation link again using the correct account.</p>
          {onSignOut && (
            <button type="button" onClick={onSignOut} style={btnPrimary}>
              Sign out
            </button>
          )}
        </div>
      </div>
    )
  }

  // C – Signed in as correct user: auto-join so the session opens without an extra click (invite link purpose)
  const autoJoinStarted = useRef(false)
  useEffect(() => {
    if (autoJoinStarted.current || !token || !resolveResult || resolveResult.status === 'accepted') return
    autoJoinStarted.current = true
    handleAccept()
  }, [token, resolveResult?.session_id, resolveResult?.status])

  const isUnauthorized = acceptError && (acceptError.toLowerCase() === 'unauthorized' || acceptError.toLowerCase().includes('unauthorized'))
  return (
    <div style={containerStyle}>
      <h1 style={headingStyle}>Join session</h1>
      <p style={subStyle}>
        You’re invited to <strong>{resolveResult.session_title || 'a session'}</strong>
        {resolveResult.inviter_name && ` by ${resolveResult.inviter_name}`}.
      </p>
      <div style={cardStyle}>
        <p style={{ marginBottom: '16px', fontSize: '14px' }}>You’re signed in as {authUser.email}.</p>
        {acceptError && (
          <div style={errorStyle}>
            {isUnauthorized ? (
              <>
                The server didn’t recognize your session. This often happens when the app and API are on different domains and the login cookie wasn’t sent, or when using a private/incognito window (browsers may block the cookie there). Try signing out and signing in again on this page, or use a normal browser window instead of private/incognito.
                {onSignOut && (
                  <div style={{ marginTop: '12px' }}>
                    <button type="button" onClick={onSignOut} style={{ ...btnSecondary, marginLeft: 0 }}>
                      Sign out and try again
                    </button>
                  </div>
                )}
              </>
            ) : (
              acceptError
            )}
          </div>
        )}
        {acceptError && (
          <button type="button" onClick={handleAccept} disabled={acceptLoading} style={btnPrimary}>
            {acceptLoading ? 'Joining…' : 'Join session'}
          </button>
        )}
      </div>
    </div>
  )
}
