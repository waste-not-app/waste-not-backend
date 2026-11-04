import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import statistics
import sys
from time import perf_counter

from dotenv import load_dotenv
from pydantic import ValidationError
import httpx

from .catalog import load_catalog, preserve_detected_risk
from .contracts import Context
from .media import MAX_BYTES, MediaError
from .providers import GEMINI_KEY_ENV_NAMES, FixtureProvider, GeminiProvider, load_gemini_api_keys
from .recommendation import load_guides
from .decisions import Decisions
from .wire import DecisionInput

AI_DIR = Path(__file__).resolve().parents[2]
ROOT = AI_DIR.parent


def emit(value: dict, output: Path | None = None):
    text = json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False)
    if output:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(text + "\n", encoding="utf-8")
    print(text)


def read_image(path: Path) -> bytes:
    with path.open("rb") as handle:
        return handle.read(MAX_BYTES + 1)


def detector_for(args):
    from .detector import YoloDetector
    return YoloDetector(args.model, args.device, args.input_shape)


def recommendation_for(args, context):
    provider = FixtureProvider() if args.provider == "fixture" else GeminiProvider()
    return Decisions(provider, load_guides(args.guides), demo_guides=args.demo_guides).evaluate(DecisionInput(context=context))


def run_pipeline(detector, data: bytes, context: Context, object_id: str, provider,
                 guides, *, demo_guides=False) -> dict:
    detection = detector.detect(data)
    selected = next((obj for obj in detection["objects"] if obj["object_id"] == object_id), None)
    if selected is None or not selected["supported_for_review"]:
        return {"detection": detection, "recommendation": None,
                "next_action": "INPUT_MANUAL_OR_RESCAN", "saved_log": False}
    context = preserve_detected_risk(context, selected)
    suggestion = Decisions(provider, guides, demo_guides=demo_guides).evaluate(DecisionInput(context=context))
    return {"detection": detection, "selected_object_id": object_id,
            "confirmed_context": context.model_dump(mode="json"),
            "recommendation": suggestion.model_dump(mode="json"),
            "next_action": "USER_CONFIRM_METHOD_AND_ENTER_QUANTITY" if suggestion.status == "READY" else "INPUT_MANUAL_OR_GUIDE",
            "saved_log": False}


def benchmark(args):
    detector = detector_for(args)
    paths = sorted(p for p in args.images.rglob("*") if p.suffix.lower() in {".jpg", ".jpeg", ".png"})
    if not paths:
        raise ValueError("Folder tidak berisi gambar JPEG/PNG.")
    records = []
    for path in paths:
        data = read_image(path)
        # Warm up once, separately from the timed repetitions.
        first = detector.detect(data)
        times = []
        for _ in range(args.repeat):
            start = perf_counter()
            detector.detect(data)
            times.append((perf_counter() - start) * 1000)
        records.append({"image_sha256": hashlib.sha256(data).hexdigest(),
                        "cold_or_first_ms": first["timing_ms"]["total"], "warm_ms": times,
                        "status": first["status"], "objects": first["objects"]})
    all_times = sorted(t for r in records for t in r["warm_ms"])
    from math import ceil
    import torch
    emit({"kind": "exploratory_inference_not_accuracy_evaluation", "sample_count": len(records),
          "model": detector.artifact, "environment": {"python": platform.python_version(),
          "platform": platform.platform(), "torch": torch.__version__, "cuda_available": torch.cuda.is_available()},
          "warm_p50_ms": round(statistics.median(all_times), 2),
          "warm_p95_ms": round(all_times[ceil(len(all_times) * .95) - 1], 2),
          "accuracy": "BELUM DIUKUR: belum ada holdout pilot berlabel.", "records": records}, args.output)


def main():
    if len(sys.argv) < 2 or sys.argv[1] not in {"serve-mock", "export-contracts"}:
        load_dotenv(AI_DIR / ".env", override=False)
    parser = argparse.ArgumentParser(description="WasteNot YOLO + Gemini: prototipe lokal, tanpa penyimpanan waste log.")
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("catalog")
    sub.add_parser("doctor")
    sub.add_parser("list-gemini-models")
    sub.add_parser("fetch-assets")
    export = sub.add_parser("export-contracts", help="Generate/check versioned OpenAPI and examples offline")
    export.add_argument("--check", action="store_true")
    export.add_argument("--output", type=Path, default=AI_DIR / "contracts/v1")
    mock = sub.add_parser("serve-mock", help="Public Go contract mock; no real JWT, YOLO or Gemini")
    mock.add_argument("--port", type=int, default=8094)
    mock.add_argument("--scenario", choices=["normal", "uncertain", "no-detections", "provider-unavailable", "rate-limited", "timeout"], default="normal")
    recommendation_worker = sub.add_parser("serve-recommendations", help="Private Python policy and Gemini HTTP adapter")
    recommendation_worker.add_argument("--port", type=int, default=8093)
    recommendation_worker.add_argument("--guides", type=Path)
    recommendation_worker.add_argument("--demo-guides", action="store_true")
    recommendation_worker.add_argument("--provider", choices=["gemini", "fixture"], default="gemini")
    detect = sub.add_parser("detect")
    detect.add_argument("image", type=Path)
    bench = sub.add_parser("benchmark")
    bench.add_argument("images", type=Path)
    bench.add_argument("--repeat", type=int, choices=range(1, 21), default=3)
    rec = sub.add_parser("recommend")
    pipe = sub.add_parser("pipeline")
    pipe.add_argument("image", type=Path)
    pipe.add_argument("--object-id", required=True)
    pipe.add_argument("--confirm-object", action="store_true", required=True,
                      help="Konfirmasi bahwa context JSON adalah koreksi/pilihan objek pengguna.")
    for command in (rec, pipe):
        command.add_argument("--context", type=Path, required=True)
        command.add_argument("--provider", choices=["gemini", "fixture"], default="gemini")
        command.add_argument("--guides", type=Path)
        command.add_argument("--demo-guides", action="store_true", help="Izinkan draf hanya untuk demonstrasi berlabel.")
    worker = sub.add_parser("serve-worker")
    worker.add_argument("--port", type=int, default=8091)
    demo = sub.add_parser("demo", help="Buka demo kamera lokal dengan pemilihan objek dan Gemini.")
    for command in (detect, bench, pipe, demo, worker):
        command.add_argument("--model", type=Path, default=AI_DIR / "artifacts/models/yolo11n.pt")
        command.add_argument("--device", default=os.getenv("AI_DEVICE", "cpu"))
        command.add_argument("--input-shape", choices=["adaptive", "square"], default=os.getenv("AI_INPUT_SHAPE", "adaptive"))
    for command in (detect, bench, rec, pipe):
        command.add_argument("--output", type=Path)
    demo.add_argument("--port",type=int,default=8092)
    demo.add_argument("--no-browser",action="store_true")
    demo.add_argument("--demo-guides",action="store_true",help="Izinkan draf dengan label demo.")
    demo.add_argument("--guides",type=Path)
    args = parser.parse_args()
    if args.command in {"demo", "serve-worker"}:
        # Explicit flags and env-based launchers select the same checkpoint.
        if any(arg == "--model" or arg.startswith("--model=") for arg in sys.argv):
            os.environ["AI_MODEL_PATH"] = str(args.model.resolve())
        os.environ["AI_DEVICE"] = args.device
        os.environ["AI_INPUT_SHAPE"] = args.input_shape
    try:
        if args.command == "export-contracts":
            from .openapi_export import export_contracts
            export_contracts(args.output, check=args.check)
        elif args.command == "serve-mock":
            for name in (*GEMINI_KEY_ENV_NAMES, "AI_SERVICE_TOKEN", "AI_RECOMMENDATION_TOKEN"):
                os.environ.pop(name, None)
            import uvicorn
            from .mock_api import create_mock_app
            print("PUBLIC CONTRACT MOCK: mock-user-a / mock-user-b; no YOLO/Gemini/DB.", flush=True)
            uvicorn.run(create_mock_app(scenario=args.scenario), host="127.0.0.1", port=args.port, access_log=False)
        elif args.command == "serve-recommendations":
            import uvicorn
            from .recommendation_worker import create_recommendation_app
            os.environ.pop("AI_SERVICE_TOKEN", None)
            provider = FixtureProvider() if args.provider == "fixture" else GeminiProvider()
            uvicorn.run(create_recommendation_app(provider=provider, guides=load_guides(args.guides),
                        demo_guides=args.demo_guides), host="127.0.0.1", port=args.port, access_log=False)
        elif args.command == "catalog":
            emit(load_catalog())
        elif args.command == "doctor":
            import importlib.metadata
            packages = {}
            for name in ("pydantic", "httpx", "Pillow", "ultralytics", "torch", "torchvision"):
                try:
                    packages[name] = importlib.metadata.version(name)
                except importlib.metadata.PackageNotFoundError:
                    packages[name] = None
            emit({"python": platform.python_version(), "packages": packages,
                  "gemini_key_configured": bool(load_gemini_api_keys()),
                  "gemini_key_count": len(load_gemini_api_keys()),
                  "gemini_model": os.getenv("GEMINI_MODEL") or None,
                  "model_downloaded": (AI_DIR / "artifacts/models/yolo11n.pt").is_file()})
        elif args.command == "fetch-assets":
            from .assets import fetch_assets
            emit(fetch_assets(AI_DIR))
        elif args.command == "list-gemini-models":
            keys = load_gemini_api_keys()
            if not keys:
                raise ValueError("Isi API key Gemini di ai/.env terlebih dahulu.")
            with httpx.Client(timeout=15, follow_redirects=False) as client:
                response = client.get("https://generativelanguage.googleapis.com/v1beta/models",
                                      headers={"x-goog-api-key": keys[0]})
                if response.status_code != 200:
                    raise ValueError("Gagal membaca daftar model Gemini; periksa akses/kuota API.")
                emit({"models": [m["name"].removeprefix("models/") for m in response.json().get("models", [])
                                 if "generateContent" in m.get("supportedGenerationMethods", [])],
                      "note": "Periksa dukungan structured output sebelum mengisi GEMINI_MODEL."})
        elif args.command == "detect":
            emit(detector_for(args).detect(read_image(args.image)), args.output)
        elif args.command == "benchmark":
            benchmark(args)
        elif args.command == "recommend":
            context = Context.model_validate_json(args.context.read_text("utf-8"))
            emit(recommendation_for(args, context).model_dump(mode="json"), args.output)
        elif args.command == "pipeline":
            context = Context.model_validate_json(args.context.read_text("utf-8"))
            provider = FixtureProvider() if args.provider == "fixture" else GeminiProvider()
            emit(run_pipeline(detector_for(args), read_image(args.image), context, args.object_id,
                              provider, load_guides(args.guides), demo_guides=args.demo_guides), args.output)
        elif args.command == "demo":
            import uvicorn
            import webbrowser
            from threading import Timer
            from .demo import create_demo_app
            url = f"http://127.0.0.1:{args.port}"
            def open_demo():
                if not args.no_browser:
                    timer = Timer(.8,lambda:webbrowser.open(url))
                    timer.daemon = True
                    timer.start()
            print(f"Demo kamera: {url} | Ctrl+C untuk berhenti.")
            uvicorn.run(create_demo_app(demo_guides=args.demo_guides,guides_path=args.guides,on_ready=open_demo),
                        host="127.0.0.1",port=args.port,access_log=False)
        elif args.command == "serve-worker":
            # The inference worker must not inherit the CLI's recommendation secret.
            for name in GEMINI_KEY_ENV_NAMES:
                os.environ.pop(name, None)
            os.environ.pop("AI_RECOMMENDATION_TOKEN", None)
            import uvicorn
            from .worker import create_app
            uvicorn.run(create_app(), host="127.0.0.1", port=args.port, access_log=False)
    except (ValueError, OSError, ImportError, ValidationError, MediaError, httpx.HTTPError) as exc:
        # No provider exception text, request body, key or image is ever printed.
        if args.command in {"recommend", "pipeline", "list-gemini-models"}:
            message = "Periksa context, konfigurasi Gemini, akses jaringan dan file panduan. Jalankan doctor untuk status konfigurasi."
        else:
            message = str(exc) if isinstance(exc, MediaError) else "Periksa file input, model dan dependency; lihat README ai."
        print(json.dumps({"error": message}, ensure_ascii=False), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
