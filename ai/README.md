# AI dikelola di repo tersendiri

Source Python, model manifest, CPU lock, panduan policy dan CI/CD berada di [waste-not-app/waste-not-ai](https://github.com/waste-not-app/waste-not-ai). Backend Go tidak memuat YOLO atau memanggil Gemini langsung.

- [Kontrak AI yang dipakai Go](../contracts/README.md).
- [Batas tanggung jawab dan konfigurasi](../docs/AI.md).
- [Handoff Android](../docs/ANDROID.md).

File lama di `deploy/ai/` adalah jejak bootstrap systemd sebelum ekstraksi, bukan jalur deploy aktif. Unit yang dipelihara berada di repo AI; worker berjalan langsung dari `/opt/wastenot-ai/current`.
