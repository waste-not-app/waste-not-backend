"""Stateless private recommendation module; Go owns sessions and idempotency."""
import asyncio
import hmac
import os
from threading import Lock

from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from .decisions import DecisionError, Decisions
from .http_api import BoundedHTTP, configure_errors, envelope, install_security_schema
from .providers import GeminiProvider
from .recommendation import load_guides
from .wire import API_VERSION, DecisionInput, Envelope, ObjectCatalog, RecommendationResult


def create_recommendation_app(*, token=None, provider=None, guides=None, demo_guides=False, timeout=18, today=None):
    service_token = token if token is not None else os.getenv("AI_RECOMMENDATION_TOKEN", "")
    if len(service_token) < 24:
        raise ValueError("AI_RECOMMENDATION_TOKEN wajib minimal 24 karakter.")
    decisions = Decisions(provider if provider is not None else GeminiProvider(),
                          guides if guides is not None else load_guides(), demo_guides=demo_guides, today=today)
    busy = Lock()
    bearer = HTTPBearer(auto_error=False)

    def authorize(credentials: HTTPAuthorizationCredentials | None = Depends(bearer)):
        if (credentials is None or credentials.scheme.lower() != "bearer" or
                not hmac.compare_digest(credentials.credentials.encode(), service_token.encode())):
            raise HTTPException(401, detail="UNAUTHENTICATED")

    app = FastAPI(title="WasteNot private AI decisions", version=API_VERSION,
                  docs_url=None, redoc_url=None, openapi_url=None, dependencies=[Depends(authorize)])
    app.add_middleware(BoundedHTTP)
    configure_errors(app)
    app.state.decisions = decisions

    @app.get("/health/ready")
    def ready(request: Request):
        return envelope(request, {"ready": True, "recommendation_version": "pilot-policy-06"})

    @app.get("/internal/v1/objects", response_model=Envelope[ObjectCatalog])
    def objects(request: Request):
        return envelope(request, decisions.catalog())

    @app.post("/internal/v1/recommendations", response_model=Envelope[RecommendationResult],
              responses={401: {"description": "Service authentication required"},
                         503: {"description": "Busy; Retry-After: 1"}, 504: {"description": "Deadline exceeded"}})
    async def recommendation(value: DecisionInput, request: Request):
        if not busy.acquire(blocking=False):
            raise HTTPException(503, detail="AI_BUSY", headers={"Retry-After": "1"})

        def evaluate():
            try:
                return decisions.evaluate(value)
            finally:
                busy.release()

        task = asyncio.create_task(asyncio.to_thread(evaluate))
        task.add_done_callback(lambda done: done.exception() if not done.cancelled() else None)
        try:
            async with asyncio.timeout(timeout):
                return envelope(request, await asyncio.shield(task))
        except DecisionError as exc:
            raise HTTPException(422, detail=exc.code) from None
        except TimeoutError:
            raise HTTPException(504, detail="AI_TIMEOUT") from None

    install_security_schema(app)
    return app
