import { useEffect, useRef, useState } from 'react';
import {
  ArrowUp,
  BookOpen,
  ChevronRight,
  MessageCircle,
  MessagesSquare,
  Sparkles,
  UserRound,
} from 'lucide-react';
import type { Context } from './model';
interface Props {
  ctx: Context;
  busy: string;
  send: (text: string) => Promise<boolean>;
  handoff: () => void;
}
export function Chat({ ctx, busy, send, handoff }: Props) {
  const [text, setText] = useState(''),
    log = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight;
  }, [ctx.messages.length, busy]);
  async function submit(value: string) {
    if (value.trim() && !busy && (await send(value.trim())))
      setText((current) => (current.trim() === value.trim() ? '' : current));
  }
  return (
    <section className="panel chat" aria-labelledby="dialog-title">
      <div className="panelhead">
        <div className="iconbox purple">
          <MessagesSquare />
        </div>
        <div>
          <h2 id="dialog-title">Диалог с AI-buddy</h2>
          <span className="sub">Помощник по адаптации</span>
        </div>
      </div>
      <div
        className="conversation"
        ref={log}
        role="log"
        aria-label="Сообщения"
        aria-live="polite"
      >
        {!ctx.messages.length && (
          <div className="greeting">
            <span className="speaker">
              <Sparkles size={15} /> AI-buddy
            </span>
            <p>
              Привет, {ctx.user.name.split(' ')[0]}! Для знакомства с командой{' '}
              {ctx.user.team_name} пригодится её база знаний. Помогу найти
              инструкцию и оформить доступ.
            </p>
          </div>
        )}
        {ctx.messages.map((m) => (
          <article key={m.id} className={'bubble ' + m.role}>
            <span className="speaker">
              {m.role === 'user' ? (
                <UserRound size={14} />
              ) : (
                <Sparkles size={14} />
              )}{' '}
              {m.role === 'user' ? 'Вы' : 'AI-buddy'}
            </span>
            <p>{m.text}</p>
            {m.source && (
              <details className="source">
                <summary>
                  <BookOpen size={14} />
                  Тестовая инструкция <ChevronRight size={14} />
                </summary>
                <strong>
                  {m.source.title} · {m.source.section}
                </strong>
                <p>{m.source.excerpt}</p>
              </details>
            )}
          </article>
        ))}
        {busy === 'chat' && (
          <p role="status" className="sub">
            <span className="pulse" /> Ищу ответ в инструкциях…
          </p>
        )}
      </div>
      <div className="compose">
        {!ctx.messages.length && (
          <div className="suggestions">
            {['Как запросить доступ?', 'Мне нужен доступ'].map((s) => (
              <button
                key={s}
                className="chip"
                disabled={!!busy}
                onClick={() => submit(s)}
              >
                {s}
                <ChevronRight size={13} />
              </button>
            ))}
          </div>
        )}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void submit(text);
          }}
        >
          <label className="sr" htmlFor="chat-text">
            Сообщение в чат
          </label>
          <input
            id="chat-text"
            maxLength={1500}
            placeholder="Напишите вопрос…"
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <button
            disabled={!text.trim() || !!busy}
            aria-label="Отправить сообщение"
            className="send"
          >
            <ArrowUp size={19} />
          </button>
        </form>
        {ctx.active_operation && <a className="jump" href="#request">Перейти к заявке ↓</a>}
        <div className="mentor">
          <MessageCircle size={15} />
          {ctx.handoff ? (
            <span role="status">
              Обращение {ctx.handoff.id} сохранено для {ctx.handoff.mentor} в
              демосервисе.
            </span>
          ) : (
            <button disabled={!!busy} onClick={handoff}>
              Обратиться к наставнику <span>{ctx.user.mentor_handle}</span>
            </button>
          )}
        </div>
      </div>
    </section>
  );
}
