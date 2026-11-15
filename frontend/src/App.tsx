import { useEffect, useRef, useState } from 'react';
import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronRight,
  FlaskConical,
  Layers3,
  LoaderCircle,
  MapPin,
  MessageSquareText,
} from 'lucide-react';
import { ApiError, request } from './api/client';
import { Chat } from './Chat';
import { RequestCard } from './RequestCard';
import { confirmationKey, needsCheck } from './domain/stateMachine';
import type { Context, Mode } from './model';
const recoverKey = (user: string) => 'buddy.unanswered.' + user;
function marker(user: string, value?: string | null) {
  try {
    const key = recoverKey(user);
    if (value === null) sessionStorage.removeItem(key);
    else if (value !== undefined) sessionStorage.setItem(key, value);
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}
export function App() {
  const [ctx, setCtx] = useState<Context | null>(null),
    [busy, setBusy] = useState(''),
    [error, setError] = useState('');
  const [mode, setMode] = useState<Mode>('normal'),
    [unanswered, setUnanswered] = useState<string | null>(null);
  const gate = useRef(false);
  useEffect(() => {
    let live = true;
    request('/context')
      .then((c) => {
        if (live) {
          setCtx(c);
          setUnanswered(marker(c.user.user_id));
        }
      })
      .catch(() => {
        if (live)
          setError(
            'Не удалось загрузить профиль. Проверьте подключение к сервису.',
          );
      });
    return () => {
      live = false;
    };
  }, []);
  const op = ctx?.active_operation;
  async function action(
    kind: string,
    path: string,
    body?: object,
    key?: string,
  ) {
    if (gate.current) return false;
    gate.current = true;
    setBusy(kind);
    setError('');
    const user = ctx?.user.user_id,
      confirm = kind === 'confirm' && op && user;
    if (confirm) {
      marker(user, op.operation_id);
      setUnanswered(op.operation_id);
    }
    try {
      const result = await request(
        path,
        body
          ? { ...body, task_instance_id: ctx?.scenario.task_instance_id }
          : undefined,
        path === '/demo/session' || kind === 'load' ? undefined : user,
        key,
        ctx || undefined,
      );
      setCtx(result);
      if (kind === 'profile') {
        setMode('normal');
        setUnanswered(marker(result.user.user_id));
      }
      if (kind === 'confirm' || kind === 'reconcile' || kind === 'new') {
        marker(result.user.user_id, null);
        setUnanswered(null);
      }
      return true;
    } catch (e) {
      const err = e instanceof ApiError ? e : null;
      if (err?.context) setCtx(err.context);
      if (confirm && err && (err.context || err.status < 500)) {
        marker(user, null);
        setUnanswered(null);
      }
      setError(
        err?.message ||
          (confirm
            ? 'Соединение прервалось при отправке. Проверьте статус операции.'
            : 'Сервис недоступен. Проверьте соединение и повторите действие.'),
      );
      return false;
    } finally {
      gate.current = false;
      setBusy('');
    }
  }
  const locked = !!busy;
  const ctxKey = ctx?.scenario.task_instance_id || '';
  return (
    <>
      <header className="top">
        <div className="wrap nav">
          <div className="brand">
            <span className="mark" aria-hidden="true">
              <i />
              <i />
              <i />
              <i />
            </span>
            Авито <span className="brandline" />{' '}
            <span className="brand-sub">Старт</span>
          </div>
          {ctx && (
            <label className="profile">
              <span className="avatar">
                {ctx.user.name
                  .split(' ')
                  .map((n) => n[0])
                  .join('')}
              </span>
              <select
                aria-label="Тестовый профиль"
                value={ctx.user.user_id}
                disabled={locked}
                onChange={(e) =>
                  action('profile', '/demo/session', {
                    user_id: e.target.value,
                  })
                }
              >
                {ctx.profiles.map((p) => (
                  <option key={p.user_id} value={p.user_id}>
                    {p.name}
                  </option>
                ))}
              </select>
              <ChevronDown size={14} />
            </label>
          )}
        </div>
      </header>
      <main className="wrap shell">
        {!ctx ? (
          <section className="panel loading" aria-live="polite">
            {error ? (
              <>
                <AlertCircle />
                <p>{error}</p>
                <button
                  className="secondary"
                  onClick={() => action('load', '/context')}
                >
                  Загрузить снова
                </button>
              </>
            ) : (
              <>
                <LoaderCircle className="spin" /> Загружаем профиль и вашу
                заявку…
              </>
            )}
          </section>
        ) : (
          <>
            <div className="trail">
              Адаптация <ChevronRight size={13} /> Первая неделя
            </div>
            <section className="intro">
              <div>
                <h1>Доступ в команду</h1>
                <p>
                  Оформите доступ к рабочим материалам и следите за
                  согласованием.
                </p>
              </div>
              <div className="team">
                <span className="team-icon">
                  <Layers3 size={18} />
                </span>
                <div>
                  <span>
                    {ctx.user.role === 'lead'
                      ? 'Руководитель команды'
                      : 'Backend-стажёр'}
                  </span>
                  <strong>{ctx.user.team_name}</strong>
                </div>
              </div>
            </section>
            <ol className="steps" aria-label="Этапы запроса">
              {ctx.scenario.steps.map((s, i) => (
                <li
                  key={s.id}
                  className={s.status}
                  aria-current={
                    ['in_progress', 'blocked'].includes(s.status)
                      ? 'step'
                      : undefined
                  }
                >
                  <span className="stepnum">
                    {s.status === 'done' ? <Check size={16} /> : i + 1}
                  </span>
                  <div>
                    <span>
                      {s.status === 'done'
                        ? 'Завершено'
                        : s.status === 'not_started'
                          ? 'Впереди'
                          : s.status === 'blocked'
                            ? 'Требует внимания'
                            : 'Текущий шаг'}
                    </span>
                    <strong>{s.title}</strong>
                  </div>
                </li>
              ))}
            </ol>
            {error && (
              <div className="error" role="alert">
                <AlertCircle size={18} />
                <span>{error}</span>
                <button
                  onClick={() => action('load', '/context')}
                  disabled={locked}
                >
                  Обновить данные
                </button>
              </div>
            )}
            <div className="columns">
              <Chat
                key={ctxKey}
                ctx={ctx}
                busy={busy}
                send={(text) =>
                  action('chat', '/chat/messages', { message: text })
                }
                handoff={() => action('handoff', '/handoff', {})}
              />
              <RequestCard
                key={
                  ctxKey +
                  '|' +
                  (op?.draft_version || 0) +
                  '|' +
                  JSON.stringify(ctx.partial)
                }
                ctx={ctx}
                busy={busy}
                uncertain={busy !== 'confirm' && needsCheck(ctx, unanswered)}
                save={(fields) =>
                  action('save', '/operations/draft', {
                    ...fields,
                    draft_version: op?.draft_version || 0,
                  })
                }
                confirm={() =>
                  op &&
                  action(
                    'confirm',
                    '/operations/' + op.operation_id + '/confirm',
                    { draft_version: op.draft_version, simulate_mode: mode },
                    confirmationKey(op),
                  )
                }
                reconcile={() =>
                  op &&
                  action(
                    'reconcile',
                    '/operations/' + op.operation_id + '/reconcile',
                    {},
                  )
                }
                newTask={() => {
                  setMode('normal');
                  void action('new', '/tasks/new', {});
                }}
                feedback={(helpful) =>
                  action('feedback', '/feedback', { helpful })
                }
              />
            </div>
            {ctx.demo && (
              <details className="demopanel">
                <summary>
                  <FlaskConical size={16} /> Панель демонстрации{' '}
                  <ChevronDown size={15} />
                </summary>
                <div className="demo-body">
                  <label>
                    Ответ сервиса при следующей отправке
                    <select
                      value={mode}
                      disabled={locked}
                      onChange={(e) => setMode(e.target.value as Mode)}
                    >
                      <option value="normal">Обычная отправка</option>
                      <option value="timeout">
                        Потеря ответа после сохранения
                      </option>
                      <option value="service_error">Сбой до сохранения</option>
                    </select>
                  </label>
                  {op?.ticket &&
                    ['pending', 'approved'].includes(
                      op.ticket.external_status,
                    ) && (
                      <div>
                        <span>Имитировать решение сервиса</span>
                        <div className="demobuttons">
                          {(op.ticket.external_status === 'pending'
                            ? [
                                ['approved', 'Согласовать'],
                                ['rejected', 'Отклонить'],
                              ]
                            : [['provisioned', 'Подтвердить выдачу']]
                          ).map(([status, label]) => (
                            <button
                              disabled={locked}
                              className="secondary"
                              key={status}
                              onClick={() =>
                                action(
                                  'simulate',
                                  '/operations/' +
                                    op.operation_id +
                                    '/simulate',
                                  { status },
                                )
                              }
                            >
                              {label}
                            </button>
                          ))}
                        </div>
                      </div>
                    )}
                  <div>
                    <span>События текущей задачи</span>
                    <ul className="events">
                      {ctx.events.map((e) => (
                        <li key={e.event_id}>{e.event_name}</li>
                      ))}
                    </ul>
                  </div>
                </div>
              </details>
            )}
            <footer>
              <span>
                <MessageSquareText size={14} /> AI-buddy
              </span>
              <span>
                <MapPin size={13} /> Учебное пространство · Синтетические данные
              </span>
            </footer>
          </>
        )}
      </main>
    </>
  );
}
