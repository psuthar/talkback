import { describe, it, expect } from 'vitest'
import { resolvePrimaryAutoSelection } from '../components/sessionPrimaryAutoSelect'

describe('resolvePrimaryAutoSelection (SCRUM-274)', () => {
	it('returns null when currentSession is missing', () => {
		expect(resolvePrimaryAutoSelection(null, null)).toBeNull()
		expect(resolvePrimaryAutoSelection(undefined, null)).toBeNull()
	})

	it('returns null when there is no primary descriptor', () => {
		expect(resolvePrimaryAutoSelection({ session: { id: 's1' } }, null)).toBeNull()
		expect(resolvePrimaryAutoSelection({ session: { id: 's1' }, primary: null }, null)).toBeNull()
	})

	it('returns null when primary.id is missing', () => {
		expect(resolvePrimaryAutoSelection({ primary: { kind: 'document' } }, null)).toBeNull()
	})

	it('returns null when the user already has something selected', () => {
		const session = {
			primary: { kind: 'document', id: 'm1' },
			materials: [{ id: 'm1', filename: 'x.pdf' }],
		}
		expect(resolvePrimaryAutoSelection(session, 'm1')).toBeNull()
		expect(resolvePrimaryAutoSelection(session, 'something-else')).toBeNull()
	})

	it('selects the matching material when kind=document', () => {
		const material = { id: 'm1', filename: 'spec.pdf' }
		const session = {
			primary: { kind: 'document', id: 'm1' },
			materials: [{ id: 'other', filename: 'x.pdf' }, material],
		}
		const got = resolvePrimaryAutoSelection(session, null)
		expect(got).toEqual({ type: 'material', material })
	})

	it('returns null when kind=document but no material matches', () => {
		const session = {
			primary: { kind: 'document', id: 'm-missing' },
			materials: [{ id: 'm1', filename: 'x.pdf' }],
		}
		expect(resolvePrimaryAutoSelection(session, null)).toBeNull()
	})

	it('selects the matching link from currentSession.links when kind=link', () => {
		const link = { id: 'l1', url: 'https://example.com', title: 'Example' }
		const session = {
			primary: { kind: 'link', id: 'l1' },
			links: [link],
		}
		const got = resolvePrimaryAutoSelection(session, null)
		expect(got).toEqual({ type: 'link', link })
	})

	it('falls back to currentSession.session.links when top-level links is absent', () => {
		const link = { id: 'l1', url: 'https://example.com' }
		const session = {
			primary: { kind: 'link', id: 'l1' },
			session: { links: [link] },
		}
		const got = resolvePrimaryAutoSelection(session, null)
		expect(got).toEqual({ type: 'link', link })
	})

	it('returns null when the link lacks a url', () => {
		const session = {
			primary: { kind: 'link', id: 'l1' },
			links: [{ id: 'l1', url: '' }],
		}
		expect(resolvePrimaryAutoSelection(session, null)).toBeNull()
	})

	it('returns null when kind=video so the existing video default renders', () => {
		const session = {
			primary: { kind: 'video', id: 'fa1' },
			materials: [{ id: 'fa1', filename: 'video.mp4' }],
		}
		expect(resolvePrimaryAutoSelection(session, null)).toBeNull()
	})

	it('returns null on unknown kinds (defensive)', () => {
		const session = {
			primary: { kind: 'audio', id: 'x' },
		}
		expect(resolvePrimaryAutoSelection(session, null)).toBeNull()
	})
})
