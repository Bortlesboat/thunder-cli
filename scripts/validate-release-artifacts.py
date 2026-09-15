#!/usr/bin/env python3

import argparse
import json
import re
import sys
from pathlib import Path


def read_checksums(path: Path) -> dict[str, str]:
    entries = {}
    for number, line in enumerate(path.read_text().splitlines(), 1):
        if not line.strip():
            continue
        match = re.fullmatch(r"([0-9a-fA-F]{64})[ \t]+\*?([A-Za-z0-9][A-Za-z0-9._+-]*)", line)
        if not match:
            raise ValueError(f"{path}:{number}: malformed checksum entry")
        checksum, name = match.groups()
        if name in entries:
            raise ValueError(f"{path}:{number}: duplicate checksum for {name}")
        entries[name] = checksum.lower()
    if not entries:
        raise ValueError(f"{path}: checksum file is empty")
    return entries


def is_archive(name: str) -> bool:
    return name.startswith("tnr_") and name.endswith((".tar.gz", ".zip"))


def expected_archives(version: str) -> dict[str, str]:
    return {
        f"{platform}/{arch}": f"tnr_{version}_{os_name}_{arch}.{extension}"
        for platform, os_name, extension in (
            ("macos", "darwin", "tar.gz"),
            ("linux", "linux", "tar.gz"),
            ("windows", "windows", "zip"),
        )
        for arch in ("amd64", "arm64")
    }


def validate_manifest(path: Path, version: str, archives: dict[str, str]) -> None:
    manifest = json.loads(path.read_text())
    if not isinstance(manifest, dict) or manifest.get("version") != f"v{version}":
        raise ValueError(f"{path}: manifest version does not match v{version}")
    assets = manifest.get("assets")
    if not isinstance(assets, dict):
        raise ValueError(f"{path}: manifest assets must be an object")
    base = f"https://github.com/Thunder-Compute/thunder-cli/releases/download/v{version}/"
    for key, name in {**archives, "checksums": "checksums.txt"}.items():
        if assets.get(key) != base + name:
            raise ValueError(f"{path}: {key} must reference {base + name}")
    for key, url in assets.items():
        if not isinstance(url, str) or not url.startswith(base):
            raise ValueError(f"{path}: {key} must reference an asset from v{version}")
        name = url[len(base):]
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]*", name):
            raise ValueError(f"{path}: invalid asset filename for {key}")
        # The combined checksum file is written only after every check succeeds.
        if key != "checksums" and not (path.parent / name).is_file():
            raise ValueError(f"{path}: advertised asset {name} is missing")


def validate(artifacts: Path, version: str, published: Path | None) -> None:
    archives = expected_archives(version)
    platforms = ("windows",) if published else ("macos", "linux", "windows")
    parts = [artifacts / f"checksums-{platform}.txt" for platform in platforms]
    if set(artifacts.glob("checksums-*.txt")) != set(parts):
        raise ValueError("Expected checksum files: " + ", ".join(path.name for path in parts))

    new_entries = {}
    for part in parts:
        entries = read_checksums(part)
        duplicates = new_entries.keys() & entries.keys()
        if duplicates:
            raise ValueError("Duplicate checksums across platforms: " + ", ".join(sorted(duplicates)))
        new_entries.update(entries)

    local_archives = {path.name for path in artifacts.iterdir() if is_archive(path.name)}
    required = {name for key, name in archives.items() if not published or key.startswith("windows/")}
    if local_archives != required:
        raise ValueError(f"Expected archives {sorted(required)}, found {sorted(local_archives)}")
    missing = required - new_entries.keys()
    if missing:
        raise ValueError("Missing archive checksums: " + ", ".join(sorted(missing)))

    combined = {}
    if published:
        release = json.loads((published / "release.json").read_text())
        if not isinstance(release, dict) or release.get("tag_name") != f"v{version}":
            raise ValueError("Windows-only publishing requires the existing target release")
        assets = release.get("assets")
        if not isinstance(assets, list) or not assets:
            raise ValueError("Existing release has no asset list")
        if any(not isinstance(asset, dict) or not isinstance(asset.get("name"), str) for asset in assets):
            raise ValueError("Existing release has a malformed asset list")
        names = {asset["name"] for asset in assets}
        if len(names) != len(assets) or "checksums.txt" not in names:
            raise ValueError("Existing release must have unique assets and checksums.txt")
        combined = read_checksums(published / "checksums.txt")
        missing = {name for name in names if is_archive(name)} - combined.keys()
        if missing:
            raise ValueError("Existing archives have no checksums: " + ", ".join(sorted(missing)))
        missing = set(archives.values()) - (names | local_archives)
        if missing:
            raise ValueError("Release would be missing archives: " + ", ".join(sorted(missing)))
        for asset in assets:
            name = asset["name"]
            digest = asset.get("digest")
            if name in combined and name not in new_entries and digest:
                if digest != "sha256:" + combined[name]:
                    raise ValueError(f"Existing checksum disagrees with the published digest for {name}")
        if (artifacts / "latest.json").exists():
            raise ValueError("Windows-only publishing must preserve the existing manifest")
    elif "-" not in version or (artifacts / "latest.json").exists():
        validate_manifest(artifacts / "latest.json", version, archives)

    combined.update(new_entries)
    output = artifacts / "checksums.txt"
    temporary = artifacts / "checksums.txt.tmp"
    temporary.write_text("".join(f"{checksum}  {name}\n" for name, checksum in sorted(combined.items())))
    temporary.replace(output)
    for part in parts:
        part.unlink()
    print(f"Validated release metadata; wrote {len(combined)} checksum entries.")


def main() -> int:
    parser = argparse.ArgumentParser(description="Validate CLI release artifacts and combine their checksums.")
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--version", required=True, help="Release version without the v prefix")
    parser.add_argument("--published", type=Path, help="Existing release.json and checksums.txt for Windows-only publishing")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", args.version):
        parser.error("version must be a semantic version without the v prefix")
    try:
        validate(args.artifacts, args.version, args.published)
    except (OSError, ValueError) as error:
        print(f"Release validation failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
