# JB Apul v4 — Go + HTMX

**Stack:**
- **Backend:** Go (Chi router) — compiled, lightweight, concurrency via goroutine
- **Frontend:** Go html/template + HTMX + Tailwind CSS (CDN)
- **Database:** PostgreSQL 16
- **Cache/Queue:** Redis 7
- **Workers:** All in 1 Go binary (goroutine-based monolith)
- **Deployment:** Docker Compose + nginx

## Quick Start

```bash
cp .env.example .env
# Edit .env with your settings

./scripts/setup.sh
# Or manually:
docker compose up -d --build
```

## Hot Reload

Frontend: edit `.html` files → refresh browser  
Backend Go: `air` auto-restarts on file changes

## Services

| Service | Port | Description |
|---------|------|-------------|
| backend | 8001 | Go API + HTML |
| db | 5433 | PostgreSQL |
| redis | 6379 | Redis |
| nginx | 80/443 | Reverse proxy |

## Dev Notes

- No Node.js required — all frontend is server-side rendered
- No build step — edit HTML, refresh browser
- Workers run as goroutines inside the same process
- `./backend/migrations/init.sql` auto-runs on first DB startup
