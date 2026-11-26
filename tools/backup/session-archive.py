"""Archive the pinned Hermes session files without copying a live SQLite WAL."""
import os
import sqlite3
import sys
import tarfile
import tempfile
import time
from contextlib import closing
from pathlib import Path


def snapshot(source, destination):
    deadline = time.monotonic() + 60

    def check_progress(_status, _remaining, _total):
        if time.monotonic() > deadline:
            raise TimeoutError("Hermes SQLite snapshot timed out")

    with closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True)) as db:
        with closing(sqlite3.connect(destination)) as copy:
            db.backup(copy, pages=512, progress=check_progress)


def capture(root):
    with tempfile.TemporaryDirectory() as temporary:
        with tarfile.open(fileobj=sys.stdout.buffer, mode="w|gz") as archive:
            for directory, _, files in os.walk(root):
                for name in sorted(files):
                    path = Path(directory) / name
                    local = path.relative_to(root)
                    if path.is_symlink() or not path.is_file():
                        continue
                    if name == "state.db":
                        copy = Path(temporary) / "snapshot.db"
                        snapshot(path, copy)
                        archive.add(copy, arcname=str(local))
                        copy.unlink()
                    elif name.startswith("state.db-"):
                        continue
                    elif "sessions" in local.parts[:-1]:
                        archive.add(path, arcname=str(local))


def restore(root):
    with tarfile.open(fileobj=sys.stdin.buffer, mode="r|gz") as archive:
        for entry in archive:
            if not entry.isfile():
                raise ValueError(f"Unexpected session archive entry: {entry.name}")
            archive.extract(entry, root, filter="data")


os.umask(0o077)
if __name__ == "__main__":
    {"capture": capture, "restore": restore}[sys.argv[1]](Path("/var/tmp"))
