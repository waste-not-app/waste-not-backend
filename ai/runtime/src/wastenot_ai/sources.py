from importlib.resources import files
import json
from urllib.parse import urlparse


def load_sources() -> dict[str, dict]:
    rows = json.loads(files("wastenot_ai").joinpath("data/source_registry.json").read_text("utf-8"))
    return {row["url"]:row for row in rows}


def describe_sources(urls: list[str]) -> list[dict]:
    registry = load_sources()
    return [registry.get(url, {"url":url,"title":urlparse(url).hostname or url,
                              "publisher":"Sumber panduan kustom","region":"Belum dicatat",
                              "checked_at":None,"scope_note":"Metadata sumber belum dicatat dalam registri prototipe."})
            for url in urls]
