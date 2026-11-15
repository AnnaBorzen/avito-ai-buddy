import type { Context, Operation } from '../model';
export function confirmationKey(op: Operation) {
  return 'idem_' + op.operation_id + '_v' + op.draft_version;
}
export function needsCheck(ctx: Context, unanswered: string | null) {
  const op = ctx.active_operation;
  return (
    !!op &&
    (op.operation_status === 'unknown' ||
      (op.operation_status === 'awaiting_confirmation' &&
        unanswered === op.operation_id))
  );
}
