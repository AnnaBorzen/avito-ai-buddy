export type Level = '' | 'read' | 'write';
export type Status =
  'awaiting_confirmation' | 'failed' | 'unknown' | 'succeeded';
export type TicketStatus = 'pending' | 'approved' | 'rejected' | 'provisioned';
export type Mode = 'normal' | 'timeout' | 'service_error';
export interface Fields {
  resource_id: string;
  access_level: Level;
  justification: string;
}
export interface Payload extends Fields {
  resource_title: string;
  approver_name: string;
  approver_id: string;
}
export interface Ticket extends Payload {
  ticket_id: string;
  created_at: string;
  external_status: TicketStatus;
}
export interface Operation {
  operation_id: string;
  draft_version: number;
  operation_status: Status;
  payload: Payload;
  ticket: Ticket | null;
}
interface Person {
  user_id: string;
  name: string;
  role: string;
}
export interface User extends Person {
  team_id: string;
  team_name: string;
  mentor_handle: string;
  approver_name: string;
}
export interface Message {
  id: string;
  role: 'assistant' | 'user';
  text: string;
  handoff?: boolean;
  source?: {
    id: string;
    title: string;
    section: string;
    excerpt: string;
    is_synthetic: boolean;
  };
}
export interface Context {
  user: User;
  profiles: Person[];
  resources: { id: string; title: string; owner: string; kind: string }[];
  scenario: {
    task_instance_id: string;
    step_status: string;
    next_step_hint: string;
    steps: { id: string; title: string; status: string }[];
  };
  active_operation: Operation | null;
  messages: Message[];
  partial: Fields | null;
  demo: boolean;
  previous_tickets: Ticket[];
  events: { event_id: string; event_name: string; time: string }[];
  handoff: { id: string; mentor: string } | null;
  feedback: boolean | null;
}
export const blank = (): Fields => ({
  resource_id: '',
  access_level: '',
  justification: '',
});
export const levels = {
  read: 'Только чтение',
  write: 'Редактирование',
  '': 'Выберите уровень',
};
export const ticketLabels: Record<TicketStatus, string> = {
  pending: 'Ожидает согласования',
  approved: 'Согласована',
  rejected: 'Отклонена',
  provisioned: 'Доступ выдан',
};
export function valid(f: Fields) {
  return !!(
    f.resource_id &&
    f.access_level &&
    f.justification.trim().length >= 10 &&
    f.justification.trim().length <= 300
  );
}
