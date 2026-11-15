import { createRequire } from 'node:module';
import { randomUUID } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { answer } from './assistant.mjs';
import { profiles, resources, validate, problem } from './catalog.mjs';
const { DatabaseSync } = createRequire(import.meta.url)('node:sqlite');

const id = (prefix) => prefix + '_' + randomUUID();
const now = () => new Date().toISOString();
const scenario = 'scn_team_space_access';
const currentUser = (key) =>
  profiles.find((p) => p.user_id === key) ||
  problem(401, 'Выберите доступный тестовый профиль.');
const editable = (op) =>
  !op || ['awaiting_confirmation', 'failed'].includes(op.operation_status);
export function openService(filename) {
  if (filename !== ':memory:')
    mkdirSync(dirname(filename), { recursive: true });
  const db = new DatabaseSync(filename);
  db.exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
    CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY, user TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS tasks(user TEXT PRIMARY KEY, data TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS tickets(operation TEXT PRIMARY KEY, owner TEXT NOT NULL, data TEXT NOT NULL);
    CREATE TABLE IF NOT EXISTS events(id TEXT PRIMARY KEY, task TEXT NOT NULL, data TEXT NOT NULL);`);
  const sql = (query, ...args) => db.prepare(query).get(...args);
  const run = (query, ...args) => db.prepare(query).run(...args);
  function log(task, name, op, extra = {}) {
    const ev = {
      event_id: id('evt'),
      time: now(),
      event_name: name,
      task_instance_id: task.id,
      scenario_id: scenario,
      user_id: task.user,
      operation_id: op?.operation_id || null,
      version: '3',
      test_mode: true,
      ...extra,
    };
    run(
      'INSERT INTO events VALUES (?,?,?)',
      ev.event_id,
      task.id,
      JSON.stringify(ev),
    );
  }
  function newTask(user) {
    const task = {
      id: id('task'),
      user,
      messages: [],
      partial: null,
      op: null,
      handoff: null,
      feedback: null,
    };
    log(task, 'task_started');
    return task;
  }
  function ticketFor(op, user) {
    const row = sql(
      'SELECT data FROM tickets WHERE operation=? AND owner=?',
      op.operation_id,
      user,
    );
    return row ? JSON.parse(row.data) : null;
  }
  function snapshot(user, t) {
    const op = t.op,
      ticket = op?.ticket;
    const rulesRead = t.messages.some(
      (m) => m.source?.section === 'Порядок подачи',
    );
    const stage = ticket ? 2 : op || t.partial || rulesRead ? 1 : 0;
    const stepStatus =
      op?.operation_status === 'unknown' ||
      op?.operation_status === 'failed' ||
      ticket?.external_status === 'rejected'
        ? 'blocked'
        : ticket?.external_status === 'provisioned'
          ? 'done'
          : 'in_progress';
    const hints = {
      pending: 'Заявка у руководителя. Пока ждёте, изучите AvitoTech Playbook.',
      approved:
        'Руководитель согласовал заявку. Дождитесь технической выдачи прав.',
      rejected: 'Уточните причину отказа у руководителя или наставника.',
      provisioned:
        'Доступ выдан. Откройте материалы команды и продолжайте онбординг.',
    };
    const hint =
      hints[ticket?.external_status] ||
      (op?.operation_status === 'unknown'
        ? 'Проверьте исходную операцию перед повторной отправкой.'
        : op?.operation_status === 'failed'
          ? 'Сервис подтвердил, что заявки нет. Можно повторить отправку.'
          : op
            ? 'Проверьте параметры и подтвердите отправку.'
            : 'Узнайте правила или заполните заявку на доступ.');
    return {
      user,
      profiles: profiles.map(({ user_id, name, role }) => ({
        user_id,
        name,
        role,
      })),
      resources,
      scenario: {
        scenario_id: scenario,
        title: 'Доступ в пространство команды',
        task_instance_id: t.id,
        step_status: stepStatus,
        next_step_hint: hint,
        steps: [
          'Узнать правила',
          'Подготовить заявку',
          'Получить результат',
        ].map((title, i) => ({
          id: 'step_' + (i + 1),
          title,
          status:
            i < stage || stepStatus === 'done'
              ? 'done'
              : i === stage
                ? stepStatus
                : 'not_started',
        })),
      },
      active_operation: op,
      messages: t.messages,
      partial: t.partial,
      handoff: t.handoff,
      feedback: t.feedback,
      demo: true,
      previous_tickets: db
        .prepare(
          'SELECT data FROM tickets WHERE owner=? AND operation<>? ORDER BY rowid DESC',
        )
        .all(user.user_id, op?.operation_id || '')
        .map((row) => JSON.parse(row.data)),
      events: db
        .prepare(
          'SELECT data FROM events WHERE task=? ORDER BY rowid DESC LIMIT 20',
        )
        .all(t.id)
        .map((row) => JSON.parse(row.data)),
    };
  }
  function draft(user, t, fields) {
    if (!editable(t.op))
      problem(409, 'Отправленная операция защищена от изменений.');
    const payload = validate(user, fields),
      old = t.op;
    if (
      old &&
      JSON.stringify(old.payload) === JSON.stringify(payload) &&
      old.operation_status === 'awaiting_confirmation'
    )
      return old;
    t.op = {
      operation_id: old?.operation_id || id('op'),
      draft_version: (old?.draft_version || 0) + 1,
      operation_status: 'awaiting_confirmation',
      payload,
      ticket: null,
    };
    t.partial = null;
    log(t, 'action_proposed', t.op);
    return t.op;
  }
  function recordTicket(user, t, op) {
    const serial = sql(
      'SELECT COALESCE(MAX(rowid),0)+1 AS serial FROM tickets',
    ).serial;
    const ticket = {
      ...op.payload,
      ticket_id: 'REQ-' + String(serial).padStart(6, '0'),
      external_status: 'pending',
      created_at: now(),
    };
    run(
      'INSERT INTO tickets VALUES (?,?,?)',
      op.operation_id,
      user.user_id,
      JSON.stringify(ticket),
    );
    return ticket;
  }
  function success(t, op, ticket) {
    if (op.operation_status !== 'succeeded')
      log(t, 'action_succeeded', op, { result: 'saved' });
    op.operation_status = 'succeeded';
    op.ticket = ticket;
  }
  function dispatch(method, path, body, headers) {
    const token = /(?:^|;\s*)buddy_session=([\w-]+)/.exec(
      headers.cookie || '',
    )?.[1];
    const session =
      token && sql('SELECT user FROM sessions WHERE token=?', token);
    let user = currentUser(session?.user || 'usr_intern_01'),
      cookie;
    if (path === '/demo/session' && method === 'POST')
      user = currentUser(body.user_id);
    else if (!session && !(path === '/context' && method === 'GET'))
      problem(401, 'Сначала загрузите профиль.');
    else if (
      headers['x-test-user-id'] &&
      headers['x-test-user-id'] !== user.user_id
    )
      problem(403, 'Контекст профиля изменился. Обновите страницу.');
    if (!session || (path === '/demo/session' && method === 'POST')) {
      const fresh = randomUUID();
      if (session) run('DELETE FROM sessions WHERE token=?', token);
      run('INSERT INTO sessions VALUES (?,?)', fresh, user.user_id);
      cookie = 'buddy_session=' + fresh + '; Path=/; HttpOnly; SameSite=Strict';
    }
    const saved = sql('SELECT data FROM tasks WHERE user=?', user.user_id);
    let t = saved ? JSON.parse(saved.data) : newTask(user.user_id),
      status = 200,
      message,
      extras = {};
    const op = t.op;
    if (
      (method === 'GET' && path === '/context') ||
      (method === 'POST' && path === '/demo/session')
    ) {
    } else if (method !== 'POST') problem(404, 'Маршрут не найден.');
    else if (body.task_instance_id !== t.id)
      problem(409, 'Задача была изменена. Обновите данные.');
    else if (path === '/chat/messages') {
      const q = body.message;
      if (typeof q !== 'string' || !q.trim() || q.length > 1500)
        problem(422, 'Сообщение должно содержать от 1 до 1500 символов.');
      const result = answer(user, q, t.partial, !editable(op));
      t.messages.push(
        { id: id('msg'), role: 'user', text: q.trim() },
        {
          id: id('msg'),
          role: 'assistant',
          text: result.text,
          source: result.source,
          handoff: result.handoff,
        },
      );
      if (result.fields && editable(op)) {
        t.partial = result.fields;
        if (!result.propose) t.op = null;
      }
      if (result.propose) draft(user, t, result.fields);
      log(t, 'answer_delivered', t.op, {
        source_ids: result.source ? [result.source.id] : [],
      });
      extras = {
        reply: result.text,
        sources: result.source ? [result.source] : [],
        proposed_operation: result.propose ? t.op : null,
        handoff_suggested: !!result.handoff,
      };
    } else if (path === '/operations/draft') {
      if (body.draft_version !== (op?.draft_version || 0))
        problem(409, 'Черновик изменён. Загрузите актуальную версию.');
      draft(user, t, body);
    } else if (path === '/tasks/new') {
      if (!op?.ticket) problem(409, 'Сначала завершите текущую операцию.');
      t = newTask(user.user_id);
    } else if (path === '/handoff') {
      if (!t.handoff) {
        t.handoff = {
          id: 'HLP-' + randomUUID().slice(0, 8),
          mentor: user.mentor_handle,
          created_at: now(),
        };
        log(t, 'handoff_requested', op);
      }
    } else if (path === '/feedback') {
      if (!op?.ticket || typeof body.helpful !== 'boolean')
        problem(422, 'Оценка доступна после подачи заявки.');
      if (t.feedback === null) {
        t.feedback = body.helpful;
        log(t, 'task_feedback_submitted', op, { helpful: body.helpful });
      }
    } else {
      const m = path.match(
        /^\/operations\/([^/]+)\/(confirm|reconcile|simulate)$/,
      );
      if (!m || !op || m[1] !== op.operation_id)
        problem(404, 'Операция не найдена в вашем профиле.');
      if (m[2] === 'confirm') {
        if (
          headers['idempotency-key'] !==
            'idem_' + op.operation_id + '_v' + op.draft_version ||
          body.draft_version !== op.draft_version
        )
          problem(
            409,
            'Версия подтверждения устарела. Проверьте карточку ещё раз.',
          );
        validate(user, op.payload);
        if (op.operation_status === 'unknown') {
          status = 409;
          message = 'Результат неизвестен. Выполните сверку исходной операции.';
        } else if (op.operation_status !== 'succeeded') {
          const mode = body.simulate_mode || 'normal';
          if (!['normal', 'timeout', 'service_error'].includes(mode))
            problem(422, 'Неизвестный режим.');
          log(t, 'action_confirmed', op);
          if (mode === 'service_error') {
            op.operation_status = 'failed';
            status = 503;
            message = 'Сервис временно недоступен. Заявка не создана.';
            log(t, 'action_failed', op, { error_category: 'service_error' });
          } else {
            const ticket = recordTicket(user, t, op);
            if (mode === 'timeout') {
              op.operation_status = 'unknown';
              status = 504;
              message = 'Ответ потерян. Проверьте состояние этой операции.';
              log(t, 'action_status_unknown', op, {
                error_category: 'upstream_timeout',
              });
            } else success(t, op, ticket);
          }
        }
      } else if (m[2] === 'reconcile') {
        const ticket = ticketFor(op, user.user_id);
        if (ticket) success(t, op, ticket);
        else if (op.operation_status !== 'failed') {
          op.operation_status = 'failed';
          op.draft_version++;
          log(t, 'action_failed', op, { error_category: 'no_receipt' });
        }
      } else {
        const ticket = ticketFor(op, user.user_id);
        const allowed = {
          pending: ['approved', 'rejected'],
          approved: ['provisioned'],
        };
        if (
          op.operation_status !== 'succeeded' ||
          !ticket ||
          !allowed[ticket.external_status]?.includes(body.status)
        )
          problem(409, 'Такой переход сейчас недоступен.');
        ticket.external_status = body.status;
        op.ticket = ticket;
        run(
          'UPDATE tickets SET data=? WHERE operation=?',
          JSON.stringify(ticket),
          op.operation_id,
        );
      }
      Object.assign(extras, {
        operation_id: op.operation_id,
        operation_status: op.operation_status,
        ticket: op.ticket,
      });
    }
    run(
      'INSERT OR REPLACE INTO tasks VALUES (?,?)',
      user.user_id,
      JSON.stringify(t),
    );
    const ctx = snapshot(user, t);
    return {
      status,
      cookie,
      body: {
        ...ctx,
        ...extras,
        ...(message ? { message } : {}),
        next_step_hint: ctx.scenario.next_step_hint,
      },
    };
  }
  return {
    close: () => db.close(),
    request(method, path, body = {}, headers = {}) {
      db.exec('BEGIN IMMEDIATE');
      try {
        const result = dispatch(method, path, body, headers);
        db.exec('COMMIT');
        return result;
      } catch (error) {
        db.exec('ROLLBACK');
        if (!error.status) throw error;
        return { status: error.status, body: { message: error.message } };
      }
    },
  };
}
