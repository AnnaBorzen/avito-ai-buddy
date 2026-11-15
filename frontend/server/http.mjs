import { readFileSync } from 'node:fs';
import { join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { openService } from './service.mjs';
export const root = fileURLToPath(new URL('../', import.meta.url));
export function apiMiddleware(store) {
  return async (req, res, next) => {
    const path = new URL(req.url, 'http://localhost').pathname;
    if (!path.startsWith('/api/v1/')) return next();
    res.setHeader('Content-Type', 'application/json; charset=utf-8');
    res.setHeader('Cache-Control', 'no-store');
    function send(status, body) {
      res.statusCode = status;
      res.end(JSON.stringify(body));
    }
    try {
      if (
        req.method === 'POST' &&
        req.headers.origin &&
        new URL(req.headers.origin).host !== req.headers.host
      )
        return send(403, {
          message: 'Запрос разрешён только из этого приложения.',
        });
      const chunks = [];
      let byteCount = 0;
      for await (const chunk of req) {
        byteCount += chunk.length;
        if (byteCount > 16000)
          return send(413, { message: 'Слишком большой запрос.' });
        chunks.push(chunk);
      }
      let body;
      try {
        body = JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}');
      } catch {
        return send(400, { message: 'Некорректный JSON.' });
      }
      if (!body || typeof body !== 'object' || Array.isArray(body))
        return send(400, { message: 'Ожидался объект JSON.' });
      const result = store.request(
        req.method,
        path.slice(7),
        body,
        req.headers,
      );
      if (result.cookie) res.setHeader('Set-Cookie', result.cookie);
      send(result.status, result.body);
    } catch {
      send(500, {
        message:
          'Не удалось прочитать или сохранить данные. Проверьте состояние операции.',
      });
    }
  };
}
export function makeApi() {
  return openService(
    process.env.BUDDY_DB || join(root, 'data', 'buddy.sqlite'),
  );
}
export function staticResponse(req, res) {
  const url = new URL(req.url, 'http://localhost');
  const file = url.pathname === '/' ? 'index.html' : url.pathname.slice(1);
  if (
    req.method !== 'GET' ||
    !/^(?:index\.html|buddy\.svg|assets\/[\w.-]+\.(?:css|js|woff2))$/.test(file)
  ) {
    res.writeHead(404);
    res.end();
    return;
  }
  try {
    const data = readFileSync(join(root, 'dist', file));
    const mime = {
      '.html': 'text/html; charset=utf-8',
      '.js': 'application/javascript',
      '.css': 'text/css',
      '.svg': 'image/svg+xml',
      '.woff2': 'font/woff2',
    };
    res.writeHead(200, {
      'Content-Type': mime[extname(file)],
      'X-Content-Type-Options': 'nosniff',
      'Cache-Control': file.startsWith('assets/')
        ? 'public,max-age=31536000,immutable'
        : 'no-cache',
    });
    res.end(data);
  } catch {
    res.writeHead(404);
    res.end('Build frontend first.');
  }
}
