# Verifikasi

Tanggal: 4 November 2026. Sumber Android: c1258596cb71bc7b95dc9a4f140f55f759815c4e.

## Status pilot terakhir

178 tes Python lulus secara lokal, termasuk format Gemini ringkas, budget lintas layanan, policy draf dan empat panduan sumber-terperiksa. Source upstream 31 file beserta patch terdaftar lolos verifikasi hash. Empat request HTTP privat ke worker produksi memakai Gemini 3.1 Flash-Lite asli dan panduan PUBLISHED versi 6: seluruhnya READY/RECYCLE, `prototype_only=false`, sekitar 3.38/1.28/8.48/1.36 detik. Ini empat kasus fungsional, bukan benchmark.

Setelah capability dibuka, 17 pemeriksaan HTTPS publik lulus pada rilis e5c40c4: kedua capability, katalog empat pilot/label non-aerosol, foto melalui YOLO asli, isolasi referensi deteksi, Gemini asli/evidence versi 6, replay rekomendasi, konflik revisi, tidak ada auto-save/XP, simpan dengan provenance dan 10 XP, replay simpan, penolakan pemakaian rekomendasi kedua, snapshot DB model/Gemini, statistik 250 gram, isolasi log, delete mengembalikan XP, penghapusan sesi mempertahankan audit snapshot, dan logout mencabut sesi. Akun sementara dihapus berdasarkan ID tepatnya.

Respons rekomendasi publik pada satu konteks uji sekitar 3779 ms. Material PET/kondisi merupakan input konfirmasi sintetis pada smoke, bukan hasil identifikasi material dari foto. Snapshot DB mempertahankan checksum YOLO nyata serta provider Gemini dengan prototype false. Empat panduan diperiksa sumbernya secara otomatis oleh Codex, bukan persetujuan keselamatan manusia; masa tinjau ulang sampai 4 Desember 2026. Tujuh puluh empat draf lain tetap tidak diterbitkan. Batas provider/worker 17/18 detik dipilih agar di bawah read timeout Android 20 detik.

Model 3.5 tidak dipakai pada pilot aktif karena beberapa timeout dan kegagalan upstream; satu diagnostik compact berhasil sekitar 20.17 detik dengan 31 output token. Model 3.1 berhasil pada uji pembanding sekitar 1589 ms dan empat worker checks di atas. Koneksi TCP/TLS 3.5 selesai sekitar 0.29 detik, header Google baru tiba sekitar 20.17 detik; tidak ada bukti bahwa menambah CPU VM akan menghilangkan antrean/upstream delay. Model 2.5 tidak dipilih karena uji request tidak tersedia. Tidak ada klaim percentile, stabilitas atau SLA dari sampel kecil ini.

Detail tahap sebelumnya di bawah membedakan scan-only/draf dari pilot aktif. Holdout checkpoint fine-tuning empat kelas, mutu konten oleh manusia, uji beban, keamanan menyeluruh, retensi/restore produksi dan penerimaan UI Android tetap di luar bukti pengujian ini.

## Pengujian inti dan tahap sebelum aktivasi pilot


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

Run [41859621326](https://github.com/waste-not-app/waste-not-backend/actions/runs/41859621326) meluluskan 146 tes Python beserta verify/deploy commit a0d675f. Konfigurasi Gemini/model/timeout dan token layanan yang dirotasi sudah dimuat oleh worker; token tidak dicetak dalam pemeriksaan. Dua puluh smoke AI/core publik kembali lulus setelah rilis ini. Worker produksi menolak draf dengan `NO_REVIEWED_GUIDE`, capability rekomendasi tetap false. Uji Gemini privat berikutnya pada 21 detik mengalami `LLM_TIMEOUT`; belum ada bukti latensi model konsisten pada budget aplikasi.
