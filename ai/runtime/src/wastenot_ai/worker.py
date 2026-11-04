import asyncio
from contextlib import asynccontextmanager
import hmac
import os
from pathlib import Path
from threading import Lock

from fastapi import FastAPI, HTTPException, Request

from .media import MAX_BYTES, MediaError
from .http_api import BoundedHTTP, configure_errors, envelope, install_security_schema
from .wire import DetectionCapabilities, Envelope, InternalDetection, internal_detection


def create_app(detector=None, token: str | None = None, timeout: float = 10) -> FastAPI:
    service_token = token if token is not None else os.getenv("AI_SERVICE_TOKEN", "")
    if len(service_token) < 24:
        raise ValueError("AI_SERVICE_TOKEN wajib berupa secret minimal 24 karakter.")
    busy = Lock()

    @asynccontextmanager
    async def lifespan(app):
        if app.state.detector is None:
            from .detector import YoloDetector
            app.state.detector = YoloDetector(Path(os.getenv("AI_MODEL_PATH", "ai/artifacts/models/yolo11n.pt")),
                                              os.getenv("AI_DEVICE", "cpu"), os.getenv("AI_INPUT_SHAPE", "adaptive"))
        yield

    app = FastAPI(title="WasteNot private YOLO worker", docs_url=None, redoc_url=None,
                  openapi_url=None, lifespan=lifespan)
    app.state.detector = detector
    app.add_middleware(BoundedHTTP, maximum=None)
    configure_errors(app)

    def authorize(request):
        expected = f"Bearer {service_token}"
        if not hmac.compare_digest(request.headers.get("authorization", "").encode(), expected.encode()):
            raise HTTPException(401, detail="UNAUTHENTICATED")

    @app.get("/health/ready")
    async def ready(request: Request):
        authorize(request)
        if app.state.detector is None:
            raise HTTPException(503, detail="MODEL_NOT_READY")
        return {"ready": True}

    @app.post("/internal/detect", include_in_schema=False)
    @app.post("/internal/v1/detect", response_model=Envelope[InternalDetection],
              openapi_extra={"requestBody": {"required": True, "content": {
                  mime: {"schema": {"type": "string", "format": "binary", "maxLength": MAX_BYTES}}
                  for mime in ("image/jpeg", "image/png")}}})
    async def detect(request: Request):
        authorize(request)
        if app.state.detector is None:
            raise HTTPException(503, detail="MODEL_NOT_READY")
        if request.headers.get("content-type", "").split(";")[0] not in {"image/jpeg", "image/png"}:
            raise HTTPException(415, detail="UNSUPPORTED_MEDIA_TYPE")
        # Reject overload before reading an image; no unbounded waiting queue.
        if not busy.acquire(blocking=False):
            raise HTTPException(503, detail="AI_BUSY", headers={"Retry-After": "1"})
        buffer = bytearray()
        delegated = False

        def run_inference():
            try:
                return app.state.detector.detect(bytes(buffer))
            finally:
                buffer.clear()
                busy.release()

        try:
            async with asyncio.timeout(timeout):
                async for chunk in request.stream():
                    if len(buffer) + len(chunk) > MAX_BYTES:
                        raise HTTPException(413, detail="FILE_TOO_LARGE")
                    buffer.extend(chunk)
                # A timed-out native inference cannot be killed safely. Its lock stays
                # held until completion; a disconnected caller cannot start another job.
                task = asyncio.create_task(asyncio.to_thread(run_inference))
                task.add_done_callback(lambda done: done.exception() if not done.cancelled() else None)
                delegated = True
                result = await asyncio.shield(task)
                if request.url.path == "/internal/detect":
                    return {"data": result}
                try:
                    value = internal_detection(result)
                except (ValueError, KeyError, TypeError):
                    raise HTTPException(503, detail="INVALID_AI_RESPONSE") from None
                return envelope(request, value)
        except TimeoutError:
            raise HTTPException(504, detail="AI_TIMEOUT") from None
        except MediaError as exc:
            raise HTTPException(exc.status, detail=exc.code) from None
        except HTTPException:
            raise
        except Exception:
            raise HTTPException(503, detail="AI_UNAVAILABLE") from None
        finally:
            if not delegated:
                buffer.clear()
                busy.release()

    @app.get("/internal/v1/capabilities", response_model=Envelope[DetectionCapabilities])
    async def capabilities(request: Request):
        authorize(request)
        from .catalog import detection_capabilities, load_catalog
        model = app.state.detector.artifact
        labels = app.state.detector.labels
        names = [labels[i] for i in range(len(labels))]
        return envelope(request, {"model": {"model_version": model["model_version"],
                                           "sha256": model["sha256"], "class_names": names},
                                  "thresholds": load_catalog()["thresholds"],
                                  **detection_capabilities(labels, model_version=model["model_version"], model_sha256=model["sha256"])})

    install_security_schema(app)
    return app
