import { calculateMbps, jitter, median, scoreMetrics } from '../src/test-engine'

describe('network measurements', () => {
  it('uses a stable median for odd and even sample sets', () => {
    expect(median([40, 10, 20])).toBe(20)
    expect(median([40, 10, 30, 20])).toBe(25)
    expect(median([])).toBe(0)
  })

  it('calculates median inter-sample jitter', () => {
    expect(jitter([10, 14, 13, 21])).toBe(4)
    expect(jitter([10])).toBe(0)
  })

  it('calculates decimal megabits from actual elapsed time', () => {
    expect(calculateMbps(125_000_000, 1_000)).toBe(1000)
    expect(calculateMbps(100, 0)).toBe(0)
  })
})

describe('recommendation score', () => {
  it('rewards balanced real measurements', () => {
    const excellent = scoreMetrics({ latencyMs: 18, jitterMs: 1, lossPercent: 0, downloadMbps: 500, uploadMbps: 150 })
    const lossy = scoreMetrics({ latencyMs: 18, jitterMs: 1, lossPercent: 5, downloadMbps: 900, uploadMbps: 400 })
    expect(excellent).toBeGreaterThan(lossy)
  })

  it('has diminishing bandwidth returns', () => {
    const base = { latencyMs: 60, jitterMs: 5, lossPercent: 0, uploadMbps: 50 }
    const gain50to100 = scoreMetrics({ ...base, downloadMbps: 100 }) - scoreMetrics({ ...base, downloadMbps: 50 })
    const gain500to550 = scoreMetrics({ ...base, downloadMbps: 550 }) - scoreMetrics({ ...base, downloadMbps: 500 })
    expect(gain50to100).toBeGreaterThan(gain500to550)
  })
})
