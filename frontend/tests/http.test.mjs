// @vitest-environment node
import { it, expect } from 'vitest';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { apiMiddleware, staticResponse } from '../server/http.mjs';
import { openService } from '../server/service.mjs';
it('отдаёт контекст и cookie по HTTP, обрабатывает некорректные запросы и изолирует статические файлы', async () => {
  const db = openService(':memory:'),
    api = apiMiddleware(db);
  const server = createServer((req, res) =>
    api(req, res, () => staticResponse(req, res)),
  );
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const base = 'http://127.0.0.1:' + server.address().port;
  try {
    const first = await fetch(base + '/api/v1/context'),
      ctx = await first.json();
    const cookie = first.headers.get('set-cookie').split(';')[0];
    expect(ctx.user.user_id).toBe('usr_intern_01');
    const post = (path, body, extra = {}) =>
      fetch(base + '/api/v1/' + path, {
        method: 'POST',
        headers: { cookie, 'Content-Type': 'application/json', ...extra },
        body,
      });
    const msg = await post(
      'chat/messages',
      JSON.stringify({
        message: 'Как запросить доступ?',
        task_instance_id: ctx.scenario.task_instance_id,
      }),
    );
    expect((await msg.json()).sources).toHaveLength(1);
    expect((await post('demo/session', '{')).status).toBe(400);
    expect((await post('demo/session', '[]')).status).toBe(400);
    expect((await post('demo/session', 'x'.repeat(17000))).status).toBe(413);
    expect(
      (await post('demo/session', '{}', { origin: 'https://example.invalid' }))
        .status,
    ).toBe(403);
    expect((await fetch(base + '/server/service.mjs')).status).toBe(404);
    expect((await fetch(base + '/assets/not-present.js')).status).toBe(404);
  } finally {
    await new Promise((resolve) => server.close(resolve));
    db.close();
  }
});
