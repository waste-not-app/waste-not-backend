# Panduan Kontribusi

## Alur kerja

1. Buat branch dari `main`: `feat/...`, `fix/...`, `refactor/...`, atau `chore/...`.
2. Satu commit untuk satu perubahan logis, dengan format [Conventional Commits](https://www.conventionalcommits.org/) satu baris, misalnya `fix(auth): release hashing slot on panic`.
3. Jalankan `make check` sebelum push, dan `make test-integration` bila menyentuh database.
4. Buka pull request ke `main`. CI harus hijau sebelum merge.

## Standar kode

- Format dengan `make fmt`; `make lint` harus 0 issue.
- Komentar hanya bila benar-benar perlu, cukup satu baris singkat. Anotasi Swagger (`// @...`) tetap dipertahankan.
- Logika bisnis di `service`, akses data di `repository`, HTTP di `handler`.
- Query baru ditulis di `sql/queries/` lalu `make sqlc`. Perubahan skema berupa migrasi baru di `internal/database/` dan `sql/migrations/`.
- Perubahan endpoint harus memperbarui `contracts/core.openapi.json` dan `make swag`.
