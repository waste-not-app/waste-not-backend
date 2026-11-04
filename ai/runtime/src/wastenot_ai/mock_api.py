"""Public Go contract mock. No JWT verification, YOLO, Gemini, DB or waste-log writes."""
from datetime import datetime, timedelta, timezone
import hashlib
import json
from threading import RLock
from uuid import UUID, uuid4

from fastapi import Depends, FastAPI, File, Form, HTTPException, Request, Response, UploadFile
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from .catalog import detection_capabilities, review_detections
from .contracts import RawDetection
from .decisions import DecisionError, Decisions
from .http_api import BoundedHTTP, configure_errors, envelope, install_security_schema
from .media import MAX_BYTES, MediaError, decode_image
from .providers import FixtureProvider
from .recommendation import ProviderError, load_guides
from .wire import (API_VERSION, DecisionInput, DetectionSession, Envelope, ObjectCatalog,
                   RecommendationInput, RecommendationSession, TrustedSelection)

MOCK_TOKENS = {"mock-user-a": "a", "mock-user-b": "b"}
SCENARIOS = ("normal", "uncertain", "no-detections", "provider-unavailable", "rate-limited", "timeout")
CLASS_NAMES = ["aluminium_can", "glass_bottle", "hdpe_bottle", "plastic_bottle"]


class MockDetector:
    labels = dict(enumerate(CLASS_NAMES))
    artifact = {"model_version": "mock-four-class-v1", "sha256": "0" * 64, "labels": labels}

    def detect(self, data, *, scenario="normal"):
        image = decode_image(data)
        dimensions = {"width": image.width, "height": image.height}
        image.close()
        objects = [] if scenario == "no-detections" else [RawDetection(
            class_id=1, raw_class="glass_bottle", confidence=.6 if scenario == "uncertain" else .9,
            bbox=(.1, .1, .8, .9))]
        return {**review_detections(objects, self.labels), "model": self.artifact, "image": dimensions}


class UnavailableProvider:
    name = "fixture"

    def generate(self, payload):
        raise ProviderError("LLM_UNAVAILABLE")


def create_mock_app(*, scenario="normal", clock=None, ttl_seconds=86400, uuid_factory=uuid4):
    if scenario not in SCENARIOS:
        raise ValueError("Unknown mock scenario")
    now = clock or (lambda: datetime.now(timezone.utc))
    decisions = Decisions(UnavailableProvider() if scenario == "provider-unavailable" else FixtureProvider(),
                          load_guides(), demo_guides=True, today=lambda: now().date())
    sessions, cache = {}, {}
    lock = RLock()
    bearer = HTTPBearer(auto_error=False)

    def user(credentials: HTTPAuthorizationCredentials | None = Depends(bearer)):
        if credentials is None or credentials.credentials not in MOCK_TOKENS:
            raise HTTPException(401, detail="UNAUTHENTICATED")
        return MOCK_TOKENS[credentials.credentials]

    app = FastAPI(title="WasteNot PUBLIC CONTRACT MOCK — not production Go", version=API_VERSION)
    app.add_middleware(BoundedHTTP, maximum=MAX_BYTES + 65536, mock=True)
    configure_errors(app)
    app.state.sessions, app.state.cache = sessions, cache
    app.state.decisions = decisions

    def fail_scenario():
        if scenario == "rate-limited":
            raise HTTPException(429, detail="RATE_LIMITED", headers={"Retry-After": "30"})
        if scenario == "timeout":
            raise HTTPException(504, detail="AI_TIMEOUT")

    def cleanup():
        # Expired sessions retain a bounded tombstone for distinguishing expiry from missing IDs.
        for key, row in list(cache.items()):
            if row["expires"] <= now():
                del cache[key]
        for key, row in list(sessions.items()):
            if row["expires"] + timedelta(seconds=ttl_seconds) <= now():
                del sessions[key]

    def session(owner, session_id):
        row = sessions.get(session_id)
        if row is None or row["owner"] != owner:
            raise HTTPException(404, detail="DETECTION_NOT_FOUND")
        if row["deleted"] or row["expires"] <= now():
            raise HTTPException(410, detail="AI_SESSION_EXPIRED")
        return row

    def cached(owner, operation, request_id, digest):
        cleanup()
        key = (owner, operation, request_id)
        entry = cache.get(key)
        if entry and entry["digest"] != digest:
            raise HTTPException(409, detail="IDEMPOTENCY_CONFLICT")
        if not entry and len(cache) >= 128:
            raise HTTPException(429, detail="RATE_LIMITED", headers={"Retry-After": "30"})
        return key, entry

    def remember(key, digest, value, expires):
        cache[key] = {"digest": digest, "value": value, "expires": expires}

    @app.get("/v1/ai/objects", response_model=Envelope[ObjectCatalog])
    def objects(request: Request, owner=Depends(user)):
        catalog = decisions.catalog().model_copy(update={"model_version": "mock-four-class-v1",
                                                          **detection_capabilities(MockDetector.labels)})
        return envelope(request, catalog)

    @app.post("/v1/ai/detections", response_model=Envelope[DetectionSession], status_code=201,
              responses={200: {"model": Envelope[DetectionSession], "description": "Idempotent replay"}})
    async def detect(request: Request, response: Response, image: UploadFile = File(...),
                     client_request_id: UUID = Form(...), owner=Depends(user)):
        fail_scenario()
        try:
            if image.content_type not in {"image/jpeg", "image/png"}:
                raise HTTPException(415, detail="UNSUPPORTED_MEDIA_TYPE")
            data = await image.read(MAX_BYTES + 1)
        finally:
            await image.close()
        if len(data) > MAX_BYTES:
            raise HTTPException(413, detail="FILE_TOO_LARGE")
        digest = hashlib.sha256(data).hexdigest()
        with lock:
            key, entry = cached(owner, "detect", client_request_id, digest)
            if entry:
                session(owner, entry["value"].detection_id)
                response.status_code = 200
                return envelope(request, entry["value"])
            if len(sessions) >= 64:
                raise HTTPException(429, detail="RATE_LIMITED", headers={"Retry-After": "30"})
            try:
                raw = MockDetector().detect(data, scenario=scenario)
            except MediaError as exc:
                raise HTTPException(exc.status, detail=exc.code) from None
            value = {k: raw[k] for k in ("status", "mapping_version", "thresholds", "image", "objects")}
            identifier, expires = uuid_factory(), now() + timedelta(seconds=ttl_seconds)
            value.update(detection_id=str(identifier), model_version="mock-four-class-v1", expires_at=expires.isoformat())
            result = DetectionSession.model_validate_json(json.dumps(value))
            sessions[identifier] = {"owner": owner, "result": result, "expires": expires, "deleted": False}
            remember(key, digest, result, expires)
            return envelope(request, result)

    @app.post("/v1/ai/recommendations", response_model=Envelope[RecommendationSession], status_code=201,
              responses={200: {"model": Envelope[RecommendationSession], "description": "Replay or non-READY decision"}})
    def recommendation(value: RecommendationInput, request: Request, response: Response, owner=Depends(user)):
        fail_scenario()
        digest = hashlib.sha256(value.model_dump_json().encode()).hexdigest()
        with lock:
            selection = None
            expires = now() + timedelta(seconds=ttl_seconds)
            if value.input_source == "DETECTION":
                row = session(owner, value.detection_id)
                selected = next((x for x in row["result"].objects if x.object_id == value.object_id), None)
                if selected is None:
                    raise HTTPException(422, detail="INVALID_AI_REFERENCE")
                selection = TrustedSelection(model_version=row["result"].model_version,
                    mapping_version=row["result"].mapping_version, class_names=CLASS_NAMES,
                    object=RawDetection.model_validate_json(selected.model_dump_json(include={"class_id", "raw_class", "confidence", "bbox"})))
                expires = row["expires"]
            key, entry = cached(owner, "recommend", value.client_request_id, digest)
            if entry:
                response.status_code = 200
                return envelope(request, entry["value"])
            try:
                result = decisions.evaluate(DecisionInput(context=value.context(), selection=selection))
            except DecisionError as exc:
                raise HTTPException(422, detail=exc.code) from None
            payload = {**result.model_dump(mode="json"), "recommendation_id": str(uuid_factory()),
                       "detection_id": str(value.detection_id) if value.detection_id else None,
                       "object_id": value.object_id, "context_revision": value.context_revision,
                       "confirmed_object_key": value.confirmed_object_key, "category": value.confirmed_category.value,
                       "expires_at": expires.isoformat()}
            output = RecommendationSession.model_validate_json(json.dumps(payload))
            remember(key, digest, output, expires)
            if output.status != "READY":
                response.status_code = 200
            return envelope(request, output)

    @app.delete("/v1/ai/detections/{detection_id}", status_code=204)
    def delete(detection_id: UUID, owner=Depends(user)):
        with lock:
            row = sessions.get(detection_id)
            if row is None or row["owner"] != owner:
                raise HTTPException(404, detail="DETECTION_NOT_FOUND")
            row["deleted"] = True
            return Response(status_code=204)

    install_security_schema(app, mock=True)
    return app
