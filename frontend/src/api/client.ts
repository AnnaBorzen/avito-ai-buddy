import type { Context, Message, Operation } from '../model';
const external = import.meta.env.VITE_USE_MOCK_API === 'false';
const base = (
  external ? import.meta.env.VITE_API_BASE_URL || '/api/v1' : '/api/v1'
).replace(/\/$/, '');
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public context?: Context,
  ) {
    super(message);
  }
}
type Reply = Partial<Context> & {
  message?: string;
  reply?: string;
  sources?: {
    doc_id: string;
    title: string;
    section?: string;
    excerpt?: string;
    is_synthetic: boolean;
  }[];
  handoff_suggested?: boolean;
  operation_status?: Operation['operation_status'];
};
type OperationBody = { user_id?: string; message?: string };
const demoProfiles = [
  { user_id: 'usr_intern_01', name: 'Алексей Смирнов', role: 'intern' },
  { user_id: 'usr_intern_02', name: 'Мария Иванова', role: 'intern' },
  { user_id: 'usr_lead_01', name: 'Дмитрий Волков', role: 'lead' },
];
const demoResources = [
  [
    'space_auto_backend_wiki',
    'База знаний Auto Backend',
    'team_auto_backend',
    'wiki',
  ],
  [
    'repo_auto_core_sandbox',
    'Репозиторий Auto Core',
    'team_auto_backend',
    'repo',
  ],
  [
    'space_goods_search_wiki',
    'База знаний Goods Search',
    'team_goods_search',
    'wiki',
  ],
  [
    'db_fintech_prod_cluster',
    'Промышленная БД FinTech',
    'team_fintech_core',
    'prod',
  ],
].map(([id, title, owner, kind]) => ({ id, title, owner, kind }));
function normalize(raw: Reply, previous?: Context): Context {
  if (!raw.scenario || !raw.user || !Array.isArray(raw.scenario.steps))
    throw Error('Сервис вернул неполный контекст.');
  const same =
    previous?.user.user_id === raw.user.user_id &&
    previous?.scenario.task_instance_id === raw.scenario.task_instance_id;
  return {
    user: raw.user,
    scenario: raw.scenario,
    active_operation: raw.active_operation || null,
    messages: raw.messages || (same ? previous!.messages : []),
    events: raw.events || [],
    partial: raw.partial || null,
    feedback: raw.feedback ?? null,
    handoff: raw.handoff || null,
    demo: raw.demo === true,
    profiles: raw.profiles || demoProfiles,
    resources: raw.resources || demoResources,
    previous_tickets: raw.previous_tickets || [],
  };
}
async function call(
  baseUrl: string,
  path: string,
  body: object | undefined,
  user?: string,
  key?: string,
): Promise<Reply> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body) headers['Content-Type'] = 'application/json';
  if (user) headers['X-Test-User-Id'] = user;
  if (key) headers['Idempotency-Key'] = key;
  const ctrl = new AbortController(),
    timeout = setTimeout(() => ctrl.abort(), 12000);
  try {
    const response = await fetch(baseUrl + path, {
      method: body ? 'POST' : 'GET',
      credentials: 'include',
      headers,
      body: body ? JSON.stringify(body) : undefined,
      signal: ctrl.signal,
    });
    const data = await response.json();
    if (!response.ok)
      throw new ApiError(
        response.status,
        data.message || 'Сервис не смог выполнить запрос.',
        data.scenario ? normalize(data) : undefined,
      );
    return data;
  } finally {
    clearTimeout(timeout);
  }
}
export function makeClient(baseUrl: string, remote: boolean) {
  return async function request(
    path: string,
    body?: object,
    user?: string,
    key?: string,
    previous?: Context,
  ): Promise<Context> {
    if (remote && !previous?.demo && path === '/demo/session') {
      user = (body as OperationBody).user_id;
      path = '/context';
      body = undefined;
    }
    if (remote && path === '/context' && !user) {
      try {
        user =
          sessionStorage.getItem('buddy.remote.profile') || 'usr_intern_01';
      } catch {
        user = 'usr_intern_01';
      }
    }
    const reply = await call(baseUrl, path, body, user, key);
    const raw =
      reply.user && reply.scenario
        ? reply
        : await call(baseUrl, '/context', undefined, user);
    const current = normalize(raw, previous);
    if (remote && path === '/context')
      try {
        sessionStorage.setItem('buddy.remote.profile', current.user.user_id);
      } catch {}
    if (!raw.messages && reply.reply && path === '/chat/messages') {
      const source = reply.sources?.find((s) => s.is_synthetic);
      const assistant: Message = {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: reply.reply,
        handoff: reply.handoff_suggested,
      };
      if (source)
        assistant.source = {
          id: source.doc_id,
          title: source.title,
          section: source.section || 'Источник ответа',
          excerpt: source.excerpt || source.title,
          is_synthetic: true,
        };
      current.messages = [
        ...current.messages,
        {
          id: crypto.randomUUID(),
          role: 'user',
          text: (body as OperationBody).message || '',
        },
        assistant,
      ];
    }
    return current;
  };
}
export const request = makeClient(base, external);
