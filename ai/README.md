# WasteNot AI runtime

Python tetap menjadi pemilik mapping, policy, evidence dan Gemini. Source worker disalin dari Android commit c1258596cb71bc7b95dc9a4f140f55f759815c4e, dengan daftar hash pada upstream.json. Satu patch pada detector.py hanya mengenali metadata distribusi resmi ultralytics-opencv-headless. Tidak mengubah bobot, preprocessing, taxonomy atau kebijakan rekomendasi. verify_source.py menolak drift source yang belum dicatat.

Runtime memakai Python 3.12, PyTorch CPU dan Ultralytics headless. Versi serta hash seluruh dependency berada pada requirements-cpu.lock. VM cp-bcc mengekspos CPU kvm64 tanpa SSSE3/SSE4/AVX; NumPy dipin 2.3.5 karena wheel 2.4+ memerlukan baseline x86-64-v2. Pin ini diuji pada CPU VM, tidak memalsukan feature CPU. Worker tidak membutuhkan CUDA, driver NVIDIA, GUI, atau container privileged. requirements-runtime.lock menyediakan dependency ringan untuk tes policy/transport di CI. audit_dependencies.py memeriksa seluruh versi runtime; suffix lokal +cpu dinormalisasi untuk pencarian advisory upstream, sedangkan instalasi tetap memverifikasi hash wheel CPU resmi.

Bobot model baseline YOLO11n COCO diambil dari rilis resmi Ultralytics; checksum dan sumber pada model.json. Script scripts/fetch_ai_model.py memeriksa checksum sebelum file dipakai. Model ini bukan checkpoint fine-tuning empat kelas yang dilaporkan pada Android; bobot checkpoint tersebut tidak tersedia di Git. Tidak ada klaim akurasi sampah atau material dari baseline COCO. Material dan kondisi tetap dikonfirmasi pengguna.

Distribusi headless adalah paket resmi Ultralytics, dengan AGPL-3.0/opsi Enterprise seperti sumber model. Rujukan: https://docs.ultralytics.com/quickstart dan https://docs.ultralytics.com/models/yolo11/. Foto smoke dari sumber eksperimen Android tidak disalin ke repo atau dipakai training.

## Layanan VM

- wastenot-yolo: private loopback 8091, CPU satu thread, limit CPU 75%, memory 1280 MiB. Koneksi keluar dibatasi ke loopback. Token deteksi berbeda dari token keputusan; tidak memiliki key Gemini atau credential DB.
- wastenot-decisions: private loopback 8093, limit CPU 25%, memory 256 MiB. Katalog tetap tersedia tanpa key; generasi rekomendasi memerlukan key Gemini dan panduan yang ditinjau.
- Go: AI_ENABLED mengaktifkan deteksi dan katalog, AI_RECOMMENDATION_ENABLED dapat dimatikan terpisah. Metadata melaporkan kedua capability secara jujur. Endpoint rekomendasi yang dimatikan tidak memanggil provider.

serve.py hanya menjalankan factory HTTP yang sudah ada; tidak memakai mock, CLI subprocess per request atau demo-guides. Detektor warm-up sebelum port dibuka. Foto inference tetap di memori dan tidak disimpan sebagai log/XP.

## Rilis

Paket CI menyertakan ai-runtime.zip dan ai-serve.py bersama binary Go. Helper deployment memvalidasi batas zip/path, checksum dan fingerprint dependency yang dipasang. Source Python diekstrak ke direktori release immutable dan dipilih melalui symlink current. Saat worker telah dipasang, deploy/rollback merestart layanan AI milik WasteNot serta memeriksa readiness privat dengan service token. Database, Nginx dan layanan lain tidak direstart.

Environment dependency dan bobot dikelola operator terpisah dari source release. Perubahan requirements-cpu.lock ditolak helper sampai environment baru sudah dipasang serta diverifikasi; CI tidak mengunduh ratusan MB dependency setiap deploy aplikasi. Tidak mengganti bobot atau memberi key melalui paket kode CI.
