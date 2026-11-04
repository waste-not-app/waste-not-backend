"""Deterministically generate the AI handoff contract and real fixture examples."""
from datetime import datetime, timezone
from io import BytesIO
import json
from pathlib import Path
from uuid import UUID

from fastapi.testclient import TestClient
from PIL import Image

from .http_api import ERRORS
from .mock_api import MockDetector, create_mock_app
from .providers import FixtureProvider
from .recommendation_worker import create_recommendation_app
from .wire import DecisionInput
from .worker import create_app


def example_request(key="glass_bottle", category="GLASS", **updates):
    value = {"client_request_id": str(UUID(int=10)), "context_revision": 1,
             "input_source": "MANUAL", "confirmed_object": True,
             "confirmed_object_key": key, "confirmed_category": category,
             "condition": {"cleanliness": "CLEAN", "integrity": "INTACT", "reusable": "UNKNOWN"},
             "reuse_intention": False, "recycling_access": "YES"}
    value.update(updates)
    return value


def contract_artifacts() -> dict[str, dict]:
    fixed = datetime(2026, 10, 20, 8, tzinfo=timezone.utc)
    request_header = {"Authorization": "Bearer mock-user-a", "X-Request-ID": str(UUID(int=99))}
    counter = iter(range(1, 200))
    app = create_mock_app(clock=lambda: fixed, uuid_factory=lambda: UUID(int=next(counter)))
    private = create_recommendation_app(token="contract-export-placeholder-not-a-secret", provider=FixtureProvider(), today=lambda: fixed.date())
    worker = create_app(MockDetector(), "contract-export-placeholder-not-a-secret")
    artifacts = {"public.openapi.json": app.openapi(), "private-recommendation.openapi.json": private.openapi(),
                 "private-detection.openapi.json": worker.openapi(),
                 "errors.json": {code: {"http_status": value[0], "message": value[1], "retryable": value[2]}
                                 for code, value in sorted(ERRORS.items())}}
    cases = {
        "recycle": example_request(),
        "reuse": example_request("book", "PAPER", condition={"cleanliness": "CLEAN", "integrity": "INTACT", "reusable": "YES"}, reuse_intention=True),
        "reduce": example_request(recommendation_goal="PREVENTION"),
        "trash": example_request("toothbrush", "OTHER", condition={"cleanliness": "DIRTY", "integrity": "DAMAGED", "reusable": "NO"}, recycling_access="NO"),
        "needs-context": example_request("ceramic_cup", "OTHER", risk_flag=True),
        "hdpe-recycle": example_request("hdpe_bottle", "PLASTIC", condition={
            "cleanliness": "CLEAN", "integrity": "INTACT", "former_contents": "FOOD_DRINK"}),
        "hdpe-needs-context": example_request("hdpe_bottle", "PLASTIC"),
        "receiver-needs-context": example_request(recycling_access="UNKNOWN", allow_conditional=False),
        "conditional-cup": example_request("ceramic_cup", "OTHER", condition={}, reuse_intention=None, recycling_access="UNKNOWN"),
    }
    # Object keys and categories are checked by the actual decision module, not invented responses.
    from .catalog import load_catalog
    items = {row["object_key"]: row for row in load_catalog()["items"]}
    cases["needs-context"]["confirmed_category"] = items["ceramic_cup"]["category"]
    with TestClient(app) as client:
        artifacts["examples/catalog-response.json"] = client.get("/v1/ai/objects", headers=request_header).json()
        image = Image.new("RGB", (128, 128), "white")
        data = BytesIO()
        image.save(data, format="PNG")
        response = client.post("/v1/ai/detections", headers=request_header,
                               data={"client_request_id": str(UUID(int=2))}, files={"image": ("fixture.png", data.getvalue(), "image/png")})
        if response.status_code != 201:
            raise ValueError("Detection fixture failed")
        detected = response.json()
        artifacts["examples/detection-response.json"] = detected
        for index, (name, value) in enumerate(cases.items(), 20):
            value["client_request_id"] = str(UUID(int=index))
            artifacts[f"examples/{name}-request.json"] = value
            response = client.post("/v1/ai/recommendations", headers=request_header, json=value)
            if response.status_code not in (200, 201):
                raise ValueError(f"Recommendation fixture failed: {name}: {response.json()}")
            artifacts[f"examples/{name}-response.json"] = response.json()
        selected = detected["data"]["objects"][0]
        linked = example_request(input_source="DETECTION", detection_id=detected["data"]["detection_id"],
                                 object_id=selected["object_id"], client_request_id=str(UUID(int=50)))
        artifacts["examples/detected-recycle-request.json"] = linked
        artifacts["examples/detected-recycle-response.json"] = client.post("/v1/ai/recommendations", headers=request_header, json=linked).json()
        artifacts["examples/unauthenticated-response.json"] = client.get("/v1/ai/objects", headers={"X-Request-ID": str(UUID(int=99))}).json()
    for scenario in ("provider-unavailable", "timeout", "rate-limited", "no-detections", "uncertain"):
        counter = iter(range(1, 200))
        with TestClient(create_mock_app(scenario=scenario, clock=lambda: fixed, uuid_factory=lambda: UUID(int=next(counter)))) as client:
            if scenario in {"no-detections", "uncertain"}:
                response = client.post("/v1/ai/detections", headers=request_header,
                    data={"client_request_id": str(UUID(int=2))}, files={"image": ("fixture.png", data.getvalue(), "image/png")})
            else:
                response = client.post("/v1/ai/recommendations", headers=request_header, json=example_request())
            artifacts[f"examples/{scenario}-response.json"] = response.json()
    internal = {"context": {"object_key": "glass_bottle", "category": "GLASS",
                            "condition": {"cleanliness": "CLEAN", "integrity": "INTACT"},
                            "reuse_intention": False, "recycling_access": "YES"},
                "selection": {"model_version": "mock-four-class-v1", "mapping_version": detected["data"]["mapping_version"],
                              "class_names": list(MockDetector.labels.values()), "model_sha256": MockDetector.artifact["sha256"],
                              "object": {key: selected[key] for key in ("class_id", "raw_class", "confidence", "bbox")}}}
    DecisionInput.model_validate_json(json.dumps(internal))
    artifacts["examples/internal-recommendation-request.json"] = internal
    with TestClient(create_recommendation_app(token="contract-export-placeholder-not-a-secret", provider=FixtureProvider(), demo_guides=True, today=lambda: fixed.date())) as client:
        artifacts["examples/internal-recommendation-response.json"] = client.post("/internal/v1/recommendations", json=internal,
            headers={"Authorization": "Bearer contract-export-placeholder-not-a-secret", "X-Request-ID": str(UUID(int=99))}).json()
    rec = artifacts["public.openapi.json"]["paths"]["/v1/ai/recommendations"]["post"]
    rec["requestBody"]["content"]["application/json"]["examples"] = {
        name: {"summary": name, "value": artifacts[f"examples/{name}-request.json"]} for name in (*cases, "detected-recycle")}
    for status, names in (("201", ("reuse", "recycle", "reduce", "trash", "detected-recycle", "hdpe-recycle", "conditional-cup")),
                          ("200", ("needs-context", "hdpe-needs-context", "receiver-needs-context", "provider-unavailable")),
                          ("429", ("rate-limited",)), ("504", ("timeout",))):
        rec["responses"][status]["content"]["application/json"]["examples"] = {
            name: {"value": artifacts[f"examples/{name}-response.json"]} for name in names}
    detection = artifacts["public.openapi.json"]["paths"]["/v1/ai/detections"]["post"]
    detection["responses"]["201"]["content"]["application/json"]["example"] = detected
    rec = artifacts["private-recommendation.openapi.json"]["paths"]["/internal/v1/recommendations"]["post"]
    rec["requestBody"]["content"]["application/json"]["example"] = internal
    rec["responses"]["200"]["content"]["application/json"]["example"] = artifacts["examples/internal-recommendation-response.json"]
    return artifacts


def export_contracts(directory: Path, *, check=False):
    artifacts = contract_artifacts()
    stale = []
    for name, value in artifacts.items():
        path = directory / name
        text = json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True, allow_nan=False) + "\n"
        if check:
            if not path.is_file() or path.read_text("utf-8") != text:
                stale.append(name)
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding="utf-8")
    if stale:
        raise ValueError("Contract export is stale: " + ", ".join(stale))
    print(f"{'Checked' if check else 'Exported'} {len(artifacts)} contract artifacts.")
