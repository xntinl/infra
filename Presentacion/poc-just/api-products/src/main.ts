import express from 'express';

const app = express();
const port = process.env.PRODUCTS_PORT || 3002;

app.use((_req, res, next) => {
  res.header('Access-Control-Allow-Origin', '*');
  next();
});

const products = [
  { id: 1, name: 'Laptop', price: 999 },
  { id: 2, name: 'Mouse', price: 29 },
  { id: 3, name: 'Keyboard', price: 79 },
];

app.get('/products', (_req, res) => {
  res.json(products);
});

app.get('/health', (_req, res) => {
  res.json({ status: 'ok', service: 'api-products' });
});

app.listen(port, () => {
  console.log(`api-products running on http://localhost:${port}`);
});
