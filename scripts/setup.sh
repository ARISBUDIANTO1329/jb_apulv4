#!/bin/bash
set -e

echo "=== JB Apul v4 Setup ==="

# Check env
if [ ! -f .env ]; then
    cp .env.example .env
    echo "Created .env from .env.example"
    echo "Edit .env with your settings before running!"
fi

# Start services
echo "Starting services..."
docker compose up -d --build

echo "=== Setup complete ==="
echo "Backend: http://localhost:8001"
echo "Run: docker compose logs -f backend"
