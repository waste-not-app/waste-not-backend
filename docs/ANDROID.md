# Integrasi Android dengan Go dan AI

Diperiksa 7 November 2026. Base URL Retrofit harus memiliki slash terakhir:

```text
https://pred-autoscaler.bccdev.id/wastenot/api/v1/
```

Android hanya memakai API Go. Jangan menghubungkan APK ke port 8091/8093, menyertakan service token/key Gemini, atau memuat Python/bobot YOLO. Origin, pilihan adapter dan timeout merupakan konfigurasi build/client, bukan literal pada layar.

## Kontrak yang dipakai

- Core: [core.openapi.json](../contracts/core.openapi.json) dan [API.md](API.md).
- AI publik: [public.openapi.json](../contracts/ai/public.openapi.json), [contoh request/response](../contracts/ai/examples/) dan [bundle pin](../contracts/ai.lock.json).
- Implementasi AI: [waste-not-ai](https://github.com/waste-not-app/waste-not-ai), khususnya `docs/INTEGRATION.md`.
- Snapshot AI di Go merupakan kontrak integrasi yang disepakati; Python menghasilkan schema, Go memvalidasi request publik/respons worker menggunakan schema tersebut. Jangan mengedit schema untuk meloloskan payload yang salah.

Ekspor sekarang memakai dependency runtime terkunci. Snapshot mencakup metadata sumber terkini serta representasi upload OpenAPI 3.1 `contentMediaType`. Upload tetap multipart JPEG/PNG dengan part `image`, bukan base64 JSON. Setiap repo memeriksa 34 artifact terhadap pin; sebelum perubahan kontrak diterapkan, jalankan pemeriksaan kedua checkout dan buat perubahan pasangan di kedua repo.

```sh
# Dari checkout backend; Python stdlib saja:
python3 scripts/check_contracts.py contracts/ai contracts/ai.lock.json \
  --peer ../waste-not-ai/contracts/v1
```

## Status Android yang benar-benar diperiksa

Main Android `69f3e24` telah memisahkan source AI. Pekerjaan integrasi ada pada [PR #102](https://github.com/waste-not-app/waste-not-android/pull/102), snapshot yang dibaca `ce2c19dded58b4fa276012bbacff5adb2d6dddf3`. Jangan menyamakan isi PR dengan main atau APK rilis.

Di snapshot PR tersebut, core auth/catatan/detail/edit/delete/dashboard/statistik/panduan sudah mempunyai transport. `WasteNotAiApi` juga sudah memiliki katalog, upload, rekomendasi, dan delete sesi; DTO memeriksa korelasi ID/revision serta band model. UI scan sudah mempunyai wiring, tetapi dokumen QA-nya masih menandai eksekusi AI produksi dan penerimaan native menyeluruh sebagai pekerjaan terbuka. Perubahan backend ini tidak mengedit Android atau mengubah adapter default.

Baseline perangkat terbaru adalah Android 11/API 30 ke atas. Kegagalan TLS API 24 merupakan catatan historis di luar baseline tersebut. Jangan menambahkan trust-all, hostname bypass atau cleartext release untuk mengatasinya. Penerimaan tetap perlu memakai API 30 dan runtime Android terbaru.

## Urutan integrasi dan semantik retry

1. Register/login, simpan credential sesuai mekanisme sesi Android, lalu GET `metadata` dan `me`. Metadata memerlukan bearer user. Serialisasikan refresh: hasil refresh yang tidak diketahui memerlukan login ulang; jangan mereplay refresh otomatis.
2. Gunakan server untuk daftar/detail catatan, dashboard/statistik dan progres. Paging menggunakan cursor yang terikat akun/filter; jangan menghitung agregat dari satu halaman daftar atau mengganti error dengan dummy sukses.
3. GET `ai/objects`, patuhi `ai_scan`/`ai_recommendation`, lalu POST `ai/detections` multipart dengan UUID yang dibuat sekali per operasi. JPEG/PNG maksimal 5 MiB, 12 MP, sisi 4096; orientasikan gambar sebelum menampilkan bbox. Server menormalisasi EXIF dan membuang metadata.
4. Tampilkan bbox/band/threshold dari server. Pengguna tetap harus mengonfirmasi material; label `bottle` tidak membuktikan PET/HDPE. Kirim `confirmed_object`, object key/category, `input_source`, revision dan kondisi independen menurut schema publik. Ubah revision ketika konteks berubah.
5. POST `ai/recommendations`. HTTP 200 dapat berarti NEEDS_CONTEXT/UNAVAILABLE; HTTP 201 READY bukan penyimpanan aktivitas. Tampilkan missing context, caution, sumber dan `conditions_to_verify`; jangan mengubah CONDITIONAL menjadi fakta terverifikasi. Status special handling tidak selalu berarti metode null.
6. Hanya setelah konfirmasi objek/metode dan jumlah, POST `waste-logs` dengan `ai_provenance` berisi recommendation ID dan dua flag konfirmasi. Provenance hanya untuk READY/HANDLING, kategori cocok dan sesi belum expired. Metode final boleh dikoreksi; kategori baru/Reduce pencegahan memakai alur manual. Tidak ada XP dari scan/rekomendasi saja.
7. UUID dan payload catatan yang hasilnya tidak diketahui harus dipertahankan sampai replay selesai. Create baru 201, replay identik 200; jangan mengganti UUID untuk mengatasi timeout. Replay catatan yang sudah commit tetap ditemukan meskipun referensi AI kemudian expired. PATCH memakai `version`; DELETE memakai `If-Match`.
8. AI berbeda: replay UUID identik mengembalikan hasil/error tersimpan, tanpa generasi baru. Setelah timeout/FAILED_UNCERTAIN, tindakan pengguna baru dengan UUID baru diperlukan untuk benar-benar mencoba generasi lagi. Tidak ada auto-retry provider. Hormati Retry-After dan jangan retry write secara membabi buta.
9. Saat bersihkan scan, DELETE `ai/detections/{id}` membatalkan sesi dan rekomendasi turunannya. Saat logout/pergantian akun, hapus credential, cursor/cache dan draf pribadi yang tidak sesuai akun.

Budget pilot: provider 17 detik, worker rekomendasi 18, read Android 20, response-header Go 23 dan total HTTP Go 25. Budget ini bukan jaminan latency: Android tetap harus menangani hasil tidak diketahui ketika request telah diterima tetapi respons belum sampai. Ubah timeout bersama, bukan menambah timeout satu lapisan.

## Cakupan dan penerimaan berikutnya

API core tersedia beserta endpoint `GET /v1/achievements` (`capabilities.achievements=true`) dan `POST/GET/DELETE /v1/photos` (aktif ketika `PHOTO_STORAGE_DIR` dikonfigurasi). Draf kurasi 16 artikel Panduan umum tersedia di `guides/core-guides-v1.json` dan siap di-impor setelah tinjauan manusia menjadi `PUBLISHED`. Empat policy guide AI terpisah: PET, HDPE, kaleng aluminium non-aerosol dan kardus. Model aktif gabungan v4 + COCO11s.

Pengingat lokal (`S17`) dijalankan lokal oleh Android, sementara fasilitas daur ulang (`S19`), reset password, dan penghapusan akun belum tersedia di backend. Lihat [panduan integrasi lengkap Android & AI](INTEGRATION.md) untuk detail payload `achievements`, `photos`, dan `pilot-policy-09` (`provider=rules`).

Gate Android: jalankan login → capture/upload → konfirmasi → konteks → READY/NEEDS_CONTEXT/UNAVAILABLE/CONDITIONAL → save/replay → detail/edit/delete → ringkasan → logout/pergantian akun. Uji timeout, cancel, expiry, konflik versi, jaringan terputus dan pemisahan akun. Pisahkan bukti HTTP/schema, MockWebServer, kamera/perangkat dan penerimaan visual; readiness backend tidak menyelesaikan gate UI.
