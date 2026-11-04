# Verifikasi

Tanggal: 4 November 2026. Sumber Android: c1258596cb71bc7b95dc9a4f140f55f759815c4e.

- Go 1.27.3 dan PostgreSQL 18 dipakai secara lokal. Integration suite membuat schema PostgreSQL terisolasi, melakukan migrasi dan menghapus schema setelah selesai.
- 49 kasus/subkasus lulus dengan race detector; tambahan kasus campuran unit penanganan juga lulus dengan race detector. Tidak ada race yang dilaporkan.
- Auth: validasi password/JWT, algoritma/issuer/audience/expiry, login salah, rotasi refresh, replay refresh mencabut sesi, logout idempoten, dan rate limit DB lintas instance.
- Catatan: isolation akun untuk GET/PATCH/DELETE, XOR unit/batas/enum/tanggal, create replay/conflict/deleted replay, delapan create paralel menjadi satu log, version conflict, signed cursor terikat pengguna/filter, dan edit catatan lama.
- Dataset PRD: 1400 g ditangani, 2 benda dihindari, lima aktivitas, recycling rate 50%, 50 XP, streak dua hari. Empty/Reduce-only mengirim rate null. Campuran gram/benda tetap terpisah.
- AI: HTTP worker fixture, model checksum/selection dari snapshot, public field spoofing, replay/conflict, lintas akun, dua instance berbagi satu generasi, timeout tersimpan tanpa retry provider, TTL, batas media/MIME/signature, prototype gate, delete saat generation dan crash recovery/cleanup.
- Worker keputusan Python asli dengan fixture provider/draft guides: enam skenario lulus melalui Go + PostgreSQL + HTTP privat, mencakup empat metode, NEEDS_CONTEXT dan CONDITIONAL. Tidak membaca key Gemini pengguna atau menjalankan inferensi YOLO live.
- Panduan: pencarian PUBLISHED, draft tersembunyi, query injection tidak mengubah SQL, importer menolak reviewer kosong.
- Empat OpenAPI lolos openapi-spec-validator 0.9.0. Respons core pada tes HTTP divalidasi terhadap schema OpenAPI. Empat puluh file snapshot kontrak/fixture cocok byte-for-byte dengan Android sumber.
- go vet, go mod verify, build tiga binary statis, dan pemeriksaan konfigurasi Compose lulus. govulncheck v1.9.0 melaporkan No vulnerabilities found setelah dependency crypto diperbarui ke revisi perbaikan upstream.

Tes ini membuktikan behavior transport/domain dan pengaman transaksi pada lingkungan pengujian. Bukan benchmark beban, audit keamanan menyeluruh, penerimaan UI Android, akurasi YOLO atau mutu Gemini live. Build container dan deployment telah lulus di GitHub Actions run 41813752013. Sepuluh smoke check melalui prefix Nginx di VM lulus; root Nginx dan UID/restart pod sistem dipertahankan. HTTPS publik pred-autoscaler.bccdev.id dan sepuluh smoke check auth/pencatatan/statistik/logout juga lulus; detail ada di DEPLOYMENT.md.

Dua belas tes boundary/deployment Python mencakup checksum, tar path/symlink/duplikat/limit, migration failure, rollback readiness, kebutuhan prefix Nginx dan batas source Python rilis.

## Inferensi YOLO nyata pada VM

Run [41821090984](https://github.com/waste-not-app/waste-not-backend/actions/runs/41821090984) meluluskan verify/deploy commit `9a6aefd`. Suite Python memuat 142 tes policy/transport dan audit seluruh 52 versi dependency runtime tanpa advisory yang diketahui pada waktu pengujian. CPU wheel dipasang dengan hash pada environment terpisah. NumPy 2.3.5 dan fallback tanpa NNPACK diuji langsung pada CPU KVM VM ini.

Worker YOLO dan keputusan telah dipasang melalui bootstrap setelah rilis tersebut aktif. Sepuluh pemeriksaan AI melalui HTTPS publik lulus: autentikasi, metadata scan aktif/rekomendasi nonaktif, katalog worker asli, inferensi YOLO botol, replay identik, konflik perubahan foto, gate Gemini, tanpa log/XP otomatis, isolasi akun, dan delete/expiry idempoten. Checksum model pada snapshot DB cocok dengan artifact resmi `0ebbc80d4a7680d14987a577cd21342b65ecfd94632bd9a8da63ae6417644ee1`. Akun uji sementara dihapus berdasarkan ID tepatnya setelah pengujian.

Pada satu foto, respons publik melaporkan `candidate-0ebbc80d4a76`, `REVIEW_REQUIRED`, kelas `bottle`, confidence 0.78385, kategori/material belum diputuskan dan konfirmasi pengguna tetap diwajibkan. Waktu request publik sekitar 1002 ms. Tiga inferensi langsung sebelumnya pada foto yang sama memerlukan sekitar 7207/638/635 ms, termasuk cold inference pertama. Angka ini hanya smoke satu contoh, bukan pengukuran akurasi, throughput, p95 atau beban paralel.

Sepuluh pemeriksaan API inti auth/CRUD/statistik/logout diulang setelah AI aktif dan lulus. Setelah inferensi, memori worker YOLO sekitar 329 MiB dan keputusan 44 MiB, tanpa restart gagal; kedua port tetap loopback. Hash konfigurasi, master PID Nginx serta UID/restart tiga pod sistem tetap sama. Tidak ada restart layanan lain.

Transport Gemini nyata telah diuji secara privat dengan draf yang tetap berlabel prototype_only: model gemini-3.5-flash-lite menghasilkan READY/RECYCLE sekitar 19.2 detik setelah percobaan 15 detik mengalami timeout. Ini tidak membuktikan rekomendasi publik atau mutu konten. Panduan PUBLISHED, checkpoint fine-tuning empat kelas, holdout dan penerimaan Android belum diverifikasi. Capability rekomendasi tetap false. Bukti transport ini tidak memenuhi gate peluncuran AI untuk pengguna umum.

Deploy ulang saat kedua worker aktif juga lulus pada [run 41857739280](https://github.com/waste-not-app/waste-not-backend/actions/runs/41857739280), commit 655f116. Readiness privat Go/worker serta HTTPS publik diperiksa helper/pipeline, lalu sepuluh smoke AI nyata dan sepuluh smoke core diulang dan lulus.
