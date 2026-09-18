#!/usr/bin/env python3
"""Check local asset references in a built website without making network requests."""

import argparse
from html.parser import HTMLParser
from pathlib import Path
import sys
from urllib.parse import unquote, urljoin, urlsplit


class AssetParser(HTMLParser):
    """Collect browser asset URLs and the document's optional base URL."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.references = []
        self.base = None

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "base" and self.base is None:
            self.base = attrs.get("href")
        fields = []
        if tag in {"img", "script", "iframe", "source", "audio", "video", "embed"}:
            fields.append("src")
        if tag == "video":
            fields.append("poster")
        if tag == "object":
            fields.append("data")
        if tag == "link" and set(attrs.get("rel", "").lower().split()) & {
            "stylesheet", "icon", "apple-touch-icon", "preload", "modulepreload"
        }:
            fields.append("href")
        if tag == "meta" and attrs.get("property", attrs.get("name")) in {
            "og:image", "twitter:image"
        }:
            fields.append("content")
        for field in fields:
            if attrs.get(field):
                self.references.append((attrs[field], tag in {"iframe", "object"}))


def check(build, site_url):
    """Return actionable errors for missing assets referenced by generated HTML."""
    pages = sorted(build.rglob("*.html"))
    if not pages:
        return [f"{build}: no generated HTML found; build the website first"]
    origin = urlsplit(site_url)
    errors = []
    for page in pages:
        parser = AssetParser()
        parser.feed(page.read_text(encoding="utf-8"))
        relative = page.relative_to(build).as_posix()
        page_url = urljoin(site_url.rstrip("/") + "/", relative)
        base = urljoin(page_url, parser.base) if parser.base else page_url
        for reference, document in sorted(set(parser.references)):
            url = urlsplit(urljoin(base, reference))
            if url.scheme not in {"http", "https"} or url.netloc != origin.netloc:
                continue
            path = unquote(url.path)
            prefix = origin.path.rstrip("/") + "/"
            if not path.startswith(prefix):
                errors.append(f"{relative}: asset {reference!r} is outside site path {prefix}")
                continue
            target = (build / path[len(prefix):]).resolve()
            if not target.is_relative_to(build):
                errors.append(f"{relative}: asset {reference!r} escapes build directory")
            elif not target.is_file() and not (document and (target / "index.html").is_file()):
                errors.append(f"{relative}: missing asset {reference!r} (expected {target})")
    return errors


def main():
    """Validate the build supplied by the existing website build check."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("build", type=Path)
    parser.add_argument("--site-url", default="https://agent-layer.dev/")
    args = parser.parse_args()
    errors = check(args.build.resolve(), args.site_url)
    if errors:
        print("Website asset validation failed:\n" + "\n".join(errors), file=sys.stderr)
        return 1
    print("Website asset validation passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
