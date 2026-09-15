#!/usr/bin/env python3

import argparse
import datetime
import hashlib
import http.server
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
from pathlib import Path


IMAGE = "python:3.13.7-slim-bookworm"
CLI = Path(__file__).resolve().parent.parent


def run(command: list[str], **kwargs) -> str:
    result = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, **kwargs)
    if result.returncode:
        raise RuntimeError(f"Command failed: {' '.join(command)}\n{result.stdout}")
    return result.stdout


def prepare(released: str) -> None:
    for tool in ("bazel", "docker", "gh"):
        if not shutil.which(tool):
            raise RuntimeError(f"{tool} is required")
    info = json.loads(run(["docker", "info", "--format", "{{json .}}"], timeout=30))
    arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(info["Architecture"])
    if info["OSType"] != "linux" or arch is None:
        raise RuntimeError("A local Linux Docker engine with amd64 or arm64 is required")

    major, minor, patch = map(int, released.split("."))
    candidate, future = (f"{major}.{minor}.{patch + offset}" for offset in (1, 2))
    output = Path(tempfile.mkdtemp(prefix="tnr-self-update-"))
    print(f"Binaries and run log: {output}", flush=True)
    downloads = output / "released"
    downloads.mkdir()
    filename = f"tnr_{released}_linux_{arch}.tar.gz"
    run([
        "gh", "release", "download", f"v{released}", "--repo", "Thunder-Compute/thunder-cli",
        "--pattern", filename, "--pattern", "checksums.txt", "--dir", str(downloads),
    ], timeout=120)
    matches = [line.split()[0].lower() for line in (downloads / "checksums.txt").read_text().splitlines()
               if len(line.split()) == 2 and line.split()[1].lstrip("*") == filename]
    archive = downloads / filename
    with archive.open("rb") as handle:
        actual = hashlib.file_digest(handle, "sha256").hexdigest()
    if matches != [actual]:
        raise RuntimeError(f"Published checksum verification failed for {filename}")
    with tarfile.open(archive) as package:
        members = [member for member in package.getmembers() if member.isfile() and Path(member.name).name == "tnr"]
        if len(members) != 1:
            raise RuntimeError("Published archive must contain exactly one tnr executable")
        with package.extractfile(members[0]) as executable:
            (output / "released-tnr").write_bytes(executable.read())
    (output / "released-tnr").chmod(0o755)
    print(f"Verified published v{released} binary", flush=True)

    build_env = dict(os.environ, GOOS="linux", GOARCH=arch, CGO_ENABLED="0")
    for name, version in (("candidate", candidate), ("future", future)):
        run([
            "bazel", "run", "//:go", "--", "-C", str(CLI), "build", "-mod=readonly",
            "-ldflags", f"-X github.com/Thunder-Compute/thunder-cli/internal/version.BuildVersion={version}",
            "-o", str(output / f"{name}-tnr"), ".",
        ], cwd=CLI.parent, env=build_env, timeout=300)
        print(f"Built this branch as test version {version}", flush=True)
    (output / "versions.json").write_text(json.dumps({"released": released, "candidate": candidate, "future": future, "arch": arch}))
    command = [
        "docker", "run", "--rm", "--network", "none",
        "--mount", f"type=bind,source={output},target=/test-assets,readonly",
        "--mount", f"type=bind,source={Path(__file__).resolve()},target=/test-self-update.py,readonly",
        IMAGE, "python", "/test-self-update.py", "--inside-container", "/test-assets",
    ]
    with (output / "run.log").open("w") as log:
        with subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True) as process:
            for line in process.stdout:
                print(line, end="", flush=True)
                log.write(line)
            if process.wait() != 0:
                raise RuntimeError(f"Self-update checks failed; see {output / 'run.log'}")


def exercise(source: Path) -> None:
    if not Path("/.dockerenv").exists():
        raise RuntimeError("The checks must run inside the disposable Docker container")
    versions = json.loads((source / "versions.json").read_text())
    candidate, future, arch = (versions[key] for key in ("candidate", "future", "arch"))
    cache = Path.home() / ".cache/tnr"
    retry_file = Path.home() / ".thunder/cache/optional_update_status.json"
    payloads = {versions[name]: (source / f"{name}-tnr").read_bytes() for name in ("candidate", "future")}
    archives = {}
    for version, data in payloads.items():
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w:gz") as package:
            entry = tarfile.TarInfo("tnr")
            entry.size, entry.mode = len(data), 0o755
            package.addfile(entry, io.BytesIO(data))
        archives[version] = output.getvalue()
    state = {}
    hits = {}

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_CONNECT(self):
            # Reject HTTPS fallbacks locally; the container has no external network.
            self.send_error(503)

        def do_GET(self):
            hits[self.path] = hits.get(self.path, 0) + 1
            version = state["version"]
            filename = f"tnr_{version}_linux_{arch}.tar.gz"
            if self.path == "/latest.json":
                body = json.dumps({"version": "v" + version, "assets": {
                    f"linux/{arch}": server_url + "/" + filename,
                    "checksums": server_url + "/checksums.txt",
                }}).encode()
            elif self.path == "/checksums.txt" and not state["checksum_unavailable"]:
                checksum = "0" * 64 if state["bad_checksum"] else hashlib.sha256(archives[version]).hexdigest()
                body = f"{checksum}  {filename}\n".encode()
            elif self.path == "/" + filename and not state["download_failure"]:
                body = archives[version]
            elif self.path == "/v1/user-data":
                body = b"{}"
            elif self.path == "/v1/instances/list":
                if state["status_failure"]:
                    self.send_response(400)
                    self.end_headers()
                    self.wfile.write(b'{"message":"simulated status failure"}')
                    return
                body = b"{}"
            else:
                self.send_error(503)
                return
            self.send_response(200)
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server_url = f"http://127.0.0.1:{server.server_port}"
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    env = dict(os.environ, TNR_NO_SELFUPDATE="0", TNR_MIN_VERSION_URL="", TNR_LATEST_URL=server_url + "/latest.json",
               TNR_API_URL=server_url, TNR_API_TOKEN="local-test-token", HTTPS_PROXY=server_url, HTTP_PROXY=server_url,
               NO_PROXY="127.0.0.1,localhost")

    def setup(name="candidate"):
        shutil.rmtree(cache, ignore_errors=True)
        shutil.rmtree(retry_file.parent, ignore_errors=True)
        state.update(version=future, download_failure=False, bad_checksum=False, checksum_unavailable=False, status_failure=False)
        hits.clear()
        binary = Path(tempfile.mkdtemp(prefix="tnr-case-")) / "tnr"
        shutil.copy2(source / f"{name}-tnr", binary)
        return binary

    def invoke(binary, *args, expected=0):
        result = subprocess.run([str(binary), *args], env=env, text=True, capture_output=True, timeout=30)
        print(f"$ tnr {' '.join(args)} [exit {result.returncode}]\n{result.stdout}{result.stderr}", flush=True)
        assert result.returncode == expected, f"Expected exit {expected}, got {result.returncode}"
        return result

    def status(binary, expected=0):
        before = hits.get("/v1/instances/list", 0)
        result = invoke(binary, "status", "--no-wait", expected=expected)
        assert hits.get("/v1/instances/list", 0) == before + 1, "Requested status command did not execute exactly once"
        return result

    def expire(path, field):
        record = json.loads(path.read_text())
        record[field] = (datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=61)).isoformat()
        path.write_text(json.dumps(record))

    try:
        binary = setup("released")
        state["version"] = candidate
        invoke(binary, "update")
        assert binary.read_bytes() == payloads[candidate]
        assert candidate in invoke(binary, "--version").stdout
        state["version"] = future
        invoke(binary, "update")
        assert binary.read_bytes() == payloads[future]
        assert future in invoke(binary, "--version").stdout
        print(f"PASS published {versions['released']} -> candidate {candidate} -> future {future}", flush=True)

        binary = setup()
        state["checksum_unavailable"] = True
        status(binary)
        assert not retry_file.exists()
        checksum_hits = hits.get("/checksums.txt", 0)
        status(binary)
        assert hits.get("/checksums.txt", 0) == checksum_hits
        state["checksum_unavailable"] = False
        expire(cache / f"checksums_v{future}_linux.json", "checked_at")
        invoke(binary, "status", "--no-wait")
        assert binary.read_bytes() == payloads[future] and not retry_file.exists()
        print("PASS checksum outage preserves commands; one-hour cache expiry allows recovery", flush=True)

        binary = setup()
        state.update(download_failure=True, status_failure=True)
        status(binary, expected=1)
        archive_path = f"/tnr_{future}_linux_{arch}.tar.gz"
        assert hits[archive_path] == 1
        status(binary, expected=1)
        assert hits[archive_path] == 1
        expire(retry_file, "last_attempt")
        status(binary, expected=1)
        assert hits[archive_path] == 2
        state["download_failure"] = False
        invoke(binary, "update")
        assert binary.read_bytes() == payloads[future] and not retry_file.exists()
        print("PASS failed downloads preserve command errors; cooldown and immediate explicit retry work", flush=True)

        binary = setup()
        retry_file.parent.mkdir(parents=True)
        now = datetime.datetime.now(datetime.timezone.utc).isoformat()
        for record in ({"last_attempt": now}, {"version": candidate, "last_attempt": now}):
            retry_file.write_text(json.dumps(record))
            state["download_failure"] = True
            before = hits.get(archive_path, 0)
            status(binary)
            assert hits[archive_path] == before + 1
        print("PASS legacy records and cooldowns for other versions do not suppress updates", flush=True)

        binary = setup()
        state["bad_checksum"] = True
        status(binary)
        assert not list(cache.glob("checksums_*.json"))
        assert binary.read_bytes() == payloads[candidate]
        state["bad_checksum"] = False
        expire(retry_file, "last_attempt")
        invoke(binary, "status", "--no-wait")
        assert binary.read_bytes() == payloads[future] and not retry_file.exists()
        assert hits["/checksums.txt"] == 2
        print("PASS checksum mismatch preserves the binary and clears stale data; repaired release updates", flush=True)

        binary = setup()
        state["download_failure"] = True
        invoke(binary, "update", expected=1)
        assert binary.read_bytes() == payloads[candidate]
        print("PASS explicit update failure returns nonzero", flush=True)
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def main() -> int:
    parser = argparse.ArgumentParser(description="Exercise real CLI self-updates using a local server in an isolated Linux container.")
    parser.add_argument("--released-version", default="2.0.71", help="Published starting version (default: 2.0.71)")
    parser.add_argument("--inside-container", type=Path, help=argparse.SUPPRESS)
    args = parser.parse_args()
    try:
        if args.inside_container:
            exercise(args.inside_container)
        else:
            if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", args.released_version):
                parser.error("released-version must be a stable version without the v prefix")
            prepare(args.released_version)
    except (OSError, RuntimeError, AssertionError, subprocess.TimeoutExpired) as error:
        print(f"Self-update check failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
