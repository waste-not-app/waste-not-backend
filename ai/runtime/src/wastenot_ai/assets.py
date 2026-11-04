import hashlib
import json
from pathlib import Path

import httpx

ASSETS = [
    {"path": "artifacts/models/yolo11n.pt", "url": "https://github.com/ultralytics/assets/releases/download/v8.3.0/yolo11n.pt",
     "source": "https://docs.ultralytics.com/models/yolo11/", "license": "Ultralytics AGPL-3.0 / Enterprise",
     "sha256": "0ebbc80d4a7680d14987a577cd21342b65ecfd94632bd9a8da63ae6417644ee1",
     "purpose": "COCO pretrained inference baseline; not waste-specific fine-tuning"},
    {"path": "data/local/smoke/bottle.jpg", "url": "https://farm66.staticflickr.com/65535/33978196618_632623b4fc_z.jpg",
     "source": "https://github.com/pedropro/TACO", "license": "UNVERIFIED: TACO metadata image license=null",
     "image_id": 0, "taco_category": "Glass bottle", "redistribution_approved": False,
     "sha256": "2107981d12851a3205d008483ab8b5f150183c0939359bd9902f170aecd65dea",
     "purpose": "Public TACO sample for local inference smoke only; excluded from training and Git"},
]


def fetch_assets(directory: Path) -> dict:
    manifest = []
    with httpx.Client(timeout=30, follow_redirects=True, headers={"User-Agent": "WasteNot-AI-Prototype/0.1 (local academic object detection experiment)"}) as client:
        for asset in ASSETS:
            destination = directory / asset["path"]
            if not destination.is_file():
                destination.parent.mkdir(parents=True, exist_ok=True)
                part = destination.with_suffix(destination.suffix + ".part")
                try:
                    size = 0
                    with client.stream("GET", asset["url"]) as response:
                        response.raise_for_status()
                        with part.open("wb") as handle:
                            for chunk in response.iter_bytes():
                                size += len(chunk)
                                if size > 32 * 1024 * 1024:
                                    raise ValueError("Asset melebihi batas unduhan 32 MiB.")
                                handle.write(chunk)
                    part.replace(destination)
                finally:
                    part.unlink(missing_ok=True)
            data = destination.read_bytes()
            if asset.get("sha1") and hashlib.sha1(data).hexdigest() != asset["sha1"]:
                raise ValueError("Checksum gambar sumber tidak cocok.")
            if asset.get("sha256") and hashlib.sha256(data).hexdigest() != asset["sha256"]:
                raise ValueError("Checksum model tidak cocok.")
            manifest.append({**asset, "sha256": hashlib.sha256(data).hexdigest(), "size_bytes": len(data)})
    result = {"assets": manifest, "training_dataset": "NONE", "pilot_accuracy": "BELUM DIUKUR"}
    output = directory / "artifacts/asset-manifest.json"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    return result
