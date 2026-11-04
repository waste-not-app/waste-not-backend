# Menghubungkan Android

Acuan sumber: Android `c1258596cb71bc7b95dc9a4f140f55f759815c4e`; kontrak core pada design kit v1.2 dan DTO/fixture transport A03/A04. Kontrak HTTP AI terbaru dari `ai/contracts/v1` mengungguli contoh AI lama dalam dokumen desain bila berbeda.

Untuk emulator Android, API lokal memakai base URL `http://10.0.2.2:8080/v1/`. Android memerlukan slash terakhir dan hanya mengizinkan HTTP loopback/emulator pada konfigurasi debug. Staging/release membutuhkan origin HTTPS nyata. Gunakan property build yang sudah didokumentasikan dalam `docs/ANDROID_SETUP.md` Android; jangan hardcode URL di layar. Port API harus dapat dicapai emulator dari host.

Endpoint metadata, auth/login/register/refresh/logout, me dan waste-logs GET sesuai `WasteNotApi.kt`. JSON menggunakan `data`, enum uppercase, gram/count integer, UUID dan tanggal/zona valid. Fixture metadata tersimpan pada `contracts/android/metadata.json`. Capability tambahan tetap boolean; capability nonaktif tidak menggagalkan pencatatan manual.

## Wiring Android yang masih diperlukan

Checkout Android belum mengimplementasikan semua panggilan berikut. Backend ini tidak mengubah source atau perubahan lokal Android.

1. Tambahkan POST waste-logs, GET detail, PATCH, DELETE If-Match ke `WasteNotApi` dan DTO. Buat satu UUID pada persiapan draf; pertahankan UUID/payload saat timeout create.
2. Ganti `RemoteActivityStore.create/update` yang masih menghasilkan NOT_CONFIGURED dengan repository API; hubungkan delete dan penanganan VERSION_CONFLICT. Jangan mengubah demo store menjadi penyimpanan remote.
3. Tambahkan DTO dashboard/statistics/waste-guide/progress. Jangan memakai data contoh sebagai fallback ketika server gagal.
4. Tambahkan kontrak AI publik dari `contracts/ai/public.openapi.json`: confirmed_object eksplisit, input_source, context_revision, condition, status READY/NEEDS_CONTEXT/UNAVAILABLE, basis CONDITIONAL dan conditions_to_verify.
5. Satu request refresh pada satu waktu; setelah hasil refresh tidak diketahui, login ulang. Rotasi membuat access token lama tidak berlaku. Request write tidak boleh direplay otomatis oleh transport.
6. Buang credential/draf pribadi saat pergantian akun dan logout; cursor terikat akun/filter. Hormati Retry-After dan jangan menganggap respons 200 AI selalu READY.

AI tidak menyimpan log. Setelah pengguna mengonfirmasi objek/metode dan mengisi jumlah, POST waste-logs dapat membawa ai_provenance berisi recommendation_id dan kedua flag konfirmasi true. Kategori harus sesuai; final method boleh berbeda dari saran. REDUCE tetap manual tanpa provenance sampai kontrak provenance pencegahan diperluas. Hasil fixture/draf harus ditampilkan sebagai simulasi pada pengujian.

Penerimaan bersama berikutnya harus menjalankan auth → catat → detail/edit/delete → dashboard/statistik → logout pada emulator/perangkat dengan backend ini. Tes HTTP dan schema di repo backend membuktikan transport/domain server, belum membuktikan rendered UI Android atau jaringan perangkat.
