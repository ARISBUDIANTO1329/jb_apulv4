# Hermes Workflow — JB Apul v4

**Status:** Lokal adalah titik utama perbaikan → push GitHub → VPS ambil dari GitHub

---

## 📋 Alur Kerja Harian (Local → GitHub → VPS)

### 1. Buat Perubahan di Lokal
```bash
# Edit file, buat fitur, atau perbaiki bug
# Contoh: edit README.md, buat fitur baru, dll.

# Jika membuat progress.md atau file baru:
# Pastikan file tersebut tidak berisi rahasia (password, key, dll.)
```

### 2. Commit & Push ke GitHub
```bash
# Tambah semua perubahan (hanya file yang boleh di-commit)
git add .

# Commit dengan pesan deskriptif
git commit -m "feat: deskripsi perubahan Anda"

# Push ke GitHub (origin master)
git push origin master
```

### 3. Update di VPS (sampai setelahbeli VPS)
```bash
# Masuk ke direktori project
cd /home/jaybani/backup-project/jb_apulv4

# Ambil perubahan terbaru dari GitHub
git pull origin master

# Rebuild & restart containers dengan konfigurasi baru
docker compose down
docker compose up -d --build

# Cek status
docker ps --filter "name=jb_apulv4"
```

---

## 🛡️ Rules & Catatan Penting

### File yang BOLEH di-commit:
- `.md` files (README, progress, doc)
- Source code Go (`*.go`, `go.mod`, `go.sum`)
- `docker-compose.yml` (jika ada perubahan konfigurasi)
- `nginx/nginx.conf` (jika butuh SSL atau routing ubah)
- `scripts/setup.sh`
- `.env.example` (tidak ada rahasia)
- File baru yang tidak berisi kredensial

### File yang JANGAN di-commit (sudah di `.gitignore`):
- `.env` (isi rahasia — password DB, SECRET_KEY, OAuth credentials)
- `storage/` (filenya besar, binary)
- `backend/server` (binary hasil build Go)
- `vendor/` (jika ada, biasanya di `go.sum` saja)
- `postgres_data/`, `redis_data/` (volume Docker)
- Log file (`*.log`)

### 🔐 Penanganan .env
- **Never commit `.env`** — sudah ada di `.gitignore`
- Jika butuh ubah `.env.example`, isi nilai dengan placeholder seperti `change-me-to-random-string`
- Di VPS, salin `.env.example` menjadi `.env` dan isi nilai nyata di sana (atau gunakan secret manager)

---

## 📦 Script Otomatisasi (Opsional)

Buat `scripts/git-push.sh` untuk sekali-kali command:

```bash
#!/bin/bash
# Script: git-push.sh
# Usage: ./scripts/git-push.sh "pesan commit"

set -e

# Validasi pesan commit
if [ -z "$1" ]; then
    echo "Usage: $0 \"pesan commit\""
    exit 1
fi

echo "📦 Menambahkan perubahan..."
git add .

echo "🛠️ Commit: $1"
git commit -m "$1"

echo "⬆️ Push ke GitHub..."
git push origin master

echo "✅ Selesai! Perubahan terupload ke GitHub."
echo "💡 Di VPS: cd /home/jaybani/backup-project/jb_apulv4 && git pull origin master && docker compose up -d --build"
```

Buat file executable:
```bash
chmod +x scripts/git-push.sh
```

Gunakan setiap mau push:
```bash
./scripts/git-push.sh "feat: tambah fitur X"
```

---

## 📊 Cek Status Sekarang

```bash
# Cek branch & status
git status

# Cek log commit terakhir
git log --oneline -5

# Cek apakah progress.md udah ter-push
git log --all --oneline | grep progress
```

Output terakhir saat ini:
```
5fa195b feat: add progress.md — development status tracking (Indonesia)
698ccea feat: add full navigation bar
```

---

## 🚀 Siap ke VPS

Setelah kamu beli VPS dan sudah mau deploy:

```bash
# di VPS (tidak perlu pakai git credentials jika sudah clone)
cd /home/jaybani/backup-project/jb_apulv4
git pull origin master       # ambil perubahan dari GitHub
./scripts/setup.sh           # atau:
docker compose up -d --build # rebuild containers
```

Seluruh konfigurasi, code, dan progress.md akan sinkron dari GitHub ke VPS.