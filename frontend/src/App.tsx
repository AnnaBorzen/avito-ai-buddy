import { useQuery } from '@tanstack/react-query'
import { fetchReadiness } from './api/health'
import { useUiStore } from './store/ui'

const REFRESH_INTERVAL_MS = 5_000

export default function App() {
  const autoRefresh = useUiStore((state) => state.autoRefresh)
  const toggleAutoRefresh = useUiStore((state) => state.toggleAutoRefresh)

  const { data, error, isPending, isFetching, dataUpdatedAt, refetch } = useQuery({
    queryKey: ['readiness'],
    queryFn: ({ signal }) => fetchReadiness(signal),
    refetchInterval: autoRefresh ? REFRESH_INTERVAL_MS : false,
  })

  const isReady = data?.status === 'ok'

  return (
    <main className="app">
      <header className="app__header">
        <h1>Avito AI Buddy</h1>
        <p className="app__subtitle">
          Каркас проекта: проверка связки frontend → backend → инфраструктура
        </p>
      </header>

      <section className="card" aria-live="polite">
        <div className="card__row">
          <span className={isReady ? 'badge badge--ok' : 'badge badge--warn'}>
            {isPending ? 'проверяем…' : isReady ? 'ready' : 'degraded'}
          </span>
          <button className="button" type="button" onClick={() => void refetch()} disabled={isFetching}>
            Проверить снова
          </button>
        </div>

        {error ? <p className="card__error">{error.message}</p> : null}

        {data ? (
          <ul className="checks">
            {Object.entries(data.checks).map(([name, check]) => (
              <li className="checks__item" key={name}>
                <span>{name}</span>
                <span className={check.status === 'ok' ? 'checks__ok' : 'checks__fail'}>
                  {check.status}
                  {check.error ? `: ${check.error}` : ''}
                </span>
              </li>
            ))}
          </ul>
        ) : null}

        <footer className="card__footer">
          <label className="toggle">
            <input type="checkbox" checked={autoRefresh} onChange={toggleAutoRefresh} />
            Автообновление каждые 5 секунд
          </label>
          {dataUpdatedAt ? (
            <span>Обновлено: {new Date(dataUpdatedAt).toLocaleTimeString('ru-RU')}</span>
          ) : null}
        </footer>
      </section>
    </main>
  )
}
