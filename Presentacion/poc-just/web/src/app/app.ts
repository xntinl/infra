import { Component, inject, OnInit, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { environment } from '../environments/environment';

interface User {
  id: number;
  name: string;
}

interface Product {
  id: number;
  name: string;
  price: number;
}

@Component({
  selector: 'app-root',
  template: `
    <div class="container">
      <header>
        <h1>Just PoC</h1>
        <p class="subtitle">Multi-Project Orchestration</p>
      </header>

      <div class="grid">
        <section class="card">
          <div class="card-header users">
            <h2>Users</h2>
            <span class="badge">:3001</span>
          </div>
          <ul>
            @for (user of users(); track user.id) {
              <li>
                <span class="id">#{{ user.id }}</span>
                <span class="name">{{ user.name }}</span>
              </li>
            }
          </ul>
          @if (usersError()) {
            <p class="error">{{ usersError() }}</p>
          }
        </section>

        <section class="card">
          <div class="card-header products">
            <h2>Products</h2>
            <span class="badge">:3002</span>
          </div>
          <ul>
            @for (product of products(); track product.id) {
              <li>
                <span class="id">#{{ product.id }}</span>
                <span class="name">{{ product.name }}</span>
                <span class="price">\${{ product.price }}</span>
              </li>
            }
          </ul>
          @if (productsError()) {
            <p class="error">{{ productsError() }}</p>
          }
        </section>
      </div>

      <div class="terminal">
        <div class="terminal-header">
          <span class="terminal-dot red"></span>
          <span class="terminal-dot yellow"></span>
          <span class="terminal-dot green"></span>
          <span class="terminal-title">justfile</span>
        </div>
        <div class="terminal-body">
          <div class="terminal-line"><span class="terminal-prompt">$</span> <span class="terminal-cmd">just setup</span> <span class="terminal-comment"># Instalar dependencias de todos los proyectos</span></div>
          <div class="terminal-line"><span class="terminal-prompt">$</span> <span class="terminal-cmd">just dev</span> <span class="terminal-comment"># Levantar todo: backends + frontend</span></div>
          <div class="terminal-line"><span class="terminal-prompt">$</span> <span class="terminal-cmd">just health</span> <span class="terminal-comment"># Verificar salud de los 3 servicios</span></div>
          <div class="terminal-line"><span class="terminal-prompt">$</span> <span class="terminal-cmd">just ci</span> <span class="terminal-comment"># CI completo de todos los proyectos</span></div>
          <div class="terminal-line"><span class="terminal-prompt">$</span> <span class="terminal-cmd">just stop</span> <span class="terminal-comment"># Detener servicios y liberar puertos</span></div>
          <div class="terminal-line"><span class="terminal-prompt">$</span> <span class="terminal-cmd">just clean</span> <span class="terminal-comment"># Limpiar node_modules</span></div>
        </div>
      </div>

      <footer>
        <span class="footer-version">v1.0.0</span> &middot; orquestado con <code>just</code>
      </footer>
    </div>
  `,
  styles: [`
    /* ── Reset & Base ──────────────────────────────── */

    * { margin: 0; padding: 0; box-sizing: border-box; }

    :host {
      display: block;
      min-height: 100vh;
      background: #0a0a0a;
    }

    /* ── Animations ────────────────────────────────── */

    @keyframes fadeSlideUp {
      from {
        opacity: 0;
        transform: translateY(20px);
      }
      to {
        opacity: 1;
        transform: translateY(0);
      }
    }

    @keyframes pulse-dot {
      0%, 100% { opacity: 1; box-shadow: 0 0 6px #22c55e88; }
      50% { opacity: 0.6; box-shadow: 0 0 2px #22c55e44; }
    }

    /* ── Layout ────────────────────────────────────── */

    .container {
      max-width: 860px;
      margin: 0 auto;
      padding: 3rem 2rem;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      justify-content: center;
      gap: 2.5rem;
      background: #0a0a0a;
      color: #e5e5e5;
      font-family: 'Inter', system-ui, -apple-system, sans-serif;
    }

    /* ── Header / Typography ───────────────────────── */

    header {
      text-align: center;
      animation: fadeSlideUp 0.6s ease-out both;
    }

    h1 {
      font-size: 2.75rem;
      font-weight: 800;
      letter-spacing: -0.03em;
      line-height: 1.1;
      color: #a78bfa;
    }

    .subtitle {
      color: #737373;
      margin-top: 0.625rem;
      font-size: 1rem;
      font-weight: 400;
      letter-spacing: 0.01em;
    }

    /* ── Grid ──────────────────────────────────────── */

    .grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 1.75rem;
    }

    /* ── Card ──────────────────────────────────────── */

    .card {
      background: #141414;
      border-radius: 14px;
      border: 1px solid #262626;
      overflow: hidden;
      transition:
        border-color 0.3s ease,
        box-shadow 0.3s ease,
        transform 0.3s ease;
      animation: fadeSlideUp 0.5s ease-out both;
    }

    .card:nth-child(1) { animation-delay: 0.1s; }
    .card:nth-child(2) { animation-delay: 0.25s; }

    .card:hover {
      border-color: #3a3a3a;
      transform: translateY(-2px);
      box-shadow:
        0 4px 24px rgba(99, 102, 241, 0.07),
        0 1px 6px rgba(168, 85, 247, 0.05);
    }

    /* ── Card Header ───────────────────────────────── */

    .card-header {
      display: flex;
      align-items: center;
      gap: 0.625rem;
      padding: 1.1rem 1.35rem;
      border-bottom: 1px solid #262626;
      position: relative;
    }

    .card-header.users {
      background: #1e293b;
    }

    .card-header.products {
      background: #1c1c2e;
    }

    h2 {
      font-size: 0.85rem;
      font-weight: 600;
      flex: 1;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: #d4d4d4;
    }

    .badge {
      display: inline-flex;
      align-items: center;
      gap: 0.4rem;
      font-size: 0.7rem;
      padding: 0.2rem 0.6rem;
      border-radius: 999px;
      background: rgba(99, 102, 241, 0.08);
      border: 1px solid rgba(99, 102, 241, 0.15);
      color: #a5b4fc;
      font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
      font-weight: 500;
    }

    .badge::before {
      content: '';
      display: inline-block;
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: #22c55e;
      box-shadow: 0 0 6px #22c55e88;
      animation: pulse-dot 2s ease-in-out infinite;
    }

    /* ── List ──────────────────────────────────────── */

    ul {
      list-style: none;
      padding: 0.375rem 0;
    }

    li {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      padding: 0.8rem 1.35rem;
      transition:
        background 0.2s ease,
        padding-left 0.2s ease;
      position: relative;
    }

    li:not(:last-child)::after {
      content: '';
      position: absolute;
      bottom: 0;
      left: 1.35rem;
      right: 1.35rem;
      height: 1px;
      background: #1e1e1e;
      opacity: 0.5;
    }

    li:hover {
      background: rgba(99, 102, 241, 0.04);
      padding-left: 1.5rem;
    }

    /* ── List Item Elements ────────────────────────── */

    .id {
      font-family: 'SF Mono', 'Fira Code', monospace;
      font-size: 0.75rem;
      font-weight: 500;
      color: #818cf8;
      min-width: 1.75rem;
      opacity: 0.8;
    }

    .name {
      flex: 1;
      font-size: 0.925rem;
      font-weight: 450;
      color: #e5e5e5;
    }

    .price {
      font-family: 'SF Mono', 'Fira Code', monospace;
      font-size: 0.85rem;
      color: #4ade80;
      font-weight: 600;
      background: rgba(34, 197, 94, 0.08);
      padding: 0.15rem 0.5rem;
      border-radius: 6px;
    }

    /* ── Error State ───────────────────────────────── */

    .error {
      padding: 1rem 1.35rem;
      color: #f87171;
      font-size: 0.85rem;
      background: rgba(239, 68, 68, 0.05);
      border-left: 2px solid rgba(239, 68, 68, 0.3);
      margin: 0.5rem 1rem;
      border-radius: 0 6px 6px 0;
    }

    /* ── Terminal Section ──────────────────────────── */

    .terminal {
      background: #0c0c0c;
      border: 1px solid #262626;
      border-radius: 14px;
      overflow: hidden;
      transition:
        border-color 0.3s ease,
        box-shadow 0.3s ease;
      animation: fadeSlideUp 0.5s ease-out 0.4s both;
    }

    .terminal:hover {
      border-color: #333;
      box-shadow: 0 2px 16px rgba(34, 197, 94, 0.04);
    }

    .terminal-header {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      padding: 0.75rem 1rem;
      background: #1a1a1a;
      border-bottom: 1px solid #262626;
    }

    .terminal-dot {
      width: 12px;
      height: 12px;
      border-radius: 50%;
    }

    .terminal-dot.red { background: #ef4444; }
    .terminal-dot.yellow { background: #eab308; }
    .terminal-dot.green { background: #22c55e; }

    .terminal-title {
      margin-left: 0.5rem;
      font-size: 0.8rem;
      color: #525252;
      font-family: 'SF Mono', 'Fira Code', monospace;
    }

    .terminal-body {
      padding: 1rem 1.25rem;
      display: flex;
      flex-direction: column;
      gap: 0.45rem;
    }

    .terminal-line {
      font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
      font-size: 0.82rem;
      line-height: 1.6;
      display: flex;
      gap: 0.4rem;
      align-items: baseline;
      transition: opacity 0.2s ease;
    }

    .terminal-line:hover {
      opacity: 0.85;
    }

    .terminal-prompt {
      color: #22c55e;
      font-weight: 700;
      user-select: none;
    }

    .terminal-cmd {
      color: #e5e5e5;
      font-weight: 500;
      white-space: nowrap;
    }

    .terminal-comment {
      color: #525252;
      font-style: italic;
      margin-left: 0.25rem;
    }

    /* ── Footer ────────────────────────────────────── */

    footer {
      text-align: center;
      color: #3f3f3f;
      font-size: 0.82rem;
      animation: fadeSlideUp 0.5s ease-out 0.55s both;
    }

    .footer-version {
      font-family: 'SF Mono', 'Fira Code', monospace;
      color: #525252;
    }

    code {
      background: #1c1c1c;
      border: 1px solid #2a2a2a;
      padding: 0.15rem 0.5rem;
      border-radius: 6px;
      font-size: 0.8rem;
      color: #c084fc;
      font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
    }

    /* ── Responsive ────────────────────────────────── */

    @media (max-width: 640px) {
      .grid { grid-template-columns: 1fr; }
      .terminal-comment { display: none; }
    }
  `],
})
export class App implements OnInit {
  private http = inject(HttpClient);

  users = signal<User[]>([]);
  products = signal<Product[]>([]);
  usersError = signal('');
  productsError = signal('');

  ngOnInit() {
    this.http.get<User[]>(`${environment.usersApi}/users`).subscribe({
      next: (data) => this.users.set(data),
      error: () => this.usersError.set('Cannot connect to api-users'),
    });

    this.http.get<Product[]>(`${environment.productsApi}/products`).subscribe({
      next: (data) => this.products.set(data),
      error: () => this.productsError.set('Cannot connect to api-products'),
    });
  }
}
