import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
  within,
} from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../src/App';
import { openService } from '../server/service.mjs';
let db: ReturnType<typeof openService>,
  cookie = '',
  drop = false,
  offline = false;
beforeEach(() => {
  db = openService(':memory:');
  sessionStorage.clear();
  cookie = '';
  drop = false;
  offline = false;
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: string, options: RequestInit) => {
      if (offline) throw Error('offline');
      const h = Object.fromEntries(new Headers(options.headers));
      const route = input.replace('/api/v1', '');
      const res = db.request(
        options.method,
        route,
        JSON.parse(String(options.body || '{}')),
        { ...h, cookie },
      );
      if (res.cookie) cookie = res.cookie.split(';')[0];
      if (drop && route.endsWith('/confirm')) {
        drop = false;
        throw Error('response dropped');
      }
      return {
        ok: res.status < 400,
        status: res.status,
        json: async () => res.body,
      };
    }),
  );
});
afterEach(() => {
  cleanup();
  db.close();
  vi.unstubAllGlobals();
});
const btn = (name: string) => screen.getByRole('button', { name, exact: true });
const click = async (name: string) =>
  fireEvent.click(await screen.findByRole('button', { name, exact: true }));
const field = (name: string) => screen.getByLabelText(name, { exact: false });
async function start() {
  render(<App />);
  await screen.findByRole('heading', { name: 'Доступ в команду' });
}
async function draft() {
  await click('Заполнить вручную');
  await click('Проверить заявку');
  expect(
    await screen.findByText('Выберите ресурс.', { exact: true }),
  ).toBeTruthy();
  fireEvent.change(field('Ресурс'), {
    target: { value: 'repo_auto_core_sandbox' },
  });
  fireEvent.change(field('Уровень доступа'), { target: { value: 'read' } });
  fireEvent.change(field('Зачем нужен доступ'), {
    target: { value: 'Изучить учебные материалы' },
  });
  await click('Проверить заявку');
  await screen.findByText('К подтверждению');
}
async function ask(text: string) {
  fireEvent.change(field('Сообщение в чат'), { target: { value: text } });
  await click('Отправить сообщение');
  await waitFor(() =>
    expect((field('Сообщение в чат') as HTMLInputElement).value).toBe(''),
  );
}
describe('Рабочий экран стажёра', () => {
  it('предпросмотр, отказ сервиса, повтор, восстановление всей заявки, статусы и оценка', async () => {
    await start();
    await draft();
    await click('Изменить параметры');
    await click('Отменить изменения');
    fireEvent.change(field('Ответ сервиса'), {
      target: { value: 'service_error' },
    });
    await click('Подтвердить и отправить');
    await screen.findByRole('alert');
    fireEvent.change(field('Ответ сервиса'), { target: { value: 'normal' } });
    await click('Повторить отправку');
    const num = (await screen.findByLabelText('Номер заявки')).textContent;
    cleanup();
    await start();
    expect(screen.getByLabelText('Номер заявки').textContent).toBe(num);
    for (const text of [
      'Репозиторий Auto Core',
      'Только чтение',
      'Изучить учебные материалы',
      'Ожидает согласования',
    ])
      expect(screen.getByText(text)).toBeTruthy();
    await click('Да');
    await screen.findByText('Оценка сохранена');
    await click('Согласовать');
    await screen.findByText('Согласована');
    await click('Подтвердить выдачу');
    await screen.findByText('Доступ выдан');
    await click('Начать другую заявку');
    await screen.findByText('Подготовьте запрос');
  });
  it('не позволяет редактировать или повторить операцию при таймауте и потере сети', async () => {
    await start();
    await draft();
    fireEvent.change(field('Ответ сервиса'), { target: { value: 'timeout' } });
    await click('Подтвердить и отправить');
    await screen.findByText('Результат неизвестен');
    expect(
      screen.queryByRole('button', { name: 'Изменить параметры' }),
    ).toBeNull();
    await click('Проверить статус операции');
    await screen.findByText('Ожидает согласования');
    await click('Начать другую заявку');
    await draft();
    drop = true;
    await click('Подтвердить и отправить');
    await screen.findByText('Результат неизвестен');
    await click('Проверить статус операции');
    await screen.findByLabelText('Номер заявки');
  });
  it('показывает источник, корректно отказывает вне темы, сохраняет обращение и меняет профиль', async () => {
    await start();
    await click('Как запросить доступ?');
    await screen.findByText('Тестовая инструкция');
    await ask('Как оформить отгул?');
    expect(
      within(screen.getByRole('log')).getByText(/нет ответа/),
    ).toBeTruthy();
    await click('Обратиться к наставнику @ivan_petrov');
    await screen.findByText(/Обращение HLP-/);
    await ask('Мне нужен доступ');
    await screen.findByText(/К какому ресурсу/);
    await ask('База знаний');
    await ask('Чтение');
    await ask('Изучить архитектуру команды');
    await screen.findByText('К подтверждению');
    fireEvent.change(field('Тестовый профиль'), {
      target: { value: 'usr_intern_02' },
    });
    await screen.findByText('Goods Search', { exact: true });
    expect(screen.queryByText('К подтверждению')).toBeNull();
    expect(btn('Обратиться к наставнику @olga_sidorova')).toBeTruthy();
  });
  it('восстанавливается после ошибки первой загрузки', async () => {
    offline = true;
    render(<App />);
    await screen.findByText(/Не удалось загрузить профиль/);
    offline = false;
    await click('Загрузить снова');
    await screen.findByText('Подготовьте запрос');
  });
});
