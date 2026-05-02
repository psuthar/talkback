import { describe, it, expect, vi } from 'vitest'
import { patchSessionPrimary, SessionPrimaryError } from '../api/sessionPrimary'

describe('patchSessionPrimary (SCRUM-275 client)', () => {
	it('PATCHes the right URL and body on happy path', async () => {
		const fetchImpl = vi.fn().mockResolvedValue({
			ok: true,
			json: async () => ({ id: 's1' }),
		})
		const out = await patchSessionPrimary(
			'https://api/',
			's1',
			{ kind: 'document', id: 'm1' },
			{ fetchImpl },
		)
		expect(fetchImpl).toHaveBeenCalledTimes(1)
		const [url, init] = fetchImpl.mock.calls[0]
		expect(url).toBe('https://api/api/sessions/s1')
		expect(init.method).toBe('PATCH')
		expect(init.credentials).toBe('include')
		expect(JSON.parse(init.body)).toEqual({
			primary: { kind: 'document', id: 'm1' },
		})
		expect(out).toEqual({ id: 's1' })
	})

	it('sends primary.kind="" when called with primary=null (clear)', async () => {
		const fetchImpl = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
		await patchSessionPrimary('https://api', 's1', null, { fetchImpl })
		const [, init] = fetchImpl.mock.calls[0]
		expect(JSON.parse(init.body)).toEqual({ primary: { kind: '' } })
	})

	it('throws SessionPrimaryError carrying the response code on 4xx', async () => {
		const fetchImpl = vi.fn().mockResolvedValue({
			ok: false,
			status: 400,
			json: async () => ({ error: 'material missing', code: 'PRIMARY_MATERIAL_NOT_FOUND' }),
		})
		await expect(
			patchSessionPrimary('https://api', 's1', { kind: 'document', id: 'm1' }, { fetchImpl }),
		).rejects.toMatchObject({
			name: 'SessionPrimaryError',
			status: 400,
			code: 'PRIMARY_MATERIAL_NOT_FOUND',
			message: 'material missing',
		})
	})

	it('throws SessionPrimaryError with empty code when the response has no JSON body', async () => {
		const fetchImpl = vi.fn().mockResolvedValue({
			ok: false,
			status: 500,
			json: async () => { throw new Error('not json') },
		})
		const err = await patchSessionPrimary('https://api', 's1', { kind: 'document', id: 'm1' }, { fetchImpl })
			.catch((e) => e)
		expect(err).toBeInstanceOf(SessionPrimaryError)
		expect(err.status).toBe(500)
		expect(err.code).toBe('')
	})
})
