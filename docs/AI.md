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

## Status dan input operator

Pada 4 November 2026 worker YOLO11n COCO sudah terpasang sebagai layanan dan inferensi melalui HTTPS publik lulus. Ini pemasangan eksperimental untuk verifikasi integrasi; gate evaluasi/holdout dan penerimaan Android di atas tetap belum selesai. Worker keputusan melayani katalog asli, tetapi kesiapan prosesnya tidak membuktikan Gemini dapat menghasilkan rekomendasi. Capability rekomendasi pada Go tetap false.

File `/etc/wastenot/ai-recommendation.env` sudah disiapkan root-only pada VM. Isi `GEMINI_API_KEY` dan `GEMINI_MODEL` langsung melalui sesi operator, tanpa menaruh key di repo, GitHub Actions atau chat:

```sh
ssh -t cp-bcc 'sudoedit /etc/wastenot/ai-recommendation.env'
```

Pertahankan `AI_RECOMMENDATION_TOKEN` yang sudah dibuat. Jangan menyalin isi file ke log. `AI_GUIDES_PATH` harus menunjuk JSON dengan schema `Guide` dari worker Python, setelah kontennya benar-benar direview untuk konteks lokal. Panduan bawaan tetap DRAFT dan tidak dipromosikan otomatis. File panduan inti yang diimpor binary Go memiliki schema berbeda; keduanya tidak saling menggantikan.

Setelah input tersedia, validasi schema/masa berlaku panduan, hak baca akun `wastenot-decisions`, akses model Gemini pada akun terkait, dan satu rekomendasi nyata melalui worker privat sebelum mengubah `AI_RECOMMENDATION_ENABLED`. Restart hanya layanan keputusan dan API WasteNot, lalu uji rekomendasi publik, dedup, provenance dan metadata. Key saja belum cukup untuk melewati gate panduan; jangan mengaktifkan `AI_ALLOW_PROTOTYPE` atau demo-guides untuk meloloskannya.

`GEMINI_TIMEOUT_SECONDS` dibatasi maksimal 21 detik pada entry layanan ini. Deadline worker satu detik lebih panjang, tetap di bawah response-header timeout Go 23 detik dan total HTTP 25 detik. Nilai yang melampaui budget ditolak saat startup; jangan menaikkan satu timeout sendiri. Uji privat Gemini 3.5 Flash-Lite pertama melewati 15 detik; uji diagnostik dengan budget 45 detik berhasil READY/RECYCLE sekitar 19.2 detik, tetap `prototype_only=true` karena panduan draf. Uji ini tidak mengaktifkan rekomendasi publik atau mempublikasikan panduan.

Uji ulang pada budget 21 detik, termasuk satu request melalui HTTP ASGI privat dengan draft eksplisit, menghasilkan `UNAVAILABLE/LLM_TIMEOUT` dengan method null. Keberhasilan satu request diagnostik tidak membuktikan latensi stabil. Rekomendasi publik tetap false; budget HTTP Android yang ada juga perlu diverifikasi sebelum penerimaan bersama. Jangan memperpanjang semua timeout tanpa pengukuran. Hasil pemeriksaan 78 panduan/195 evidence dan usulan perbaikannya ada pada [review sumber](AI_GUIDE_SOURCE_REVIEW.md); dokumen itu tidak mengubah status publikasi atau identitas reviewer.
