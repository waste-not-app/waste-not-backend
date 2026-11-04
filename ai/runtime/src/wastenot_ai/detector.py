import hashlib
import importlib.metadata
import os
from pathlib import Path
from time import perf_counter

from .catalog import review_detections
from .contracts import RawDetection
from .media import decode_image


class YoloDetector:
    def __init__(self, model_path: Path, device: str = "cpu", input_shape: str = "adaptive"):
        if input_shape not in {"adaptive", "square"}:
            raise ValueError("Input shape harus adaptive atau square.")
        if not model_path.is_file():
            raise ValueError("Model tidak ditemukan. Jalankan perintah fetch-assets terlebih dahulu.")
        # Configure before importing Ultralytics: no telemetry or automatic requirements install.
        os.environ.setdefault("YOLO_CONFIG_DIR", str(Path(".cache-ai/ultralytics").resolve()))
        os.environ.setdefault("MPLCONFIGDIR", str(Path(".cache-ai/matplotlib").resolve()))
        os.environ["YOLO_AUTOINSTALL"] = "false"
        from ultralytics import YOLO, settings
        settings.update({"sync": False})
        self.model = YOLO(str(model_path), task="detect")
        self.device = device
        self.input_shape = input_shape
        self.labels = {int(k): v for k, v in self.model.names.items()}
        checksum = hashlib.sha256(model_path.read_bytes()).hexdigest()
        try:
            runtime_version = importlib.metadata.version("ultralytics")
        except importlib.metadata.PackageNotFoundError:
            runtime_version = importlib.metadata.version("ultralytics-opencv-headless")
        self.artifact = {
            "model_version": f"candidate-{checksum[:12]}" + ("-square640" if input_shape == "square" else ""),
            "sha256": checksum,
            "labels": self.labels, "ultralytics_version": runtime_version,
            "device": device, "input_size": 640,
            "input_shape": input_shape,
            "preprocessing": "EXIF transpose, RGB, Ultralytics letterbox",
            "license": "Ultralytics AGPL-3.0 / Enterprise; see provenance documentation",
            "scope": "drinking-waste-four-class-candidate" if self.labels == {
                0: "aluminium_can", 1: "glass_bottle", 2: "hdpe_bottle", 3: "plastic_bottle"
            } else "checkpoint-native-labels",
        }

    def detect(self, image_bytes: bytes) -> dict:
        started = perf_counter()
        image = decode_image(image_bytes)
        decoded_at = perf_counter()
        try:
            result = self.model.predict(image, imgsz=640, conf=0.25, device=self.device,
                                        rect=self.input_shape != "square",
                                        iou=.7, agnostic_nms=False, verbose=False, save=False, max_det=50)[0]
            objects = []
            if result.boxes is not None:
                for box in result.boxes:
                    class_id = int(box.cls.item())
                    objects.append(RawDetection(
                        class_id=class_id, raw_class=self.labels[class_id],
                        confidence=float(box.conf.item()),
                        bbox=tuple(float(x) for x in box.xyxyn[0].tolist()),
                    ))
            return {
                **review_detections(objects, self.labels, model_version=self.artifact["model_version"],
                                    model_sha256=self.artifact["sha256"]),
                "model": self.artifact,
                "image": {"width": image.width, "height": image.height},
                "timing_ms": {
                    "preprocess": round((decoded_at - started) * 1000, 2),
                    "inference_and_postprocess": round((perf_counter() - decoded_at) * 1000, 2),
                    "total": round((perf_counter() - started) * 1000, 2),
                },
            }
        finally:
            image.close()
