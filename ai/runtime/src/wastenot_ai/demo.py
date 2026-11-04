"""Loopback-only camera playground; no production JWT, database, or log writes."""
import asyncio
from contextlib import asynccontextmanager
from datetime import date
import hashlib
import hmac
from importlib.resources import files
import json
import os
from pathlib import Path
import secrets
from threading import Lock
from time import monotonic
from typing import Literal
from uuid import UUID, uuid4

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse, JSONResponse, Response
from pydantic import ValidationError
from starlette.middleware.trustedhost import TrustedHostMiddleware

from .catalog import confirm_context, detection_capabilities, load_catalog, preserve_detected_risk
from .contracts import Context, Contract
from .media import MAX_BYTES, MediaError
from .providers import FixtureProvider, GeminiProvider
from .recommendation import is_reviewed, load_guides
from .decisions import Decisions
from .wire import DecisionInput

TTL = 15 * 60
COOKIE = "wastenot_demo_session"


class RecommendationInput(Contract):
    client_request_id: str
    detection_id: str | None = None
    object_id: str | None = None
    manual: bool = False
    confirmed_object: bool
    context: Context
    provider: Literal["gemini", "fixture"] = "gemini"


def request_uuid(value: str | None):
    try:
        return str(UUID(value or ""))
    except (ValueError, TypeError):
        raise HTTPException(422, detail="INVALID_REQUEST_ID") from None


async def limited_body(request: Request, maximum: int):
    body = bytearray()
    async with asyncio.timeout(10):
        async for chunk in request.stream():
            if len(body) + len(chunk) > maximum:
                raise HTTPException(413, detail="FILE_TOO_LARGE")
            body.extend(chunk)
    return bytes(body)


def create_demo_app(detector=None, *, demo_guides=False, guides_path: Path | None = None,
                    providers=None, on_ready=None) -> FastAPI:
    nonce = secrets.token_urlsafe(32)
    clients, detections, requests = {}, {}, {}
    detector_lock, provider_lock = Lock(), Lock()
    providers = providers or {"gemini": GeminiProvider(), "fixture": FixtureProvider()}
    guides = load_guides(guides_path)
    rate_calls = []
    background = set()

    def cleanup():
        now = monotonic()
        for mapping in (clients, detections):
            for key, value in list(mapping.items()):
                if value["expires"] <= now:
                    del mapping[key]
        for key, value in list(requests.items()):
            if value["expires"] <= now and value["task"].done():
                del requests[key]
        rate_calls[:] = [t for t in rate_calls if t > now - 3600]

    async def scavenger():
        while True:
            await asyncio.sleep(60)
            cleanup()

    @asynccontextmanager
    async def lifespan(app):
        if app.state.detector is None:
            from .detector import YoloDetector
            app.state.detector = YoloDetector(Path(os.getenv("AI_MODEL_PATH", "ai/artifacts/models/yolo11n.pt")),
                                              os.getenv("AI_DEVICE", "cpu"), os.getenv("AI_INPUT_SHAPE", "adaptive"))
        task = asyncio.create_task(scavenger())
        if on_ready:
            on_ready()
        try:
            yield
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
            if background:
                await asyncio.gather(*background, return_exceptions=True)
            clients.clear()
            detections.clear()
            requests.clear()

    app = FastAPI(title="WasteNot local camera demo", docs_url=None, redoc_url=None,
                  openapi_url=None, lifespan=lifespan)
    app.state.detector = detector
    app.add_middleware(TrustedHostMiddleware, allowed_hosts=["127.0.0.1", "localhost", "[::1]"])

    @app.middleware("http")
    async def private_local_headers(request, call_next):
        origin = request.headers.get("origin")
        if origin and origin != f"{request.url.scheme}://{request.headers.get('host')}":
            return JSONResponse({"error":"ORIGIN_NOT_ALLOWED"}, status_code=403)
        response = await call_next(request)
        response.headers["Cache-Control"] = "no-store"
        response.headers["X-Content-Type-Options"] = "nosniff"
        response.headers["Referrer-Policy"] = "no-referrer"
        response.headers["Permissions-Policy"] = "camera=(self), microphone=(), geolocation=()"
        response.headers["Content-Security-Policy"] = (
            "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "
            "img-src 'self' blob: data:; media-src 'self' blob:; connect-src 'self'; "
            "frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
        return response

    def authorize(request):
        cleanup()
        supplied = request.headers.get("x-wastenot-demo", "")
        owner = request.cookies.get(COOKIE)
        if not hmac.compare_digest(supplied.encode(), nonce.encode()) or owner not in clients:
            raise HTTPException(403, detail="DEMO_SESSION_REQUIRED")
        clients[owner]["expires"] = monotonic() + TTL
        return owner

    async def dedup(owner, operation, rid, digest, factory, timeout):
        key = (owner, operation, rid)
        existing = requests.get(key)
        if existing and existing["digest"] != digest:
            raise HTTPException(409, detail="IDEMPOTENCY_CONFLICT")
        if existing:
            task = existing["task"]
        else:
            if len(requests) >= 128:
                raise HTTPException(429, detail="DEMO_SESSION_LIMIT")
            task = asyncio.create_task(factory())
            background.add(task)
            def finished(done):
                background.discard(done)
                if not done.cancelled():
                    error = done.exception()
                    if error is not None:
                        requests.pop(key,None)
            task.add_done_callback(finished)
            requests[key] = {"digest":digest,"task":task,"expires":monotonic()+TTL}
        try:
            async with asyncio.timeout(timeout):
                return await asyncio.shield(task)
        except TimeoutError:
            raise HTTPException(504, detail="AI_TIMEOUT") from None

    @app.get("/")
    async def index(request: Request):
        cleanup()
        owner = request.cookies.get(COOKIE)
        if owner not in clients:
            if len(clients) >= 16:
                raise HTTPException(429, detail="DEMO_SESSION_LIMIT")
            owner = secrets.token_urlsafe(32)
            clients[owner] = {"expires":monotonic()+TTL,"epoch":0}
        page = files("wastenot_ai").joinpath("web/index.html").read_text("utf-8").replace("__DEMO_NONCE__",nonce)
        response = HTMLResponse(page)
        response.set_cookie(COOKIE,owner,httponly=True,samesite="strict",max_age=TTL)
        return response

    @app.get("/demo-assets/{name}")
    async def asset(name: str):
        if name not in {"demo.js","demo.css","jakarta.ttf"}:
            raise HTTPException(404)
        if name == "jakarta.ttf":
            return Response(files("wastenot_ai").joinpath("web/jakarta.ttf").read_bytes(),media_type="font/ttf")
        mime = "text/javascript" if name.endswith(".js") else "text/css"
        return Response(files("wastenot_ai").joinpath(f"web/{name}").read_text("utf-8"),media_type=mime)

    @app.get("/demo/api/config")
    async def config(request: Request):
        authorize(request)
        detector = app.state.detector
        catalog = load_catalog()
        return {"catalog":catalog,"model_labels":detector.labels,
                **detection_capabilities(detector.labels, model_version=detector.artifact["model_version"],
                                         model_sha256=detector.artifact.get("sha256")),
                "model_version":detector.artifact["model_version"],"demo_guides":demo_guides,
                "gemini_configured":bool(getattr(providers["gemini"],"api_key",None) and
                                           getattr(providers["gemini"],"model",None)),
                "gemini_model":getattr(providers["gemini"],"model",None),
                "session_ttl_seconds":TTL,"saved_log":False}

    @app.post("/demo/api/detect")
    async def detect(request: Request):
        owner = authorize(request)
        rid = request_uuid(request.headers.get("x-client-request-id"))
        if request.headers.get("content-type","").split(";")[0] not in {"image/jpeg","image/png"}:
            raise HTTPException(415,detail="UNSUPPORTED_MEDIA_TYPE")
        try:
            image = await limited_body(request,MAX_BYTES)
        except TimeoutError:
            raise HTTPException(504,detail="AI_TIMEOUT") from None
        digest = hashlib.sha256(image).hexdigest()
        epoch = clients[owner]["epoch"]

        async def process():
            if not detector_lock.acquire(blocking=False):
                raise HTTPException(503,detail="AI_BUSY")
            def infer():
                try:
                    return app.state.detector.detect(image)
                finally:
                    detector_lock.release()
            try:
                result = await asyncio.to_thread(infer)
            except MediaError as exc:
                raise HTTPException(exc.status,detail=exc.code) from None
            except Exception:
                raise HTTPException(503,detail="AI_UNAVAILABLE") from None
            if owner not in clients or clients[owner]["epoch"] != epoch:
                raise HTTPException(410,detail="AI_SESSION_EXPIRED")
            cleanup()
            if len(detections) >= 24:
                raise HTTPException(429,detail="DEMO_SESSION_LIMIT")
            detection_id = str(uuid4())
            detections[detection_id] = {"owner":owner,"result":result,"expires":monotonic()+TTL}
            return {"data":{**result,"detection_id":detection_id,"expires_in_seconds":TTL}}
        return await dedup(owner,"detect",rid,digest,process,12)

    @app.post("/demo/api/recommend")
    async def recommendation(request: Request):
        owner = authorize(request)
        try:
            body = await limited_body(request,16_384)
            value = RecommendationInput.model_validate_json(body)
            rid = request_uuid(value.client_request_id)
            item = confirm_context(value.context)
        except (ValueError, ValidationError):
            raise HTTPException(422,detail="INVALID_CONTEXT") from None
        except TimeoutError:
            raise HTTPException(504,detail="AI_TIMEOUT") from None
        if not value.confirmed_object:
            raise HTTPException(422,detail="OBJECT_CONFIRMATION_REQUIRED")
        if value.manual:
            if value.detection_id is not None or value.object_id is not None:
                raise HTTPException(422,detail="INVALID_CONTEXT")
        else:
            entry = detections.get(value.detection_id)
            if not entry or entry["owner"] != owner:
                raise HTTPException(404,detail="DETECTION_NOT_FOUND")
            selected = next((o for o in entry["result"]["objects"] if o["object_id"] == value.object_id),None)
            if selected is None or not selected["supported_for_review"]:
                raise HTTPException(422,detail="OBJECT_REQUIRES_MANUAL")
            value = value.model_copy(update={"context":preserve_detected_risk(value.context,selected)})
        canonical = value.model_dump_json()
        digest = hashlib.sha256(canonical.encode()).hexdigest()

        async def process():
            if not provider_lock.acquire(blocking=False):
                raise HTTPException(503,detail="AI_BUSY")
            # A process-wide ceiling bounds repeated live demo calls. Policy-only
            # and fixture paths have no external cost, but share the same ceiling.
            if len(rate_calls) >= 30:
                provider_lock.release()
                raise HTTPException(429,detail="DEMO_HOURLY_LIMIT")
            rate_calls.append(monotonic())
            def generate():
                try:
                    return Decisions(providers[value.provider],guides,demo_guides=demo_guides).evaluate(DecisionInput(context=value.context))
                finally:
                    provider_lock.release()
            result = await asyncio.to_thread(generate)
            guide = next((g for g in guides if g.object_key == item["object_key"]),None)
            if guide and not (is_reviewed(guide,date.today()) or (demo_guides and guide.publication_status == "DRAFT")):
                guide = None
            response = result.model_dump(mode="json")
            response.update(
                            guide_status=guide.publication_status if guide else None,
                            input_source="manual_confirmation" if value.manual else "yolo_user_confirmation",
                            saved_log=False)
            return {"data":response}
        return await dedup(owner,"recommend",rid,digest,process,20)

    @app.delete("/demo/api/session")
    async def reset(request: Request):
        owner = authorize(request)
        clients[owner]["epoch"] += 1
        for key,entry in list(detections.items()):
            if entry["owner"] == owner:
                del detections[key]
        for key,entry in list(requests.items()):
            if key[0] == owner and entry["task"].done():
                del requests[key]
        return Response(status_code=204)

    return app
