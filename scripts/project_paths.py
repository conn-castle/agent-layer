"""Project-relative paths shared by repository scripts."""

from pathlib import Path

SOURCE = Path(__file__).resolve().parents[1] / "internal/projectpaths/layout.env"
paths = dict(line.split("=", 1) for line in SOURCE.read_text(encoding="utf-8").splitlines()
             if line and not line.startswith("#"))
WORKING = Path(paths["AL_PROJECT_WORKING"])
CACHE = Path(paths["AL_PROJECT_CACHE"])
RUNS = Path(paths["AL_PROJECT_RUNS"])
