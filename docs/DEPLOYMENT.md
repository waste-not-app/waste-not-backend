# CI/CD dan Deployment Containerized Blue-Green (cp-bcc)

Target deployment: host `okta-mlops`, SSH alias lokal `cp-bcc`, endpoint SSH `proxy.bccdev.id:11049`.

## Arsitektur Deployment Baru (Containerized Blue-Green)

1. **Docker Containerized**:
   - Seluruh layanan WasteNot dideploy menggunakan Docker Engine dan Docker Compose v2.
   - **PostgreSQL 18**: Dijalankan sebagai container `wastenot-db` (`postgres:18-bookworm`) dengan persistent Docker volume `wastenot-postgres-data`. Bind port aman `127.0.0.1:5432`.
   - **Kubernetes (K3s)**: K3s dibiarkan bersih dan tidak lagi digunakan oleh WasteNot, sehingga resource K3s sepenuhnya dialokasikan untuk workload/proyek lain.
   - **Go Backend API**: Berjalan di dalam container Distroless non-root dengan mode Blue-Green:
     - Slot Blue: Container `wastenot-api-blue` pada port loopback `127.0.0.1:18081`.
     - Slot Green: Container `wastenot-api-green` pada port loopback `127.0.0.1:18082`.

2. **Nginx Upstream Switching (Zero-Downtime)**:
   - File upstream: `/etc/nginx/conf.d/wastenot-upstream.conf`:
     ```nginx
     upstream wastenot_upstream {
         server 127.0.0.1:18081; # atau 18082
     }
     ```
   - Snippet Nginx: `/etc/nginx/snippets/wastenot-api.conf` me-routing `location ^~ /wastenot/api/` ke `proxy_pass http://wastenot_upstream/;`.
   - Perpindahan rilis dilakukan dengan mengubah port upstream, menjalankan `nginx -t`, lalu `systemctl reload nginx` tanpa downtime.

3. **Mekanisme Blue-Green Deploy (`deploy/deploy-bluegreen.sh`)**:
   - Deteksi slot aktif saat ini (`blue` atau `green`).
   - Eksekusi database migration terhadap target image:
     `docker run --rm --network host -e DATABASE_URL=... $IMAGE /migrate`.
   - Jalankan container pada target slot yang tidak aktif.
   - Jalankan healthcheck loopback `http://127.0.0.1:<target_port>/health/ready` hingga siap (maksimal 30 detik).
   - Jika target gagal ready, target container dibatalkan dan container aktif sebelumnya tetap melayani traffic tanpa gangguan.
   - Jika target sehat, Nginx upstream dialihkan ke slot baru dan Nginx di-reload secara graceful.
   - Container lama dihentikan dan dibersihkan.

## Pipeline GitHub Actions (`.github/workflows/ci.yml`)

1. **Job `verify`**:
   - Menjalankan PostgreSQL integration tests (dengan service container).
   - Menjalankan unit tests, Go vet, race detection, dependency audit, dan vulncheck.
   - Membangun container image Docker multi-stage Distroless: `wastenot-backend:${{ github.sha }}`.
   - Mengompres dan mengunggah image container sebagai release artifact.

2. **Job `deploy`** (otomatis pada push ke branch `main`):
   - Mengunduh artifact container image.
   - Mengirim image melalui SSH ke VM dan memuatnya (`docker load`).
   - Menjalankan script `/usr/local/bin/wastenot-deploy-bluegreen wastenot-backend:${{ github.sha }}`.
   - Memverifikasi endpoint publik HTTPS:
     `https://pred-autoscaler.bccdev.id/wastenot/api/health/ready`.

## Secrets dan Variables Repository

Konfigurasi repository GitHub Actions:

- **Secrets**:
  - `SSH_PRIVATE_KEY` / `WASTENOT_DEPLOY_KEY`: SSH private key untuk otentikasi ke VM.
  - `SSH_KNOWN_HOSTS` / `WASTENOT_KNOWN_HOSTS`: SSH host keys untuk `[proxy.bccdev.id]:11049`.
- **Variables**:
  - `VM_HOST` / `WASTENOT_DEPLOY_HOST`: `proxy.bccdev.id`
  - `VM_PORT` / `WASTENOT_DEPLOY_PORT`: `11049`
  - `VM_USER` / `WASTENOT_DEPLOY_USER`: `dev`
  - `VM_IP`: `103.179.134.116`

## Pemeriksaan Status oleh Operator

Perintah untuk memeriksa status runtime di VM:

```sh
# Cek container aktif
ssh cp-bcc "docker ps"

# Cek logs database
ssh cp-bcc "docker logs wastenot-db --tail 20"

# Cek logs container API yang sedang aktif
ssh cp-bcc "docker logs wastenot-api-blue --tail 30"  # atau wastenot-api-green

# Cek konfigurasi upstream Nginx saat ini
ssh cp-bcc "cat /etc/nginx/conf.d/wastenot-upstream.conf"

# Uji health check lokal dan publik
ssh cp-bcc "curl -fsS http://127.0.0.1:18081/health/ready"
curl -fsS https://pred-autoscaler.bccdev.id/wastenot/api/health/ready
```
