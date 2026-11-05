# Adapter AI

Snapshot worker Python sekarang dipaketkan pada ai/runtime dan berasal dari repo Android pada commit `c1258596cb71bc7b95dc9a4f140f55f759815c4e`. Python memiliki mapping, threshold, policy/evidence, kondisi berisiko dan Gemini. Go memiliki auth, ownership, rate limit, dedup, TTL, snapshot dan transaksi simpan. Tidak ada subprocess Python per request atau key Gemini pada Go.

## Konfigurasi

Jalankan worker sesuai `ai/README.md` Android: `serve-worker` pada loopback 8091 dan `serve-recommendations` pada loopback 8093. Gunakan interpreter Python yang cocok dengan pin dependency; Python 3.12 dipakai pada pengujian integrasi. Pasang model yang checksum, mapping dan kelasnya sesuai manifest. Untuk keputusan produksi, pilih Gemini dan panduan PUBLISHED yang benar-benar direview; fixture dan `--demo-guides` hanya untuk pengujian.

Pada proses Go:

```sh
export AI_ENABLED=true
# Aktifkan rekomendasi hanya setelah key Gemini dan panduan review tersedia.
export AI_RECOMMENDATION_ENABLED=false
export AI_ALLOW_PROTOTYPE=false
export AI_DETECTION_URL=http://127.0.0.1:8091
export AI_RECOMMENDATION_URL=http://127.0.0.1:8093
# Isi dua secret berbeda, minimal 24 karakter:
# AI_SERVICE_TOKEN, AI_RECOMMENDATION_TOKEN
```

Endpoint privat memakai bearer service masing-masing, deadline 25 detik, koneksi dibatasi, tanpa redirect dan tanpa retry generasi otomatis. Origin worker hanya berasal dari konfigurasi operator; input publik tidak menerima URL, prompt, confidence, raw class, provider, guide atau allowed_methods. Token/key tidak ada di access log. Gemini key hanya pada worker keputusan.

`AI_ENABLED` adalah gate operator untuk katalog/deteksi. `AI_RECOMMENDATION_ENABLED` memisahkan generasi saran; false mengembalikan AI_DISABLED tanpa memanggil provider. Deteksi baseline adalah kandidat eksperimental dan tidak membuktikan akurasi sampah/material. Aktifkan untuk pengguna hanya setelah gate pada AI_SPEC terpenuhi: artifact/taksonomi/lisensi/holdout, threshold, panduan lokal, evaluasi model/LLM, retensi, dan penerimaan Android. `AI_ALLOW_PROTOTYPE=true` mengizinkan hasil fixture/draft yang tetap diberi label asli. Hasil tersebut ditolak Go ketika flag false. Metadata katalog/capability di-cache maksimal 30 detik; setiap deteksi tetap menyimpan model/checksum/mapping aktualnya sendiri. Health ready core tidak menyatakan worker, model, editorial atau Gemini siap.

## Media dan privasi

Multipart hanya menerima `image` dan `client_request_id`, maksimal satu per field. JPEG/PNG wajib cocok signature/decode dengan MIME, maksimal 5 MiB, 12 MP, sisi 4096. Body dibatasi 5 MiB + 64 KiB overhead; tidak ada parser multipart yang menulis temp file. Orientasi EXIF dinormalisasi lalu gambar dienkode ulang tanpa metadata sebelum dikirim ke worker. Dua slot per instance membatasi decode/upload bersamaan; beban berlebih 503 AI_BUSY dengan Retry-After. Input asli dipakai untuk digest idempotensi, bukan untuk training atau penyimpanan gambar.

Gambar tinggal di memori request dan tidak ditulis ke DB/disk/bucket. Crash tidak menyisakan file ingress. Ini tidak menjamin penghapusan RAM secara forensik atau menonaktifkan swap/core dump mesin; hardening host/worker tetap menjadi tanggung jawab deployment. Proses API tidak menyediakan lampiran foto permanen.

## Idempotensi dan siklus hidup

PostgreSQL menyimpan satu operasi per `(user_id, operation, client_request_id)`. Hash input kanonis berbeda menghasilkan 409; worker hanya dipanggil pemilik job. Request bersamaan dari instance lain menunggu status tersimpan maksimal 27 detik. Polling tidak memanggil provider. AI dibatasi sepuluh operasi baru/menit dan 100/hari per akun, digabung untuk deteksi/rekomendasi. Batas ini baseline operasional yang perlu ditinjau berdasarkan pemakaian nyata.

State IN_PROGRESS, COMPLETED atau FAILED_UNCERTAIN tahan restart. Transport timeout/error yang tidak diketahui disimpan, termasuk error/retry-after. Replay ID yang sama mengembalikan hasil/error tersimpan. Pengguna harus memulai generasi baru dengan UUID baru secara eksplisit bila ingin mencoba lagi; Go tidak membelanjakan quota provider dua kali diam-diam. Operasi IN_PROGRESS yang tertinggal dua menit setelah crash menjadi FAILED_UNCERTAIN melalui cleanup tiap menit.

Sesi/rekomendasi berlaku maksimal 24 jam; rekomendasi deteksi tidak melampaui sesi induk. Snapshot model/class/checksum/objek tersimpan menjadi trusted selection privat. Client tidak dapat menggantinya. Skema JSON asli Python dan pemeriksaan silang label/bbox/band/status menghalangi respons worker malformed. Respons fallback tidak diubah menjadi saran buatan Go.

DELETE sesi menandai sesi dan rekomendasi terkait tidak berlaku serta mengosongkan snapshot sementara. Hasil yang datang terlambat tidak menghidupkan sesi lagi. Cleanup tiap menit menghapus record expired; jika API berhenti, expiry tetap diperiksa pada setiap akses dan cleanup berjalan lagi setelah proses hidup. Tabel provenance log menyimpan snapshot audit tanpa gambar dan tanpa foreign key yang memperpanjang umur sesi AI.

POST waste-logs memeriksa ownership, expiry, status READY, scope HANDLING, kategori, kedua konfirmasi dan linkage unik dalam transaksi. Final method boleh berbeda; accepted dihitung server. Satu recommendation_id tidak dapat dipakai log baru meskipun log pertama sudah soft-delete. Statistik hanya membaca log aktif.

## Menguji worker Python

Suite standar memakai worker HTTP fixture untuk menguji protokol, biaya panggilan, cross-instance dedup, expiry, media dan spoofing tanpa key/model. Uji opsional berikut memanggil modul keputusan Python asli dengan fixture provider dan demo guides:

```sh
# Jalankan serve-recommendations dari repo Android pada 8093,
# --provider fixture --demo-guides, dengan token pengujian baru.
export TEST_AI_RECOMMENDATION_URL=http://127.0.0.1:8093
export TEST_AI_RECOMMENDATION_TOKEN='<token pengujian worker>'
export TEST_DATABASE_URL='<database PostgreSQL khusus tes>'
go test ./internal/api -run TestPythonDecisionWorker -v
```

Test memverifikasi empat metode, NEEDS_CONTEXT dan CONDITIONAL melalui Go + DB + Python, dengan deteksi capability fixture. Ini tidak mengukur akurasi YOLO, kualitas Gemini live, latency perangkat atau biaya produksi. Tidak ada key Gemini yang dibaca dari checkout pengguna.


## Runtime CPU pada cp-bcc

Source, provenance, pin wheel CPU dan service units tersedia di ai/ dan deploy/ai/. Fingerprint requirements-cpu.lock terikat pada paket rilis. Helper memeriksa worker privat setelah restart dan mengembalikan source/binary sebelumnya bila readiness gagal. Bobot resmi diverifikasi SHA-256 sebelum load; source Python dan service entry dipilih dari direktori release immutable yang sama dengan Go.

Bootstrap AI tidak mengubah database atau Nginx. Pemasangan menggunakan environment Python tersendiri di /opt/wastenot-ai/venv, dua akun OS khusus, service tokens berbeda, batas memori/CPU dan port loopback. Detektor tidak memiliki akses jaringan keluar. Key Gemini/AI_GUIDES_PATH tetap kosong sampai operator menyediakannya; tidak memakai fixture atau meloloskan draf diam-diam.

Urutan bootstrap: pasang environment CPU dari lock dengan hash, deploy paket Go/Python, jalankan deploy/ai/bootstrap.py pada staging yang sudah diperiksa, lalu uji deteksi nyata dan metadata capability. AI_RECOMMENDATION_ENABLED tetap false sampai Gemini serta panduan terbitan siap. Gunakan checkpoint fine-tuning sendiri hanya setelah file/manifest/checksum dan hasil evaluasinya tersedia; deployment awal mempertahankan default COCO dari proyek.

## Status pilot dan konfigurasi operator

YOLO11n COCO CPU dan Gemini 3.1 Flash-Lite sudah aktif pada cp-bcc. Metadata melaporkan `ai_scan=true` dan `ai_recommendation=true`, dengan `AI_ALLOW_PROTOTYPE=false`. Alur HTTPS foto → YOLO → konfirmasi konteks → Gemini → simpan → statistik/XP telah diuji dengan model/provider nyata. Ini pilot integrasi backend; holdout, akurasi material dan penerimaan Android belum terbukti.

Empat panduan versi 6 aktif: PET, HDPE, kaleng aluminium non-aerosol dan kardus. [Artifact panduan](../ai/guides/README.md) menyebut reviewer **Codex automated source review; not human safety approval**, cakupan Indonesia bersyarat konfirmasi penerima setempat, dan masa berlaku sampai 4 Desember 2026. Tujuh puluh empat panduan lain tetap DRAFT/tidak tersedia. Sumber asing tidak diubah menjadi klaim penerimaan nasional. Ketidakjelasan material/riwayat isi, akses penerima NO, risiko di luar cakupan, panduan kedaluwarsa dan objek lain tetap meminta konteks atau UNAVAILABLE; tidak menghasilkan metode cadangan buatan Go.

File panduan terpasang pada `/opt/wastenot-ai/guides/pilot-v6-375c0fd47b64.json`, SHA-256 `375c0fd47b6405a0b1551209d941782c5cf58e28fb85e9d467395bf6064cadb1`. Parent root:group keputusan mode 0750, file 0440; layanan dapat membaca tetapi tidak menulisnya. `AI_GUIDES_PATH` menunjuk file tersebut. [Proposal draf](../ai/guide-proposals/README.md) tetap terpisah dan tidak dipasang. Panduan inti binary Go mempunyai schema berbeda; endpoint `/v1/guides` tidak otomatis mengambil panduan policy AI.

Secret hanya pada `/etc/wastenot/ai-recommendation.env`, root-only. Ubah key/model langsung melalui operator, tanpa menaruh key di repo, GitHub Actions atau chat:

```sh
ssh -t cp-bcc 'sudoedit /etc/wastenot/ai-recommendation.env'
```

Konfigurasi pilot memakai `GEMINI_MODEL=gemini-3.1-flash-lite`, `GEMINI_TIMEOUT_SECONDS=17`. Deadline worker 18 detik, di bawah read timeout Android 20 detik, response-header Go 23 detik dan total HTTP Go 25 detik. Entry menolak budget provider di atas 17. Jangan mengubah satu timeout sendiri. Model 3.5 sempat berhasil tetapi berulang kali melewati 21 detik; koneksi TLS sekitar 0.29 detik dan waktu terbesar berada di upstream. Pemilihan 3.1 didasarkan pada uji nyata, bukan mengganti provider dengan fixture. Ini tidak menjamin latency percentile atau ketersediaan Google. Hasil lambat/invalid tetap UNAVAILABLE tanpa auto-retry generasi.

Protokol Gemini meminta `recommended_method`, `special_handling`, `evidence_ids`; prosa internal lama dinormalisasi untuk validasi. Prosa publik, langkah dan sumber tetap dirender dari evidence. Validasi metode/evidence, special handling, idempotensi dan provenance tetap berlaku. Perubahan metadata headless, format pilihan, label pilot dan tanggal pemeriksaan sumber dicatat pada `ai/upstream.json`; class ID, bobot, mapping dan threshold tidak berubah.

Setelah perubahan key/model/panduan, validasi schema/masa berlaku, hak baca akun keputusan dan satu rekomendasi nyata lewat worker privat sebelum membuka capability publik. Pertahankan token layanan dan jangan menyalin file env ke log. Restart hanya layanan keputusan/API WasteNot; pembaruan source lewat CI merestart kedua worker serta API dan memeriksa readiness terautentikasi. Readiness proses tidak membuktikan kualitas konten atau SLA provider.
