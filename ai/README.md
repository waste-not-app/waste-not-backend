# WasteNot AI Service Architecture

Modul runtime Python AI (YOLO11 CPU detection & Gemini recommendation engine) kini telah dipisahkan ke repositori mandiri:
👉 [waste-not-app/waste-not-ai](https://github.com/waste-not-app/waste-not-ai)

## Hubungan Backend (Go) & AI (Python)

1. **Kontrak API (Contract-First)**:
   - Skema OpenAPI antarmuka AI tersimpan di `contracts/ai/` (`private-detection.openapi.json`, `private-recommendation.openapi.json`, dan `public.openapi.json`).
   - Klien HTTP backend di `internal/ai/client.go` memvalidasi payload request dan response terhadap kontrak tersebut.

2. **Komunikasi Layanan**:
   - Backend Go bertindak sebagai **API Gateway / Backend-for-Frontend (BFF)**.
   - Di server produksi / VM (`cp-bcc` / `okta-mlops`), layanan AI berjalan sebagai microservice internal di loopback port:
     - `http://127.0.0.1:8091` (`wastenot-yolo.service` — Ultralytics YOLOv11 CPU worker).
     - `http://127.0.0.1:8093` (`wastenot-decisions.service` — Gemini recommendation worker).
   - Backend Go terhubung ke port internal tersebut menggunakan autentikasi Bearer service token terisolasi.

3. **CI/CD & Deployment Independen**:
   - Backend Go dideploy via Blue-Green container Docker melalui workflow `.github/workflows/ci.yml`.
   - Modul AI dideploy secara terpisah via workflow CI/CD mandiri di repo `waste-not-ai`, mengekstrak release immutable ke `/opt/wastenot-ai/releases/` dengan symlink atomik `/opt/wastenot-ai/current`.
