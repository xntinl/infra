import express from 'express';

const app = express();
const port = process.env.USERS_PORT || 3001;

app.use((_req, res, next) => {
  res.header('Access-Control-Allow-Origin', '*');
  next();
});

const users = [
  { id: 1, name: 'Oscar' },
  { id: 2, name: 'Ana' },
  { id: 3, name: 'Carlos' },
];

app.get('/users', (_req, res) => {
  res.json(users);
});

app.get('/health', (_req, res) => {
  res.json({ status: 'ok', service: 'api-users' });
});

app.listen(port, () => {
  console.log(`api-users running on http://localhost:${port}`);
});
