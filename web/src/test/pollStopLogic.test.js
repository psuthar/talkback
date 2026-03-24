/**
 * Tests for the poll-stop counter logic used in CreatorMode.jsx
 *
 * The three polling loops (processing, ingestion) share the same pattern:
 *
 *   noJobCountRef.current = 0                  // on mount / on job found
 *   noJobCountRef.current += 1                  // on empty response
 *   if (noJobCountRef.current >= 5) stopPolling() // after 5 consecutive empties
 *
 * We extract and verify the pure counter logic here without mounting the component.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// ---------------------------------------------------------------------------
// Helper: simulate the processing poll-stop behaviour
// Returns a controller that mimics what CreatorMode does with processingNoJobCountRef
// ---------------------------------------------------------------------------
function makePollController({ stopThreshold = 5, hasSeenJob = false } = {}) {
  let noJobCount = 0
  let hasSeenProcessingJob = hasSeenJob
  let stopped = false
  let callCount = 0

  function onFetchResult(data) {
    callCount += 1
    if (data.state != null && data.state !== '') {
      // valid job state — reset counter, mark as seen
      noJobCount = 0
      hasSeenProcessingJob = true
    } else {
      noJobCount += 1
      if (!hasSeenProcessingJob && noJobCount >= stopThreshold) {
        stopped = true
      }
    }
  }

  return { onFetchResult, get noJobCount() { return noJobCount }, get stopped() { return stopped }, get callCount() { return callCount }, get hasSeenProcessingJob() { return hasSeenProcessingJob } }
}

// Helper: simulate ingestion poll-stop (stops regardless of hasSeenJob)
function makeIngestionPollController({ stopThreshold = 5 } = {}) {
  let noJobCount = 0
  let stopped = false
  let callCount = 0

  function onFetchResult(data) {
    callCount += 1
    if (data.source && data.state) {
      noJobCount = 0
    } else {
      noJobCount += 1
      if (noJobCount >= stopThreshold) {
        stopped = true
      }
    }
  }

  return { onFetchResult, get noJobCount() { return noJobCount }, get stopped() { return stopped }, get callCount() { return callCount } }
}

// ---------------------------------------------------------------------------
// Processing poll-stop tests
// ---------------------------------------------------------------------------
describe('processing poll-stop logic', () => {
  it('does not stop after fewer than 5 empty responses', () => {
    const ctrl = makePollController()
    for (let i = 0; i < 4; i++) ctrl.onFetchResult({ state: null })
    expect(ctrl.stopped).toBe(false)
    expect(ctrl.noJobCount).toBe(4)
  })

  it('stops after exactly 5 consecutive empty responses when no job has ever been seen', () => {
    const ctrl = makePollController()
    for (let i = 0; i < 5; i++) ctrl.onFetchResult({ state: null })
    expect(ctrl.stopped).toBe(true)
  })

  it('stops after 5 empty responses with empty-string state', () => {
    const ctrl = makePollController()
    for (let i = 0; i < 5; i++) ctrl.onFetchResult({ state: '' })
    expect(ctrl.stopped).toBe(true)
  })

  it('does NOT stop after 5 empty responses when a job was previously seen', () => {
    const ctrl = makePollController({ hasSeenJob: true })
    for (let i = 0; i < 10; i++) ctrl.onFetchResult({ state: '' })
    expect(ctrl.stopped).toBe(false)
  })

  it('resets counter when a valid job appears mid-stream', () => {
    const ctrl = makePollController()
    for (let i = 0; i < 3; i++) ctrl.onFetchResult({ state: '' })
    expect(ctrl.noJobCount).toBe(3)
    ctrl.onFetchResult({ state: 'running' }) // valid job
    expect(ctrl.noJobCount).toBe(0)
    expect(ctrl.hasSeenProcessingJob).toBe(true)
    // Now 10 more empties should NOT stop (job was seen)
    for (let i = 0; i < 10; i++) ctrl.onFetchResult({ state: '' })
    expect(ctrl.stopped).toBe(false)
  })

  it('does not stop before threshold even after many calls with job present', () => {
    const ctrl = makePollController()
    ctrl.onFetchResult({ state: 'running' }) // job seen
    for (let i = 0; i < 20; i++) ctrl.onFetchResult({ state: '' })
    expect(ctrl.stopped).toBe(false)
  })

  it('counter increments by one per empty response', () => {
    const ctrl = makePollController()
    ctrl.onFetchResult({ state: '' })
    expect(ctrl.noJobCount).toBe(1)
    ctrl.onFetchResult({ state: '' })
    expect(ctrl.noJobCount).toBe(2)
  })
})

// ---------------------------------------------------------------------------
// Ingestion poll-stop tests
// ---------------------------------------------------------------------------
describe('ingestion poll-stop logic', () => {
  it('does not stop before 5 empty responses', () => {
    const ctrl = makeIngestionPollController()
    for (let i = 0; i < 4; i++) ctrl.onFetchResult({})
    expect(ctrl.stopped).toBe(false)
  })

  it('stops after exactly 5 empty responses', () => {
    const ctrl = makeIngestionPollController()
    for (let i = 0; i < 5; i++) ctrl.onFetchResult({})
    expect(ctrl.stopped).toBe(true)
  })

  it('resets counter when a valid ingestion job appears', () => {
    const ctrl = makeIngestionPollController()
    for (let i = 0; i < 4; i++) ctrl.onFetchResult({})
    ctrl.onFetchResult({ source: 'zoom', state: 'running' })
    expect(ctrl.noJobCount).toBe(0)
    expect(ctrl.stopped).toBe(false)
  })

  it('does not stop when ingestion jobs keep appearing', () => {
    const ctrl = makeIngestionPollController()
    for (let i = 0; i < 20; i++) ctrl.onFetchResult({ source: 'zoom', state: 'running' })
    expect(ctrl.stopped).toBe(false)
  })

  it('stops after 5 empties following valid jobs', () => {
    const ctrl = makeIngestionPollController()
    // First some valid responses
    for (let i = 0; i < 3; i++) ctrl.onFetchResult({ source: 'zoom', state: 'running' })
    // Then 5 empties
    for (let i = 0; i < 5; i++) ctrl.onFetchResult({})
    expect(ctrl.stopped).toBe(true)
  })
})
