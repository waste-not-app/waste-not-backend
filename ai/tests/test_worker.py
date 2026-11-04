import asyncio
import threading

from fastapi.testclient import TestClient
import httpx

from wastenot_ai.media import MediaError
from wastenot_ai.worker import create_app

TOKEN = "fixture-service-token-at-least-24-characters"
HEADERS = {"Authorization":f"Bearer {TOKEN}","Content-Type":"image/png"}


class Detector:
    def __init__(self): self.calls = 0
    def detect(self,data):
        self.calls += 1
        if data == b"bad": raise MediaError("INVALID_IMAGE")
        return {"status":"MANUAL_REQUIRED","objects":[]}


def test_worker_auth_media_and_no_persistence():
    detector = Detector()
    with TestClient(create_app(detector, TOKEN)) as client:
        assert client.post("/internal/detect", content=b"x").status_code == 401
        assert client.post("/internal/detect",content=b"x",headers={"Authorization":f"Bearer {TOKEN}"}).status_code == 415
        assert detector.calls == 0
        assert client.post("/internal/detect",content=b"bad",headers=HEADERS).status_code == 422
        assert client.post("/internal/detect",content=b"ok",headers=HEADERS).json()["data"]["objects"] == []
        assert client.post("/internal/detect",content=b"x"*(5*1024*1024+1),headers=HEADERS).status_code == 413
        assert client.get("/health/ready",headers=HEADERS).status_code == 200
        assert client.get("/docs").status_code == 404


def test_timed_out_native_job_keeps_busy_until_it_finishes():
    async def scenario():
        started,release,finished = threading.Event(),threading.Event(),threading.Event()
        class BlockingDetector:
            def detect(self,data):
                started.set()
                release.wait(2)
                finished.set()
                return {"objects":[]}
        app = create_app(BlockingDetector(),TOKEN,timeout=.05)
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app),base_url="http://test") as client:
            try:
                first = await client.post("/internal/detect",content=b"image",headers=HEADERS)
                assert first.status_code == 504 and started.is_set()
                second = await client.post("/internal/detect",content=b"image",headers=HEADERS)
                assert second.status_code == 503
            finally:
                release.set()
                await asyncio.to_thread(finished.wait,1)
            await asyncio.sleep(.02)
            third = await client.post("/internal/detect",content=b"image",headers=HEADERS)
            assert third.status_code == 200
    asyncio.run(scenario())
