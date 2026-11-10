import { z } from 'zod'

const dependencyCheckSchema = z.object({
  status: z.string(),
  error: z.string().optional(),
})

// Схема ответа GET /readyz — контракт описан в contracts/api.yaml.
const readinessSchema = z.object({
  status: z.string(),
  checks: z.record(z.string(), dependencyCheckSchema),
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
