import { z } from 'zod'

// Схема ответа GET /readyz — контракт описан в contracts/api.yaml.
const readinessSchema = z.object({
  status: z.enum(['ok', 'degraded']),
  dependencies: z.record(z.enum(['ok', 'unavailable'])),
})

export type Readiness = z.infer<typeof readinessSchema>

export async function fetchReadiness(signal?: AbortSignal): Promise<Readiness> {
  const response = await fetch('/readyz', {
    signal,
    headers: { Accept: 'application/json' },
  })

  const payload: unknown = await response.json().catch(() => null)
  const parsed = readinessSchema.safeParse(payload)

  if (!parsed.success) {
    throw new Error(`Неожиданный ответ /readyz (HTTP ${response.status})`)
  }

  return parsed.data
}
