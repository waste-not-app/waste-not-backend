"""HTTP envelope, bounded bodies and redacted errors for integration adapters."""
import asyncio
from uuid import UUID, uuid4

from fastapi import HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from .wire import API_VERSION, ErrorDetail, ErrorEnvelope

ERRORS = {
    "UNAUTHENTICATED": (401, "Autentikasi diperlukan.", False),
    "NOT_FOUND": (404, "Data tidak ditemukan.", False),
    "DETECTION_NOT_FOUND": (404, "Sesi deteksi tidak ditemukan.", False),
    "AI_SESSION_EXPIRED": (410, "Sesi AI berakhir; lakukan scan kembali.", False),
    "IDEMPOTENCY_CONFLICT": (409, "ID permintaan sudah dipakai untuk isi yang berbeda.", False),
    "VALIDATION_ERROR": (422, "Periksa kembali isianmu.", False),
    "INVALID_CONTEXT": (422, "Objek dan kategori tidak cocok dengan katalog.", False),
    "INVALID_AI_REFERENCE": (422, "Referensi hasil deteksi tidak cocok.", False),
    "OBJECT_REQUIRES_MANUAL": (422, "Pilih objek melalui input manual.", False),
    "FILE_TOO_LARGE": (413, "Ukuran gambar maksimal 5 MiB.", False),
    "REQUEST_TOO_LARGE": (413, "Ukuran permintaan melampaui batas layanan.", False),
    "UNSUPPORTED_MEDIA_TYPE": (415, "Gunakan JPEG atau PNG.", False),
    "INVALID_IMAGE": (422, "Gambar tidak dapat dibaca.", False),
    "IMAGE_DIMENSIONS_EXCEEDED": (422, "Gambar melampaui batas dimensi.", False),
    "RATE_LIMITED": (429, "Batas permintaan tercapai; tunggu sebelum mencoba lagi.", True),
    "AI_BUSY": (503, "Layanan AI sedang sibuk.", True),
    "AI_UNAVAILABLE": (503, "Layanan AI belum tersedia.", True),
    "INVALID_AI_RESPONSE": (503, "Respons layanan AI tidak dapat digunakan.", True),
    "AI_TIMEOUT": (504, "Layanan AI melewati batas waktu.", True),
    "AI_DISABLED": (503, "Fitur AI belum diaktifkan.", False),
    "METHOD_NOT_ALLOWED": (405, "Metode HTTP tidak didukung.", False),
}


def error_response(code, request_id, *, status=None, fields=None, headers=None):
    default_status, message, retryable = ERRORS.get(code, ERRORS["AI_UNAVAILABLE"])
    envelope = ErrorEnvelope(request_id=request_id, error=ErrorDetail(
        code=code, message=message, fields=fields or {}, retryable=retryable))
    return JSONResponse(envelope.model_dump(mode="json"), status_code=status or default_status, headers=headers)


def configure_errors(app):
    @app.exception_handler(RequestValidationError)
    async def invalid(request, exc):
        fields = {".".join(str(x) for x in err["loc"] if x != "body"): "Isian tidak valid."
                  for err in exc.errors()}
        return error_response("VALIDATION_ERROR", request.state.request_id, fields=fields)

    # Starlette also uses this base class for unknown paths and unsupported methods.
    from starlette.exceptions import HTTPException as StarletteHTTPException

    @app.exception_handler(StarletteHTTPException)
    async def failure(request, exc):
        code = exc.detail if isinstance(exc.detail, str) and exc.detail in ERRORS else {
            404: "NOT_FOUND", 405: "METHOD_NOT_ALLOWED", 413: "FILE_TOO_LARGE", 422: "VALIDATION_ERROR"
        }.get(exc.status_code, "AI_UNAVAILABLE")
        return error_response(code, request.state.request_id, status=exc.status_code, headers=exc.headers)

    @app.exception_handler(Exception)
    async def unexpected(request, exc):
        return error_response("AI_UNAVAILABLE", request.state.request_id)


class BoundedHTTP:
    """Buffer at most the configured limit before FastAPI parses JSON or multipart."""
    def __init__(self, app, maximum=16_384, mock=False):
        self.app, self.maximum, self.mock = app, maximum, mock

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        headers = dict(scope.get("headers", []))
        try:
            request_id = UUID(headers.get(b"x-request-id", b"").decode())
        except (ValueError, UnicodeError):
            request_id = uuid4()
        scope.setdefault("state", {})["request_id"] = request_id
        body = bytearray()
        try:
            async with asyncio.timeout(5):
                while self.maximum is not None:
                    message = await receive()
                    if message["type"] == "http.disconnect":
                        return
                    chunk = message.get("body", b"")
                    if len(body) + len(chunk) > self.maximum:
                        response = error_response("FILE_TOO_LARGE" if self.mock else "REQUEST_TOO_LARGE", request_id, headers={
                            "X-Request-ID": str(request_id), "Cache-Control": "no-store",
                            **({"X-WasteNot-Mock": "true"} if self.mock else {})})
                        await response(scope, receive, send)
                        return
                    body.extend(chunk)
                    if not message.get("more_body", False):
                        break
        except TimeoutError:
            response = error_response("AI_TIMEOUT", request_id, headers={
                "X-Request-ID": str(request_id), "Cache-Control": "no-store",
                **({"X-WasteNot-Mock": "true"} if self.mock else {})})
            await response(scope, receive, send)
            return
        sent = False

        async def buffered():
            nonlocal sent
            if self.maximum is None:
                return await receive()
            if not sent:
                sent = True
                return {"type": "http.request", "body": bytes(body), "more_body": False}
            return await receive()

        started = False

        async def outgoing(message):
            nonlocal started
            if message["type"] == "http.response.start":
                started = True
                extra = [(b"x-request-id", str(request_id).encode()), (b"cache-control", b"no-store")]
                if self.mock:
                    extra.append((b"x-wastenot-mock", b"true"))
                message["headers"] = list(message.get("headers", [])) + extra
            await send(message)

        try:
            await self.app(scope, buffered, outgoing)
        except Exception:
            if started:
                return
            response = error_response("AI_UNAVAILABLE", request_id)
            await response(scope, receive, outgoing)


def envelope(request: Request, value):
    return {"api_version": API_VERSION, "request_id": request.state.request_id, "data": value}


def install_security_schema(app, *, mock=False):
    original = app.openapi

    def schema():
        doc = original()
        doc.setdefault("components", {}).setdefault("securitySchemes", {})["BearerAuth"] = {
            "type": "http", "scheme": "bearer",
            "description": "MOCK ONLY: mock-user-a or mock-user-b" if mock else "Private Go-to-Python credential; never send to Android"}
        doc["security"] = [{"BearerAuth": []}]
        doc["info"]["x-api-version"] = API_VERSION
        if mock:
            doc["info"]["x-implementation-status"] = "public contract mock; Go not implemented here"
        for operations in doc["paths"].values():
            for operation in operations.values():
                if not isinstance(operation, dict) or "responses" not in operation:
                    continue
                operation["security"] = [{"BearerAuth": []}]
                for status in (401, 404, 409, 410, 413, 415, 422, 429, 503, 504):
                    response = operation["responses"].setdefault(str(status), {"description": "AI error; see errors.json"})
                    response["content"] = {"application/json": {"schema": {"$ref": "#/components/schemas/ErrorEnvelope"}}}
                for status in (429, 503):
                    operation["responses"][str(status)]["headers"] = {"Retry-After": {
                        "description": "Wait at least this many seconds when present; retry explicitly with the same client_request_id.",
                        "schema": {"type": "integer", "minimum": 1}}}
        raw = ErrorEnvelope.model_json_schema(ref_template="#/components/schemas/{model}")
        definitions = raw.pop("$defs", {})
        doc["components"].setdefault("schemas", {}).update(definitions)
        doc["components"]["schemas"]["ErrorEnvelope"] = raw
        doc["components"]["schemas"]["ErrorDetail"]["properties"]["code"]["enum"] = sorted(ERRORS)
        return doc

    app.openapi = schema
