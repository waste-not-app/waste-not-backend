#!/usr/bin/env python3
"""Operator smoke: create disposable accounts, exercise public AI/save, then delete only those accounts.

Requires authorized SSH administrator access to the Docker/PostgreSQL host for cleanup.
Uses one real Gemini generation; never prints or persists user/service credentials.
"""

import argparse
import datetime, json, secrets, subprocess, time, urllib.error, urllib.request, uuid, zoneinfo
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--base", default="https://pred-autoscaler.bccdev.id/wastenot/api")
parser.add_argument("--image", type=Path, required=True)
parser.add_argument(
    "--ssh", required=True, help="Authorized SSH host alias for scoped fixture cleanup"
)
parser.add_argument("--ssh-port", type=int, default=22)
parser.add_argument("--expected-model-version", default="broad-coco-b0f633193403")
parser.add_argument("--expected-model-sha256", default="b0f633193403d0f6c475f3ebd616ce55d47b738ff115d867efed92de193c687c")
parser.add_argument("--output", type=Path, required=True)
args = parser.parse_args()
BASE = args.base.rstrip("/")
args.output.parent.mkdir(parents=True, exist_ok=True)
users, checks = [], []


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


opener = urllib.request.build_opener(NoRedirect())


def call(path, method="GET", body=None, token=None, headers=None):
    headers = {"Content-Type": "application/json", **(headers or {})}
    if token:
        headers["Authorization"] = "Bearer " + token
    if body is not None and not isinstance(body, bytes):
        body = json.dumps(body).encode()
    request = urllib.request.Request(
        BASE + path, data=body, method=method, headers=headers
    )
    try:
        with opener.open(request, timeout=35) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read())


def register():
    status, value = call(
        "/v1/auth/register",
        "POST",
        {
            "name": "AI Pilot E2E",
            "email": "pilot-e2e-" + uuid.uuid4().hex + "@example.invalid",
            "password": secrets.token_urlsafe(24),
            "timezone": "Asia/Jakarta",
        },
    )
    assert status == 201, (status, value)
    users.append(str(uuid.UUID(value["data"]["user"]["id"])))
    return value["data"]["access_token"]


def upload(token):
    boundary = "wastenot" + uuid.uuid4().hex
    body = (
        f'--{boundary}\r\nContent-Disposition: form-data; name="client_request_id"\r\n\r\n{uuid.uuid4()}\r\n'
        f'--{boundary}\r\nContent-Disposition: form-data; name="image"; filename="sample.jpg"\r\nContent-Type: image/jpeg\r\n\r\n'
    ).encode()
    body += args.image.read_bytes() + f"\r\n--{boundary}--\r\n".encode()
    return call(
        "/v1/ai/detections",
        "POST",
        body,
        token,
        {"Content-Type": "multipart/form-data; boundary=" + boundary},
    )


def sql(query):
    result = subprocess.run(
        [
            "ssh",
            "-o",
            "BatchMode=yes",
            "-p",
            str(args.ssh_port),
            args.ssh,
            "sudo -n docker exec -i wastenot-db psql -At -U wastenot -d wastenot -v ON_ERROR_STOP=1",
        ],
        input=query.encode(),
        capture_output=True,
        check=True,
    )
    return result.stdout.decode().strip()


try:
    token, other = register(), register()
    status, value = call("/v1/metadata", token=token)
    assert (
        status == 200
        and value["data"]["capabilities"]["ai_scan"] is True
        and value["data"]["capabilities"]["ai_recommendation"] is True
    )
    checks.append("both AI capabilities enabled")
    status, value = call("/v1/ai/objects", token=token)
    assert status == 200, (status, value)
    items = value["data"]["items"]
    assert {
        item["object_key"] for item in items if item["guide_status"] == "PUBLISHED"
    } == {"pet_bottle", "hdpe_bottle", "aluminium_can", "cardboard"}
    assert "non-aerosol" in next(
        item["label"] for item in items if item["object_key"] == "aluminium_can"
    )
    checks.append("four reviewed pilot guides and scoped can label")
    manual = {
        "client_request_id": str(uuid.uuid4()),
        "input_source": "MANUAL",
        "context_revision": 1,
        "confirmed_object": True,
        "confirmed_object_key": "glass_bottle",
        "confirmed_category": "GLASS",
        "condition": {
            "cleanliness": "CLEAN",
            "integrity": "INTACT",
            "former_contents": "FOOD_DRINK",
        },
        "recycling_access": "YES",
    }
    status, value = call("/v1/ai/recommendations", "POST", manual, token)
    assert (
        status == 200
        and value["data"]["status"] == "UNAVAILABLE"
        and value["data"]["reason_code"] == "NO_REVIEWED_GUIDE"
    )
    assert value["data"]["recommended_method"] is None
    checks.append("unpublished object guide stays unavailable")
    manual.update(confirmed_object_key="pet_bottle", confirmed_category="PLASTIC")
    for change, expected_status, expected_code, label in [
        (
            {"condition": {"cleanliness": "CLEAN", "integrity": "INTACT"}},
            "NEEDS_CONTEXT",
            "CONTEXT_REQUIRED",
            "unknown contents request context",
        ),
        (
            {"risk_type": "CHEMICAL"},
            "UNAVAILABLE",
            "RISK_NOT_COVERED",
            "chemical risk gives no ordinary handling advice",
        ),
        (
            {"recycling_access": "NO"},
            "UNAVAILABLE",
            "NO_ALLOWED_METHOD",
            "no receiving access gives no guessed disposal method",
        ),
        (
            {"risk_flag": True},
            "NEEDS_CONTEXT",
            "RISK_DETAILS_REQUIRED",
            "unspecified risk requests clarification",
        ),
    ]:
        request = {**manual, **change, "client_request_id": str(uuid.uuid4())}
        status, value = call("/v1/ai/recommendations", "POST", request, token)
        assert (
            status == 200
            and value["data"]["status"] == expected_status
            and value["data"]["reason_code"] == expected_code
        ), (status, value)
        assert value["data"]["recommended_method"] is None
        checks.append(label)
    status, value = upload(token)
    assert status == 201, (status, value)
    detection = value["data"]
    assert detection["model_version"] == args.expected_model_version
    obj = next(
        item
        for item in detection["objects"]
        if item["raw_class"] == "bottle" and item["supported_for_review"]
    )
    assert (
        obj["category"] is None
        and obj["object_key"] is None
        and obj["requires_material_confirmation"] is True
    )
    checks.append("real YOLO detection requires material confirmation")
    request = {
        "client_request_id": str(uuid.uuid4()),
        "input_source": "DETECTION",
        "detection_id": detection["detection_id"],
        "object_id": obj["object_id"],
        "context_revision": 1,
        "confirmed_object": True,
        "confirmed_object_key": "pet_bottle",
        "confirmed_category": "PLASTIC",
        "condition": {
            "cleanliness": "CLEAN",
            "integrity": "INTACT",
            "former_contents": "FOOD_DRINK",
        },
        "recycling_access": "YES",
        "reuse_intention": False,
    }
    assert call("/v1/ai/recommendations", "POST", request, other)[0] == 404
    checks.append("foreign detection reference rejected")
    start = time.monotonic()
    status, value = call("/v1/ai/recommendations", "POST", request, token)
    elapsed = round((time.monotonic() - start) * 1000, 2)
    rec = value.get("data", {})
    if (
        status == 200
        and rec.get("status") == "UNAVAILABLE"
        and rec.get("reason_code") == "LLM_TIMEOUT"
    ):
        assert (
            rec["recommended_method"] is None
            and rec["provider"] in {"gemini", "rules"}
            and rec["prototype_only"] is False
        )
        checks.append("upstream timeout produces no handling method")
        status, value = call("/v1/ai/recommendations", "POST", request, token)
        assert status == 200 and value["data"] == rec
        checks.append("timeout replay returns the stored unavailable result")
        status, value = call("/v1/me", token=token)
        assert status == 200 and value["data"]["progress"]["xp"] == 0
        status, value = call("/v1/waste-logs", token=token)
        assert status == 200 and value["data"] == []
        checks.append("timeout and replay create no log or XP")
        report = {
            "count": len(checks),
            "passed": checks,
            "outcome": "controlled upstream timeout",
            "recommendation_elapsed_ms": elapsed,
            "complete_success_path": False,
        }
        args.output.write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report))
        raise SystemExit(3)
    if status != 201 or rec.get("status") != "READY":
        raise RuntimeError(
            json.dumps(
                {
                    "stage": "real_public_recommendation",
                    "http_status": status,
                    "status": rec.get("status"),
                    "reason_code": rec.get("reason_code"),
                    "error_code": value.get("error", {}).get("code"),
                    "elapsed_ms": elapsed,
                }
            )
        )
    assert (
        rec["provider"] in {"gemini", "rules"}
        and rec["prototype_only"] is False
        and rec["recommended_method"] == "RECYCLE"
        and rec["recommendation_version"] == "pilot-policy-09"
    )
    assert rec["recommendation_scope"] == "HANDLING" and all(
        item["version"] == 6 for item in rec["guide_refs"]
    )
    checks.append("evidence-backed recommendation with declared provider and no prototype")
    status, value = call("/v1/ai/recommendations", "POST", request, token)
    assert status == 200 and value["data"] == rec
    checks.append("recommendation replay reuses stored result")
    conflict = {**request, "context_revision": 2}
    assert call("/v1/ai/recommendations", "POST", conflict, token)[0] == 409
    checks.append("changed context with same request ID conflicts")
    status, value = call("/v1/me", token=token)
    assert status == 200 and value["data"]["progress"]["xp"] == 0
    status, value = call("/v1/waste-logs", token=token)
    assert status == 200 and value["data"] == []
    checks.append("detection and recommendation do not auto-save or award XP")
    today = datetime.datetime.now(zoneinfo.ZoneInfo("Asia/Jakarta")).date().isoformat()
    log = {
        "client_request_id": str(uuid.uuid4()),
        "category": "PLASTIC",
        "method": "RECYCLE",
        "quantity_kind": "WEIGHT",
        "weight_g": 250,
        "activity_date": today,
        "ai_provenance": {
            "recommendation_id": rec["recommendation_id"],
            "user_confirmed_object": True,
            "user_confirmed_method": True,
        },
    }
    status, value = call("/v1/waste-logs", "POST", log, token)
    assert status == 201 and value["data"]["progress"]["xp"] == 10, (status, value)
    logid = str(uuid.UUID(value["data"]["id"]))
    checks.append("explicit save links recommendation and awards 10 XP")
    status, value = call("/v1/waste-logs", "POST", log, token)
    assert (
        status == 200
        and value["data"]["id"] == logid
        and value["data"]["progress"]["xp"] == 10
    )
    checks.append("save replay creates no duplicate XP")
    log["client_request_id"] = str(uuid.uuid4())
    assert call("/v1/waste-logs", "POST", log, token)[0] == 409
    checks.append("same recommendation cannot fund a second log")
    query = (
        "SELECT accepted,final_category,final_method,snapshot->'result'->>'provider',snapshot->'result'->>'prototype_only',snapshot->'selection'->>'model_sha256' FROM ai_log_metadata WHERE waste_log_id='"
        + logid
        + "' AND user_id='"
        + users[0]
        + "';"
    )
    expected = "t|PLASTIC|RECYCLE|" + rec["provider"] + "|false|" + args.expected_model_sha256
    assert sql(query) == expected
    checks.append("database provenance retains real model hash and Gemini decision")
    status, value = call("/v1/statistics", token=token)
    assert status == 200 and value["data"]["summary"]["total_handled_weight_g"] == 250
    checks.append("live statistics include explicitly saved waste")
    assert call("/v1/waste-logs/" + logid, token=other)[0] == 404
    checks.append("waste log remains account scoped")
    assert (
        call(
            "/v1/waste-logs/" + logid,
            "DELETE",
            token=token,
            headers={"If-Match": '"1"'},
        )[0]
        == 204
    )
    status, value = call("/v1/me", token=token)
    assert status == 200 and value["data"]["progress"]["xp"] == 0
    checks.append("delete reverses derived XP")
    assert (
        call("/v1/ai/detections/" + detection["detection_id"], "DELETE", token=token)[0]
        == 204
    )
    assert sql(query) == expected
    checks.append("session deletion preserves linked audit snapshot")
    assert (
        call("/v1/auth/logout", "POST", token=token)[0] == 204
        and call("/v1/me", token=token)[0] == 401
    )
    checks.append("logout revokes session")
    report = {
        "count": len(checks),
        "passed": checks,
        "provider": rec["provider"],
        "model": "gemini-3.1-flash-lite" if rec["provider"] == "gemini" else None,
        "prototype_only": False,
        "recommendation_elapsed_ms": elapsed,
        "yolo_model_version": detection["model_version"],
        "note": "Single synthetic-context integration smoke, not material accuracy or latency benchmark.",
    }
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report))
finally:
    for uid in users:
        sql("DELETE FROM users WHERE id='" + uid + "';")
