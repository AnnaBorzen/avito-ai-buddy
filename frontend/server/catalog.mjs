export const profiles = [
  {
    user_id: 'usr_intern_01',
    name: 'Алексей Смирнов',
    role: 'intern',
    team_id: 'team_auto_backend',
    team_name: 'Auto Backend',
    mentor_handle: '@ivan_petrov',
    approver_name: 'Дмитрий Волков',
    approver_id: 'usr_lead_01',
  },
  {
    user_id: 'usr_intern_02',
    name: 'Мария Иванова',
    role: 'intern',
    team_id: 'team_goods_search',
    team_name: 'Goods Search',
    mentor_handle: '@olga_sidorova',
    approver_name: 'Сергей Кравцов',
    approver_id: 'usr_lead_02',
  },
  {
    user_id: 'usr_lead_01',
    name: 'Дмитрий Волков',
    role: 'lead',
    team_id: 'team_auto_backend',
    team_name: 'Auto Backend',
    mentor_handle: '@ivan_petrov',
    approver_name: 'Руководитель направления',
    approver_id: 'usr_director_01',
  },
];
export const resources = [
  {
    id: 'space_auto_backend_wiki',
    title: 'База знаний Auto Backend',
    owner: 'team_auto_backend',
    kind: 'wiki',
  },
  {
    id: 'repo_auto_core_sandbox',
    title: 'Репозиторий Auto Core',
    owner: 'team_auto_backend',
    kind: 'repo',
  },
  {
    id: 'space_goods_search_wiki',
    title: 'База знаний Goods Search',
    owner: 'team_goods_search',
    kind: 'wiki',
  },
  {
    id: 'db_fintech_prod_cluster',
    title: 'Промышленная БД FinTech',
    owner: 'team_fintech_core',
    kind: 'prod',
  },
];
export const emptyFields = () => ({
  resource_id: '',
  access_level: '',
  justification: '',
});
export function problem(status, message) {
  throw Object.assign(new Error(message), { status });
}
export function validate(user, fields) {
  const resource = resources.find((item) => item.id === fields.resource_id);
  if (
    !resource ||
    !['read', 'write'].includes(fields.access_level) ||
    typeof fields.justification !== 'string' ||
    fields.justification.trim().length < 10 ||
    fields.justification.trim().length > 300
  ) {
    problem(
      422,
      'Выберите ресурс, уровень доступа и укажите причину длиной от 10 до 300 символов.',
    );
  }
  if (
    user.role === 'intern' &&
    (resource.kind === 'prod' ||
      (resource.owner !== user.team_id &&
        (resource.kind !== 'wiki' || fields.access_level !== 'read')))
  ) {
    problem(
      403,
      'У вашей роли нет таких прав. Для соседней команды доступно только чтение её базы знаний. По рабочей задаче обратитесь к наставнику.',
    );
  }
  if (
    resource.id === 'space_goods_search_wiki' &&
    fields.access_level !== 'read'
  )
    problem(403, 'Для этого пространства доступно только чтение.');
  return {
    resource_id: resource.id,
    resource_title: resource.title,
    access_level: fields.access_level,
    justification: fields.justification.trim(),
    approver_name: user.approver_name,
    approver_id: user.approver_id,
  };
}
