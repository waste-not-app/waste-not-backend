# Adapter AI

Worker Python tetap berasal dari repo Android pada commit `c1258596cb71bc7b95dc9a4f140f55f759815c4e`. Python memiliki mapping, threshold, policy/evidence, kondisi berisiko dan Gemini. Go memiliki auth, ownership, rate limit, dedup, TTL, snapshot dan transaksi simpan. Tidak ada subprocess Python per request atau key Gemini pada Go.

## Konfigurasi

Jalankan worker sesuai `ai/README.md` Android: `serve-worker` pada loopback 8091 dan `serve-recommendations` pada loopback 8093. Gunakan interpreter Python yang cocok dengan pin dependency; Python 3.12 dipakai pada pengujian integrasi. Pasang model yang checksum, mapping dan kelasnya sesuai manifest. Untuk keputusan produksi, pilih Gemini dan panduan PUBLISHED yang benar-benar direview; fixture dan `--demo-guides` hanya untuk pengujian.

Pada proses Go:

```sh
export AI_ENABLED=true
export AI_ALLOW_PROTOTYPE=false
export AI_DETECTION_URL=http://127.0.0.1:8091
export AI_RECOMMENDATION_URL=http://127.0.0.1:8093
# Isi dua secret berbeda, minimal 24 karakter:
# AI_SERVICE_TOKEN, AI_RECOMMENDATION_TOKEN
```

Endpoint privat memakai bearer service masing-masing, deadline 25 detik, koneksi dibatasi, tanpa redirect dan tanpa retry generasi otomatis. Origin worker hanya berasal dari konfigurasi operator; input publik tidak menerima URL, prompt, confidence, raw class, provider, guide atau allowed_methods. Token/key tidak ada di access log. Gemini key hanya pada worker keputusan.

`AI_ENABLED` adalah gate operator. Aktifkan untuk pengguna hanya setelah gate pada AI_SPEC terpenuhi: artifact/taksonomi/lisensi/holdout, threshold, panduan lokal, evaluasi model/LLM, retensi, dan penerimaan Android. `AI_ALLOW_PROTOTYPE=true` mengizinkan hasil fixture/draft yang tetap diberi label asli. Hasil tersebut ditolak Go ketika flag false. Metadata katalog/capability di-cache maksimal 30 detik; setiap deteksi tetap menyimpan model/checksum/mapping aktualnya sendiri. Health ready core tidak menyatakan worker, model, editorial atau Gemini siap.

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
