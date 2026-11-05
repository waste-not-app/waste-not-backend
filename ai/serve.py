#!/usr/bin/env python3
import argparse
from contextlib import nullcontext
import hashlib
import io
import os
from pathlib import Path


def application(kind):
    if kind == 'detection':
        for name in ('GEMINI_API_KEY', *(f'GEMINI_API_KEY_{i}' for i in range(1, 6)), 'AI_RECOMMENDATION_TOKEN'):
            os.environ.pop(name, None)
        model = Path(os.environ['AI_MODEL_PATH'])
        if hashlib.sha256(model.read_bytes()).hexdigest() != os.environ['AI_MODEL_SHA256']:
            raise ValueError('Model checksum does not match the configured artifact')
        import torch
        torch.set_num_threads(1)
        torch.set_num_interop_threads(1)
        from PIL import Image
        from wastenot_ai.detector import YoloDetector
        from wastenot_ai.worker import create_app
        detector = YoloDetector(model, device='cpu', input_shape='adaptive')
        blank = io.BytesIO()
        Image.new('RGB', (64, 64)).save(blank, 'PNG')
        detector.detect(blank.getvalue())
        return create_app(detector=detector)
    os.environ.pop('AI_SERVICE_TOKEN', None)
    from wastenot_ai.providers import GeminiProvider
    from wastenot_ai.recommendation import load_guides
    from wastenot_ai.recommendation_worker import create_recommendation_app
    guide_path = os.environ.get('AI_GUIDES_PATH')
    provider = GeminiProvider()
    # Leave time for the worker envelope before Android's 20-second read and Go's 23-second header deadline.
    if provider.timeout > 17:
        raise ValueError('GEMINI_TIMEOUT_SECONDS must not exceed 17 for the Android/Go API deadlines')
    return create_recommendation_app(provider=provider, timeout=provider.timeout + 1,
                                     guides=load_guides(Path(guide_path) if guide_path else None),
                                     demo_guides=False)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('service', choices=('detection', 'recommendation'))
    args = parser.parse_args()
    import uvicorn
    backend = nullcontext()
    if args.service == 'detection':
        import torch
        if torch.backends.cpu.get_cpu_capability() in {'DEFAULT', 'NO AVX'}:
            # The VM CPU cannot run NNPACK; avoid repeating unsupported-kernel initialization for every layer.
            backend = torch.backends.nnpack.flags(enabled=False)
    with backend:
        app = application(args.service)
        uvicorn.run(app, host='127.0.0.1', port=8091 if args.service == 'detection' else 8093,
                    access_log=False, log_level='warning', workers=1)


if __name__ == '__main__':
    main()
