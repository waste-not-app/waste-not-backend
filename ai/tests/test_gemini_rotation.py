from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
from email.utils import format_datetime
import json

import httpx
import pytest

from wastenot_ai.providers import GEMINI_KEY_ENV_NAMES, GeminiProvider, load_gemini_api_keys
from wastenot_ai.recommendation import ProviderError


KEYS = [f"test-only-key-{i}" for i in range(1, 6)]


def success():
    return httpx.Response(200, json={"candidates":[{"finishReason":"STOP", "content":{"parts":[{"text":"{}"}]}}]})


def test_five_keys_rotate_round_robin_without_leaking_keys():
    used = []
    def handler(request):
        used.append(request.headers["x-goog-api-key"])
        assert all(key not in str(request.url) and key not in request.content.decode() for key in KEYS)
        return success()
    provider = GeminiProvider(api_keys=KEYS, model="test", transport=httpx.MockTransport(handler))
    for _ in range(12):
        assert provider.generate({"object_key":"book"}) == "{}"
    assert used == KEYS * 2 + KEYS[:2]


def test_concurrent_requests_preserve_balanced_rotation():
    used = []
    def handler(request):
        used.append(request.headers["x-goog-api-key"])
        return success()
    provider = GeminiProvider(api_keys=KEYS, model="test", transport=httpx.MockTransport(handler))
    with ThreadPoolExecutor(max_workers=5) as executor:
        assert list(executor.map(lambda _:provider.generate({}), range(20))) == ["{}"] * 20
    assert all(used.count(key) == 4 for key in KEYS)


def test_rate_limited_key_is_skipped_until_retry_after_expires(monkeypatch):
    now = [100.0]
    monkeypatch.setattr("wastenot_ai.providers.monotonic", lambda:now[0])
    used = []
    def handler(request):
        key = request.headers["x-goog-api-key"]
        used.append(key)
        if key == KEYS[0] and used.count(key) == 1:
            return httpx.Response(429, headers={"Retry-After":"120"})
        return success()
    provider = GeminiProvider(api_keys=KEYS[:2], model="test", transport=httpx.MockTransport(handler))
    provider.generate({})
    provider.generate({})
    now[0] = 219
    provider.generate({})
    now[0] = 221
    provider.generate({})
    assert used == [KEYS[0],KEYS[1],KEYS[1],KEYS[1],KEYS[0]]


@pytest.mark.parametrize("response", [
    httpx.Response(429, json={"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo", "retryDelay":"180.5s"}]}}),
    httpx.Response(429, headers={"Retry-After":format_datetime(datetime.now(timezone.utc) + timedelta(seconds=190), usegmt=True)}),
])
def test_provider_retry_hints_extend_cooldown(response, monkeypatch):
    now = [0.0]
    monkeypatch.setattr("wastenot_ai.providers.monotonic", lambda:now[0])
    used = []
    def handler(request):
        used.append(request.headers["x-goog-api-key"])
        return response if len(used) == 1 else success()
    provider = GeminiProvider(api_keys=KEYS[:1], model="test", transport=httpx.MockTransport(handler))
    with pytest.raises(ProviderError, match="LLM_RATE_LIMITED"):
        provider.generate({})
    now[0] = 179
    with pytest.raises(ProviderError, match="LLM_RATE_LIMITED"):
        provider.generate({})
    assert len(used) == 1
    now[0] = 200
    assert provider.generate({}) == "{}"


@pytest.mark.parametrize("body,headers", [(b"not JSON",{}),(b"[]",{"Retry-After":"garbage"}),
                                         (b'{}',{"Retry-After":"NaN"})])
def test_all_keys_limited_stop_after_one_attempt_each_and_no_calls_during_cooldown(body, headers, monkeypatch):
    now = [0.0]
    monkeypatch.setattr("wastenot_ai.providers.monotonic", lambda:now[0])
    used = []
    def handler(request):
        used.append(request.headers["x-goog-api-key"])
        return httpx.Response(429,content=body,headers=headers)
    provider = GeminiProvider(api_keys=KEYS, model="test", transport=httpx.MockTransport(handler))
    for _ in range(3):
        with pytest.raises(ProviderError, match="LLM_RATE_LIMITED") as exc:
            provider.generate({})
        assert all(key not in str(exc.value) for key in KEYS)
    assert used == KEYS
    now[0] = 61
    with pytest.raises(ProviderError, match="LLM_RATE_LIMITED"):
        provider.generate({})
    assert used == KEYS * 2


@pytest.mark.parametrize("status", [401,403])
def test_invalid_or_forbidden_key_is_disabled_until_restart(status):
    used = []
    def handler(request):
        key = request.headers["x-goog-api-key"]
        used.append(key)
        return httpx.Response(status) if key == KEYS[0] else success()
    provider = GeminiProvider(api_keys=KEYS[:2], model="test", transport=httpx.MockTransport(handler))
    provider.generate({})
    provider.generate({})
    assert used == [KEYS[0],KEYS[1],KEYS[1]]


@pytest.mark.parametrize("failure,expected", [("timeout","LLM_TIMEOUT"),("server","LLM_UNAVAILABLE"),
                                             ("malformed","LLM_UNAVAILABLE")])
def test_ambiguous_or_completed_failure_does_not_trigger_extra_generation(failure, expected):
    used = []
    def handler(request):
        used.append(request.headers["x-goog-api-key"])
        if failure == "timeout":
            raise httpx.ReadTimeout("private detail",request=request)
        return httpx.Response(503) if failure == "server" else httpx.Response(200,content=b"invalid-json")
    provider = GeminiProvider(api_keys=KEYS, model="test", transport=httpx.MockTransport(handler))
    with pytest.raises(ProviderError, match=expected):
        provider.generate({})
    assert used == KEYS[:1]


def test_failover_shares_one_timeout_budget(monkeypatch):
    now = [0.0]
    monkeypatch.setattr("wastenot_ai.providers.monotonic", lambda:now[0])
    timeouts = []
    def handler(request):
        timeouts.append(request.extensions["timeout"]["read"])
        now[0] += 10
        return httpx.Response(429)
    provider = GeminiProvider(api_keys=KEYS, model="test",timeout=15,transport=httpx.MockTransport(handler))
    with pytest.raises(ProviderError, match="LLM_TIMEOUT"):
        provider.generate({})
    assert timeouts == [15,5]


def test_completed_response_after_deadline_is_rejected(monkeypatch):
    now = [0.0]
    monkeypatch.setattr("wastenot_ai.providers.monotonic",lambda:now[0])
    def handler(request):
        now[0] = 30
        return success()
    provider = GeminiProvider(api_keys=KEYS,model="test",timeout=15,transport=httpx.MockTransport(handler))
    with pytest.raises(ProviderError,match="LLM_TIMEOUT"):
        provider.generate({})


def test_absolute_deadline_cancels_slow_response_without_key_retry():
    import asyncio
    from time import perf_counter
    used = []
    async def handler(request):
        used.append(request.headers["x-goog-api-key"])
        await asyncio.sleep(10)
        return success()
    provider = GeminiProvider(api_keys=KEYS,model="test",timeout=.05,transport=httpx.MockTransport(handler))
    started = perf_counter()
    with pytest.raises(ProviderError,match="LLM_TIMEOUT"):
        provider.generate({})
    assert perf_counter() - started < 2
    assert used == KEYS[:1]


def test_numbered_env_keys_precede_legacy_trim_deduplicate_and_allow_gaps(monkeypatch):
    for name in GEMINI_KEY_ENV_NAMES:
        monkeypatch.delenv(name,raising=False)
    monkeypatch.setenv("GEMINI_API_KEY","legacy-test")
    assert load_gemini_api_keys() == ["legacy-test"]
    monkeypatch.setenv("GEMINI_API_KEY_1","  first-test  ")
    monkeypatch.setenv("GEMINI_API_KEY_3","first-test")
    monkeypatch.setenv("GEMINI_API_KEY_5","fifth-test")
    assert load_gemini_api_keys() == ["first-test","fifth-test"]
    provider = GeminiProvider(model="test")
    assert provider.key_count == 2
    assert GeminiProvider(api_key="",model="test").key_count == 0


def test_worker_cli_removes_all_gemini_credentials(monkeypatch):
    import os
    import sys
    import uvicorn
    from wastenot_ai.cli import main
    monkeypatch.setattr(sys,"argv",["wastenot-ai","serve-worker"])
    for name in GEMINI_KEY_ENV_NAMES:
        monkeypatch.setenv(name,"test-only-secret")
    monkeypatch.setattr("wastenot_ai.worker.create_app",lambda:object())
    def run(*args,**kwargs):
        assert all(name not in os.environ for name in GEMINI_KEY_ENV_NAMES)
    monkeypatch.setattr(uvicorn,"run",run)
    assert main() == 0


def test_doctor_reports_numbered_key_count_without_values(monkeypatch,capsys):
    import sys
    from wastenot_ai.cli import main
    monkeypatch.setattr(sys,"argv",["wastenot-ai","doctor"])
    for name in GEMINI_KEY_ENV_NAMES:
        monkeypatch.setenv(name,"")
    for i,key in enumerate(KEYS,1):
        monkeypatch.setenv(f"GEMINI_API_KEY_{i}",key)
    assert main() == 0
    output = capsys.readouterr().out
    assert json.loads(output)["gemini_key_count"] == 5
    assert all(key not in output for key in KEYS)
