import { createServer } from 'node:http';
import {
  apiMiddleware,
  makeApi,
  staticResponse,
} from './http.mjs';
const db = makeApi(),
  api = apiMiddleware(db);
const server = createServer((req, res) =>
  api(req, res, () => staticResponse(req, res)),
);
const port = Number(process.env.PORT || 5173),
  host = process.env.HOST || '127.0.0.1';
server.listen(port, host, () =>
  console.log('AI-buddy: http://' + host + ':' + server.address().port),
);
for (const signal of ['SIGTERM', 'SIGINT'])
  process.on(signal, () =>
    server.close(() => {
      db.close();
      process.exit(0);
    }),
  );

