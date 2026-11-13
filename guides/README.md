# WasteNot Curated Guides (`B11`)

`core-guides-v1.json` berisi **16 artikel panduan awal** yang mencakup seluruh 7 kategori (`PLASTIC`, `ORGANIC`, `PAPER`, `GLASS`, `METAL`, `ELECTRONIC`, `OTHER`) dengan sumber HTTPS resmi (`BPOM`, `DLH DKI Jakarta`, `US EPA`, `Cornwall Council`).

## Aturan Publikasi (`PRD.md` §12 & `API.md` §7)

Seluruh artikel pada file ini disetel ke `publication_status: "DRAFT"` agar tidak tayang ke produksi sebelum ditinjau langsung oleh penanggung jawab konten.

Untuk mempublikasikan ke database:

1. Tinjau kesesuaian isi langkah dan batasan lokal di Indonesia untuk setiap artikel.
2. Ganti `reviewer` dengan nama peninjau sebenarnya, perbarui `reviewed_at`, dan ubah `publication_status` ke `"PUBLISHED"`.
3. Jalankan importer:

```sh
set -a; . ./.env; set +a
go run ./cmd/guides guides/core-guides-v1.json
```

Atau dengan binary container `/guides`:

```sh
docker run --rm --env-file /etc/wastenot/migrate.env -v "$PWD/guides:/guides:ro" --entrypoint /guides wastenot-backend:<tag> /guides/core-guides-v1.json
```
