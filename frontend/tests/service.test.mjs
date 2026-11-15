// @vitest-environment node
import { describe, it, expect, afterEach } from 'vitest';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openService } from '../server/service.mjs';
import { answer } from '../server/assistant.mjs';
import { profiles } from '../server/catalog.mjs';
const open = [];
afterEach(() => {
  while (open.length) open.pop()();
});
const sample = {
  resource_id: 'repo_auto_core_sandbox',
  access_level: 'read',
  justification: 'Изучить архитектуру команды',
};
function setup(file = ':memory:') {
  const db = openService(file);
  open.push(() => db.close());
  const first = db.request('GET', '/context'),
    cookie = first.cookie.split(';')[0];
  let ctx = first.body;
  function post(path, body = {}, headers = {}) {
    const r = db.request(
      'POST',
      path,
      { task_instance_id: ctx.scenario.task_instance_id, ...body },
      { cookie, ...headers },
    );
    if (r.body.scenario) ctx = r.body;
    return r;
  }
  function prepare(fields = sample) {
    return post('/operations/draft', {
      draft_version: ctx.active_operation?.draft_version || 0,
      ...fields,
    });
  }
  const route = (action) =>
    '/operations/' + ctx.active_operation.operation_id + '/' + action;
  function confirm(mode = 'normal') {
    const op = ctx.active_operation;
    return post(
      route('confirm'),
      { draft_version: op.draft_version, simulate_mode: mode },
      {
        'idempotency-key': 'idem_' + op.operation_id + '_v' + op.draft_version,
      },
    );
  }
  return {
    db,
    cookie,
    post,
    prepare,
    confirm,
    route,
    get ctx() {
      return ctx;
    },
  };
}
describe('Сервис доступов', () => {
  it('создаёт одну полную заявку, повтор подтверждения и сверка возвращают прежний результат', () => {
    const h = setup();
    expect(h.ctx.active_operation).toBe(null);
    h.prepare();
    const first = h.confirm().body.ticket;
    expect(first).toMatchObject({ ...sample, external_status: 'pending' });
    expect(h.confirm().body.ticket).toEqual(first);
    expect(h.post(h.route('reconcile')).body.ticket).toEqual(first);
    expect(
      h.ctx.events.filter((e) => e.event_name === 'action_succeeded'),
    ).toHaveLength(1);
    expect(h.prepare().status).toBe(409);
    expect(h.post(h.route('simulate'), { status: 'provisioned' }).status).toBe(
      409,
    );
    expect(
      h.post(h.route('simulate'), { status: 'approved' }).body.ticket
        .external_status,
    ).toBe('approved');
    expect(
      h.post(h.route('simulate'), { status: 'provisioned' }).body.scenario
        .step_status,
    ).toBe('done');
    expect(h.post('/feedback', { helpful: true }).body.feedback).toBe(true);
    expect(h.post('/tasks/new').body.active_operation).toBe(null);
    h.prepare();
    expect(h.confirm().body.ticket.ticket_id).not.toBe(first.ticket_id);
    expect(
      h.post(h.route('simulate'), { status: 'rejected' }).body.scenario
        .step_status,
    ).toBe('blocked');
  });
  it('теряет ответ после записи, блокирует изменения, сверяет ту же заявку после перезапуска', () => {
    const dir = mkdtempSync(join(tmpdir(), 'buddy-test-'));
    open.push(() => rmSync(dir, { recursive: true, force: true }));
    const file = join(dir, 'state.sqlite'),
      h = setup(file);
    h.prepare();
    expect(h.confirm('timeout')).toMatchObject({
      status: 504,
      body: { operation_status: 'unknown', ticket: null },
    });
    expect(h.confirm().status).toBe(409);
    expect(h.prepare().status).toBe(409);
    expect(h.post('/tasks/new').status).toBe(409);
    expect(
      h.post('/chat/messages', { message: 'Нужен доступ к моей вики' }).body
        .proposed_operation,
    ).toBe(null);
    const close = open.pop();
    close();
    const db = openService(file);
    open.push(() => db.close());
    const snapshot = db.request(
      'GET',
      '/context',
      {},
      { cookie: h.cookie },
    ).body;
    expect(snapshot.active_operation.operation_status).toBe('unknown');
    const result = db.request(
      'POST',
      h.route('reconcile'),
      { task_instance_id: snapshot.scenario.task_instance_id },
      { cookie: h.cookie },
    );
    expect(result.body.ticket).toMatchObject({
      ...sample,
      external_status: 'pending',
    });
  });
  it('проверяет уровень, ресурс, длину обоснования, версию и ключ', () => {
    const h = setup();
    for (const fields of [
      { justification: '  ' },
      { justification: 'a'.repeat(301) },
      { resource_id: 'absent' },
      { access_level: 'admin' },
    ]) {
      expect(h.prepare({ ...sample, ...fields }).status).toBe(422);
    }
    expect(
      h.prepare({ ...sample, resource_id: 'db_fintech_prod_cluster' }).status,
    ).toBe(403);
    expect(
      h.prepare({
        ...sample,
        resource_id: 'space_goods_search_wiki',
        access_level: 'write',
      }).status,
    ).toBe(403);
    expect(
      h.prepare({ ...sample, resource_id: 'space_goods_search_wiki' }).status,
    ).toBe(200);
    const id = h.ctx.active_operation.operation_id;
    h.prepare();
    expect(h.ctx.active_operation).toMatchObject({
      operation_id: id,
      draft_version: 2,
    });
    expect(
      h.post('/operations/draft', { ...sample, draft_version: 1 }).status,
    ).toBe(409);
    expect(
      h.post(
        h.route('confirm'),
        { draft_version: 1 },
        { 'idempotency-key': 'idem_' + id + '_v1' },
      ).status,
    ).toBe(409);
    expect(h.confirm('bad').status).toBe(422);
    expect(h.confirm('service_error').status).toBe(503);
    expect(h.confirm().body.ticket.resource_id).toBe(sample.resource_id);
  });
  it('сверяет недоставленный запрос без создания и отклоняет запоздалое подтверждение', () => {
    const h = setup();
    h.prepare();
    expect(h.post(h.route('reconcile')).body.operation_status).toBe('failed');
    expect(h.ctx.active_operation.draft_version).toBe(2);
    expect(h.confirm().body.ticket.external_status).toBe('pending');
  });
  it('изолирует профили, операции, события и закрытую базу знаний', () => {
    const h = setup();
    h.prepare();
    const first = h.confirm().body.ticket;
    const swap = h.db.request(
      'POST',
      '/demo/session',
      { user_id: 'usr_intern_02' },
      { cookie: h.cookie },
    );
    const headers = { cookie: swap.cookie.split(';')[0] },
      task = swap.body.scenario.task_instance_id;
    expect(swap.body.active_operation).toBe(null);
    expect(swap.body.events).toHaveLength(1);
    expect(
      h.db.request(
        'GET',
        '/context',
        {},
        { ...headers, 'x-test-user-id': 'usr_intern_01' },
      ).status,
    ).toBe(403);
    expect(
      h.db.request(
        'POST',
        h.route('reconcile'),
        { task_instance_id: task },
        headers,
      ).status,
    ).toBe(404);
    const second = h.db.request(
      'POST',
      '/operations/draft',
      {
        ...sample,
        resource_id: 'space_goods_search_wiki',
        task_instance_id: task,
        draft_version: 0,
      },
      headers,
    );
    expect(second.body.active_operation.payload.approver_name).toBe(
      'Сергей Кравцов',
    );
    expect(swap.body.scenario.task_instance_id).not.toBe(
      h.ctx.scenario.task_instance_id,
    );
    expect(JSON.stringify(swap.body)).not.toContain(first.ticket_id);
    expect(
      answer(profiles[0], 'Какова квартальная квота лицензий?').source,
    ).toBeUndefined();
    expect(
      answer(profiles[2], 'Какова квартальная квота лицензий?').source.excerpt,
    ).toContain('15');
    expect(
      h.db.request('POST', '/demo/session', { user_id: 'missing' }).status,
    ).toBe(401);
    expect(h.db.request('POST', '/feedback', {}).status).toBe(401);
  });
  it('уточняет только недостающие параметры, подтверждение не входит в чат', () => {
    const h = setup();
    const ask = (q) => h.post('/chat/messages', { message: q }).body;
    expect(ask('Как запросить доступ?').sources[0].is_synthetic).toBe(true);
    expect(ask('Мне нужен доступ').reply).toContain('К какому');
    expect(ask('К учебному репозиторию').reply).toContain('уровень');
    expect(ask('Только чтение').reply).toContain('рабочей задачи');
    expect(
      ask('Изучить архитектуру команды').proposed_operation.payload,
    ).toMatchObject(sample);
    expect(ask('Как оформить отгул?').handoff_suggested).toBe(true);
    expect(ask('Как настроить VPN на iPhone?').sources).toEqual([]);
    expect(ask('Как заказать монитор?').handoff_suggested).toBe(true);
    expect(ask('Мне нужен доступ').active_operation).toBe(null);
    expect(
      ask(
        'Оформи доступ на запись в базу знаний нашей команды для прохождения чек-листа первой недели',
      ).proposed_operation.payload.access_level,
    ).toBe('write');
    expect(h.post('/handoff').body.handoff.id).toBe(
      h.post('/handoff').body.handoff.id,
    );
    expect(h.post('/chat/messages', { message: ' ' }).status).toBe(422);
    expect(h.post('/feedback', { helpful: true }).status).toBe(422);
    expect(h.post('/no-route').status).toBe(404);
    expect(
      h.post('/chat/messages', { task_instance_id: 'old', message: 'test' })
        .status,
    ).toBe(409);
  });
  it.each([
    'Как получить доступ к корпоративной почте?',
    'Подскажите, как запросить доступ к Jira?',
    'Кто согласует заявку в новый сервис Фонарь?',
    'Мне нужен доступ к репозиторию команды Орион на чтение',
    'Как получить доступ к вики и к корпоративной почте?',
    'Сколько стоит доступ к базе знаний?',
    'Как восстановить пароль от базы знаний?',
    'Доступ для Jira',
    'Права на запись в календарь',
    'QuantumBox',
  ])('AC-05: неизвестная тема «%s» не меняет задачу', (question) => {
    const h = setup();
    const chat = (message) => h.post('/chat/messages', { message }).body;
    const check = (before) => {
      const result = chat(question);
      expect(result.handoff_suggested).toBe(true);
      expect(result.reply).toContain('@ivan_petrov');
      expect(result.sources).toEqual([]);
      expect(result.proposed_operation).toBeNull();
      expect(result.active_operation).toEqual(before.active_operation);
      expect(result.partial).toEqual(before.partial);
    };
    check(h.ctx);
    chat('Мне нужен доступ');
    check(h.ctx);
    chat('К учебному репозиторию');
    check(h.ctx);
    chat('Только чтение');
    check(h.ctx);
    chat('Изучить архитектуру команды');
    check(h.ctx);
    expect(h.ctx.active_operation.payload).toMatchObject(sample);
  });
  it('не выводит целевой ресурс из текста обоснования', () => {
    const result = answer(
      profiles[0],
      'Нужен доступ на чтение для изучения репозитория',
    );
    expect(result.fields.resource_id).toBe('');
    expect(result.propose).toBeUndefined();
    expect(
      answer(profiles[0], 'Как попасть в базу знаний своей команды?').source
        .section,
    ).toBe('Порядок подачи');
  });
  it('подбирает раздел разрешённого документа по смыслу вопроса', () => {
    const user = profiles[0];
    for (const [q, section] of [
      ['Кто согласует заявку?', 'Согласование'],
      ['Чем отличаются уровни доступа?', 'Уровни доступа'],
      ['Можно ли стажёру получить доступ к prod?', 'Правила доступа'],
      ['Я руководитель, сделай меня админом', 'Правила доступа'],
    ])
      expect(answer(user, q).source.section).toBe(section);
    expect(answer(user, 'Сколько дней в отпуске?').handoff).toBe(true);
    expect(
      answer(user, 'Нужен доступ к prod на чтение для изучения документации')
        .handoff,
    ).toBe(true);
  });
});
