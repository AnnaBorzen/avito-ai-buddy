import { useState } from 'react';
import {
  ArrowRight,
  Check,
  ClipboardList,
  Clock3,
  FileText,
  RotateCw,
  ShieldCheck,
} from 'lucide-react';
import { blank, levels, ticketLabels, valid } from './model';
import type { Context, Fields, Level } from './model';
interface Props {
  ctx: Context;
  busy: string;
  uncertain: boolean;
  save: (f: Fields) => Promise<boolean>;
  confirm: () => void;
  reconcile: () => void;
  newTask: () => void;
  feedback: (yes: boolean) => void;
}
export function RequestCard({
  ctx,
  busy,
  uncertain,
  save,
  confirm,
  reconcile,
  newTask,
  feedback,
}: Props) {
  const op = ctx.active_operation,
    ticket = op?.ticket;
  const [editing, setEditing] = useState(false);
  const [fields, setFields] = useState<Fields>(() => ({
    ...blank(),
    ...(ctx.partial || op?.payload),
  }));
  const [touched, setTouched] = useState(false);
  const locked = !!busy,
    pending = busy === 'confirm';
  const error =
    touched &&
    (!fields.resource_id
      ? 'Выберите ресурс.'
      : !fields.access_level
        ? 'Выберите уровень доступа.'
        : fields.justification.trim().length < 10
          ? 'Опишите рабочую причину минимум в 10 символах.'
          : '');
  const form = (editing || ctx.partial) && !ticket && !uncertain && !pending;
  const content = ticket || op?.payload;
  function edit() {
    setFields({ ...blank(), ...(ctx.partial || op?.payload) });
    setEditing(true);
    setTouched(false);
  }
  function set<K extends keyof Fields>(key: K, value: Fields[K]) {
    setFields((prev) => ({ ...prev, [key]: value }));
  }
  return (
    <section
      className="panel request"
      id="request"
      aria-labelledby="request-title"
    >
      <div className="panelhead">
        <div className="iconbox mint">
          <ClipboardList />
        </div>
        <div>
          <h2 id="request-title">
            {ticket ? 'Ваша заявка' : 'Заявка на доступ'}
          </h2>
          <span className="sub">
            {ticket
              ? 'Результат из сервиса доступов'
              : op
                ? 'Проверьте перед отправкой'
                : 'Пространства и инструменты команды'}
          </span>
        </div>
      </div>
      {form ? (
        <form
          className="draftform"
          onSubmit={async (e) => {
            e.preventDefault();
            setTouched(true);
            if (valid(fields) && (await save(fields))) setEditing(false);
          }}
        >
          <label>
            Ресурс
            <select
              aria-label="Ресурс"
              value={fields.resource_id}
              disabled={locked}
              onChange={(e) => set('resource_id', e.target.value)}
            >
              <option value="">Выберите ресурс</option>
              {ctx.resources.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            Уровень доступа
            <select
              aria-label="Уровень доступа"
              value={fields.access_level}
              disabled={locked}
              onChange={(e) => set('access_level', e.target.value as Level)}
            >
              <option value="">Выберите уровень</option>
              <option value="read">Только чтение</option>
              <option value="write">Редактирование</option>
            </select>
          </label>
          <label>
            Зачем нужен доступ
            <textarea
              aria-label="Зачем нужен доступ"
              maxLength={300}
              disabled={locked}
              value={fields.justification}
              placeholder="Например, изучить архитектуру сервисов команды"
              onChange={(e) => set('justification', e.target.value)}
            />
            <span className="sub">
              От 10 до 300 символов · {fields.justification.trim().length}/300
            </span>
          </label>
          {error && (
            <p role="alert" className="inline-error">
              {error}
            </p>
          )}
          <button className="primary" disabled={locked}>
            Проверить заявку <ArrowRight size={15} />
          </button>
          {editing && !ctx.partial && (
            <button
              type="button"
              className="link"
              disabled={locked}
              onClick={() => setEditing(false)}
            >
              Отменить изменения
            </button>
          )}
        </form>
      ) : content ? (
        <div className="cardbody">
          <span
            className={
              'pill ' +
              (uncertain || ticket?.external_status === 'rejected'
                ? 'amber'
                : ticket
                  ? 'green'
                  : 'lavender')
            }
          >
            {pending
              ? 'Отправляем…'
              : uncertain
                ? 'Результат неизвестен'
                : ticket
                  ? ticketLabels[ticket.external_status]
                  : op?.operation_status === 'failed'
                    ? 'Можно повторить'
                    : 'К подтверждению'}
          </span>
          <h3>{content.resource_title}</h3>
          {ticket && (
            <p className="ticket-id" aria-label="Номер заявки">
              {ticket.ticket_id}
            </p>
          )}
          <dl>
            <dt>Уровень доступа</dt>
            <dd>{levels[content.access_level]}</dd>
            <dt>Согласующий</dt>
            <dd>{content.approver_name}</dd>
            <dt>Обоснование</dt>
            <dd>{content.justification}</dd>
          </dl>
          <p className="notice" role="status">
            <Clock3 size={16} />
            {pending
              ? 'Ждём ответ сервиса доступов.'
              : uncertain
                ? 'Ответ мог потеряться. Проверьте эту операцию перед повторной отправкой.'
                : ctx.scenario.next_step_hint}
          </p>
          {uncertain ? (
            <button disabled={locked} className="primary" onClick={reconcile}>
              <RotateCw size={15} /> Проверить статус операции
            </button>
          ) : ticket ? (
            <>
              {ticket.external_status === 'pending' && (
                <a
                  className="link"
                  href="https://github.com/avito-tech/playbook"
                  target="_blank"
                  rel="noreferrer"
                >
                  Открыть AvitoTech Playbook ↗
                </a>
              )}
              <button className="secondary" disabled={locked} onClick={newTask}>
                Начать другую заявку
              </button>
              <div className="feedback">
                {ctx.feedback === null ? (
                  <>
                    Помощник помог?{' '}
                    <button disabled={locked} onClick={() => feedback(true)}>
                      Да
                    </button>
                    <button disabled={locked} onClick={() => feedback(false)}>
                      Нет
                    </button>
                  </>
                ) : (
                  <span>
                    <Check size={14} /> Оценка сохранена
                  </span>
                )}
              </div>
            </>
          ) : (
            <>
              <button disabled={locked} className="primary" onClick={confirm}>
                <ShieldCheck size={16} />
                {pending
                  ? 'Отправляем…'
                  : op?.operation_status === 'failed'
                    ? 'Повторить отправку'
                    : 'Подтвердить и отправить'}
              </button>
              <button disabled={locked} className="link" onClick={edit}>
                Изменить параметры
              </button>
            </>
          )}
        </div>
      ) : (
        <div className="emptycard">
          <div className="paper">
            <FileText size={33} strokeWidth={1.5} />
          </div>
          <h3>Подготовьте запрос</h3>
          <p>
            Обсудите задачу с помощником или укажите параметры самостоятельно.
          </p>
          <button disabled={locked} className="secondary" onClick={edit}>
            Заполнить вручную <ArrowRight size={15} />
          </button>
        </div>
      )}
      {ctx.previous_tickets?.length > 0 && (
        <details className="history">
          <summary>Предыдущие заявки · {ctx.previous_tickets.length}</summary>
          {ctx.previous_tickets.map((t) => (
            <article key={t.ticket_id}>
              <strong>
                {t.ticket_id} · {ticketLabels[t.external_status]}
              </strong>
              <p>
                {t.resource_title} · {levels[t.access_level]}
              </p>
              <p>{t.justification}</p>
              <small>Согласующий · {t.approver_name}</small>
            </article>
          ))}
        </details>
      )}
    </section>
  );
}
