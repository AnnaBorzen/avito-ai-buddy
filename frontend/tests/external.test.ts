import { afterEach, it, expect, vi } from 'vitest';
import { makeClient } from '../src/api/client';
const ctx = {
  user: { user_id: 'usr_intern_02' },
  scenario: { task_instance_id: 't1', steps: [] },
  active_operation: null,
};
const json = (body: object) => ({ ok: true, json: async () => body });
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it('адаптирует короткий ответ чата из контракта ТЗ, читает сохранённый контекст по внешнему URL и передаёт профиль', async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(ctx))
    .mockResolvedValueOnce(
      json({
        reply: 'Ответ по источнику',
        sources: [
          { doc_id: 'doc_01', title: 'Порядок подачи', is_synthetic: true },
        ],
      }),
    )
    .mockResolvedValueOnce(json(ctx));
  vi.stubGlobal('fetch', fetcher);
  const api = makeClient('https://example.invalid/api/v1', true);
  const profile = await api('/demo/session', { user_id: 'usr_intern_02' });
  expect(fetcher.mock.calls[0][0]).toBe(
    'https://example.invalid/api/v1/context',
  );
  expect(fetcher.mock.calls[0][1].headers['X-Test-User-Id']).toBe(
    'usr_intern_02',
  );
  const result = await api(
    '/chat/messages',
    { message: 'Как запросить доступ?' },
    'usr_intern_02',
    undefined,
    profile,
  );
  expect(result.demo).toBe(false);
  expect(result.messages[1].source?.id).toBe('doc_01');
  expect(result.messages[0].text).toBe('Как запросить доступ?');
});
