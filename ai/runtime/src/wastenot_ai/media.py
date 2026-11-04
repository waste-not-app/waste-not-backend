from io import BytesIO
import warnings

from PIL import Image, ImageOps, UnidentifiedImageError

MAX_BYTES = 5 * 1024 * 1024
MAX_PIXELS = 12_000_000
MAX_SIDE = 4096


class MediaError(ValueError):
    def __init__(self, code: str, status: int = 422):
        self.code, self.status = code, status
        super().__init__(code)


def decode_image(data: bytes) -> Image.Image:
    if len(data) > MAX_BYTES:
        raise MediaError("FILE_TOO_LARGE", 413)
    try:
        with warnings.catch_warnings():
            warnings.simplefilter("error", Image.DecompressionBombWarning)
            with Image.open(BytesIO(data)) as source:
                if source.format not in {"JPEG", "PNG"}:
                    raise MediaError("UNSUPPORTED_MEDIA_TYPE", 415)
                width, height = source.size
                if width * height > MAX_PIXELS or max(width, height) > MAX_SIDE:
                    raise MediaError("IMAGE_DIMENSIONS_EXCEEDED")
                source.load()
                oriented = ImageOps.exif_transpose(source).convert("RGB")
                # Rebuild from pixels so EXIF and ancillary metadata cannot escape.
                return Image.frombytes("RGB", oriented.size, oriented.tobytes())
    except MediaError:
        raise
    except (UnidentifiedImageError, OSError, ValueError, Image.DecompressionBombError,
            Image.DecompressionBombWarning) as exc:
        raise MediaError("INVALID_IMAGE") from exc
