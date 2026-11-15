import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { emptyFields, resources, validate } from './catalog.mjs';
import { classifyScope, requestSubject } from './scope.mjs';
const folder = join(dirname(fileURLToPath(import.meta.url)), '../../rag_docs');
const documents = readdirSync(folder)
  .filter((name) => name.endsWith('.md'))
  .map((name) => {
    const [, header, body] = readFileSync(join(folder, name), 'utf8').split(
      /^---\s*$/m,
    );
    const attr = (key) =>
      header.match(new RegExp('^' + key + ': (.+)$', 'm'))[1].trim();
    const sections = [...body.matchAll(/^## (.+)\r?\n([^#]+)/gm)].map(
      ([, title, text]) => ({ title, text: text.trim() }),
    );
    return {
      id: attr('doc_id'),
      title: attr('title'),
      roles: JSON.parse(attr('allowed_roles')),
      teams: JSON.parse(attr('allowed_teams')),
      sections,
    };
  });
function available(user) {
  return documents.filter(
    (d) =>
      d.roles.includes(user.role) &&
      (d.teams.includes('*') || d.teams.includes(user.team_id)),
  );
}
function grounded(docs, heading, prefix = '') {
  for (const doc of docs) {
    const section = doc.sections.find((s) => s.title === heading);
    if (section)
      return {
        text: prefix + section.text,
        source: {
          id: doc.id,
          title: doc.title,
          section: heading,
          excerpt: section.text,
          is_synthetic: true,
        },
      };
  }
  return null;
}
export function answer(user, message, previous = null, locked = false) {
  const docs = available(user),
    q = message.trim(),
    normalized = q.toLowerCase().replaceAll('ё', 'е');
  const fallback = {
    text:
      'В доступных тестовых инструкциях нет ответа на этот вопрос. Можно передать его наставнику ' +
      user.mentor_handle +
      '.',
    handoff: true,
  };
  const scope = classifyScope(normalized, previous);
  if (scope === 'unknown') return fallback;
  const s = scope === 'reason' ? '' : requestSubject(normalized);
  if (/квот|лицензи|закрыт.{0,12}чек/.test(s))
    return grounded(docs, 'Квоты руководителя') || fallback;
  if (/я (?:тимлид|руководитель)|сделай меня|игнорируй|права админ/.test(s))
    return grounded(docs, 'Правила доступа');
  if (
    /сколько|как долго|когда|кто соглас|кто (?:мой )?руководител|пока жду|срок/.test(
      s,
    )
  ) {
    if (/соглас|доступ|заявк|жду|срок рассмотр|руководител/.test(s))
      return grounded(
        docs,
        'Согласование',
        'В вашей команде согласующий — ' + user.approver_name + '. ',
      );
    return fallback;
  }
  if (/какие права|разниц|чем отлич|уровни доступа/.test(s))
    return grounded(docs, 'Уровни доступа');
  if (
    /можно ли|ограничени|правила безопасност/.test(s) &&
    /баз|доступ|прав|prod|промышлен/.test(s)
  )
    return grounded(docs, 'Правила доступа');
  if (
    /^(как|каков|где|что нужно|расскажи|подскажи).*(получ|запрос|оформ|правил|подать).*(доступ|заявк)|как (?:попасть|зайти|открыть)|порядок (?:запроса|подачи)|правила (?:запроса|доступа)/.test(
      s,
    )
  )
    return grounded(
      docs,
      'Порядок подачи',
      'Ваше пространство — ' + user.team_name + '. ',
    );
  const fields = { ...(previous || emptyFields()) };
  const resource =
    resources.find((r) => s.includes(r.id)) ||
    (/fintech|финтех|промышленн|продакш|prod\b/.test(s)
      ? resources[3]
      : /репозитор|песочниц|auto core|git\b/.test(s)
        ? resources[1]
        : /goods search|поиск[а]? товаров/.test(s)
          ? resources[2]
          : /auto backend|авто бэкенд/.test(s)
            ? resources[0]
            : /пространств|вики|wiki|баз[ауеы] знаний|документаци[юя]/.test(s)
              ? resources.find(
                  (r) => r.owner === user.team_id && r.kind === 'wiki',
                )
              : undefined);
  const level = /редактир|на запись|запис[ьи]|\bwrite\b/.test(s)
    ? 'write'
    : /чтени|читать|\bread\b/.test(s)
      ? 'read'
      : '';
  const reason = q
    .match(
      /(?:\s|^)(?:чтобы|для|причина\s*[:—]|обоснование\s*[:—])\s+(.+)/i,
    )?.[1]
    ?.trim();
  const followup =
    previous &&
    !q.includes('?') &&
    !/^(как|почему|где|кто|когда|что за|каки)/i.test(s);
  const intent = !!(
    resource ||
    level ||
    /доступ|подать заявку|оформить заявку/.test(s) ||
    followup
  );
  if (!intent) return fallback;
  if (locked)
    return {
      text: 'У вас уже есть отправленная операция. Её результат и дальнейшие действия указаны в карточке.',
    };
  if (scope === 'reason') fields.justification = reason || q;
  else {
    if (resource) fields.resource_id = resource.id;
    if (level) fields.access_level = level;
    if (reason) fields.justification = reason;
  }
  const missing = !fields.resource_id
    ? 'К какому ресурсу нужен доступ? Например, к базе знаний своей команды или к учебному репозиторию.'
    : !fields.access_level
      ? 'Какой уровень доступа нужен — чтение или редактирование?'
      : fields.justification.length < 10 || fields.justification.length > 300
        ? 'Для какой рабочей задачи нужен доступ? Укажите причину длиной от 10 до 300 символов.'
        : '';
  if (missing) return { text: missing, fields };
  try {
    validate(user, fields);
  } catch (error) {
    return {
      ...grounded(docs, 'Правила доступа'),
      text: error.message,
      handoff: true,
      fields,
    };
  }
  return {
    ...grounded(docs, 'Порядок подачи'),
    text: 'Черновик готов. Проверьте ресурс, уровень доступа и причину в карточке, затем подтвердите отправку.',
    fields,
    propose: true,
  };
}
