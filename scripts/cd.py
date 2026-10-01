#!/usr/bin/env python3
"""Receive checked images and publish queued releases on an idle Linux host."""
from __future__ import annotations

import argparse
import contextlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

SHA = re.compile(r"[0-9a-f]{40}\Z")
REPOSITORY = "li-sky/river-code"
MAX_UPLOAD = 128 * 1024 * 1024
BUSY_SQL = """SELECT EXISTS (
  SELECT 1 FROM river_rooms WHERE
  EXISTS (SELECT 1 FROM jsonb_array_elements(snapshot->'players') p
          WHERE p->>'connected' = 'true')
  OR (jsonb_typeof(snapshot->'hand') = 'object'
      AND COALESCE(snapshot->'hand'->>'phase', '') <> 'complete')
);"""


def validate_sha(value):
    if not SHA.fullmatch(value):
        raise ValueError("Expected a full lowercase Git commit SHA")
    return value


def atomic_write(path, content, mode=0o600):
    descriptor, name = tempfile.mkstemp(prefix=".cd-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as file:
            file.write(content)
        os.chmod(name, mode)
        os.replace(name, path)
    finally:
        Path(name).unlink(missing_ok=True)


def write_json(path, value):
    atomic_write(path, json.dumps(value, indent=2) + "\n")


class Deployment:
    def __init__(self, root, project="river"):
        self.root = Path(root).resolve()
        self.project = project
        self.pending = self.root / "pending-release.json"
        self.result = self.root / "last-deployment.json"
        self.transaction = self.root / "deployment-transaction.json"
        self.maintenance = self.root / "maintenance"

    @contextlib.contextmanager
    def locked(self):
        import fcntl
        with (self.root / "cd.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            yield

    def run(self, argv, **kwargs):
        return subprocess.run(argv, check=True, **kwargs)

    def compose(self, *args, **kwargs):
        return self.run([
            "docker", "compose", "--project-name", self.project,
            "--env-file", str(self.root / ".env"),
            "-f", str(self.root / "current/compose.yaml"),
            "-f", str(self.root / "compose.production.yaml"), *args,
        ], **kwargs)

    def database(self, command, **kwargs):
        return self.compose("exec", "-T", "db", "sh", "-c", command, **kwargs)

    def busy(self):
        result = self.database(
            'exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1',
            input=BUSY_SQL, text=True, capture_output=True)
        value = result.stdout.strip()
        if value not in ("t", "f"):
            raise RuntimeError("Could not determine whether rooms are idle")
        return value == "t"

    def image(self, sha):
        return "river:" + validate_sha(sha)

    def verify_image(self, sha):
        result = self.run([
            "docker", "image", "inspect", self.image(sha), "--format",
            '{{index .Config.Labels "org.opencontainers.image.revision"}}',
        ], text=True, capture_output=True)
        if result.stdout.strip() != sha:
            raise RuntimeError("Image revision does not match the requested release")

    def fetch_source(self, sha, directory):
        with tempfile.TemporaryDirectory(prefix="river-source-", dir=self.root) as temporary:
            archive = Path(temporary) / "source.tar.gz"
            self.run(["curl", "--fail", "--silent", "--show-error", "--location",
                      "--max-time", "120", "--output", str(archive),
                      f"https://codeload.github.com/{REPOSITORY}/tar.gz/{sha}"])
            with tarfile.open(archive, "r:gz") as tar:
                members = tar.getmembers()
                prefix = members[0].name.split("/")[0] + "/"
                for member in members:
                    if member.name == prefix[:-1]:
                        continue
                    if not member.name.startswith(prefix):
                        raise RuntimeError("Unexpected source archive layout")
                    member.name = member.name[len(prefix):]
                    tar.extract(member, directory, filter="data")
        if not (directory / "compose.yaml").is_file():
            raise RuntimeError("Release source has no Compose configuration")

    def receive(self, command, stream):
        status_match = re.fullmatch(r"status ([0-9a-f]{40})", command)
        if status_match:
            with self.locked():
                print(self.status(status_match.group(1)), flush=True)
            return
        match = re.fullmatch(r"deploy ([0-9a-f]{40})", command)
        if not match:
            raise ValueError("This key accepts only deploy/status with a full commit SHA")
        sha = match.group(1)
        with self.locked(), tempfile.TemporaryDirectory(prefix="river-upload-", dir=self.root) as temporary:
            upload = Path(temporary) / "image.tar.gz"
            size = 0
            with upload.open("wb") as file:
                while chunk := stream.read(1024 * 1024):
                    size += len(chunk)
                    if size > MAX_UPLOAD:
                        raise ValueError("Image upload exceeds 128 MiB")
                    file.write(chunk)
            if not size:
                raise ValueError("Empty image upload")
            self.run(["docker", "load", "--input", str(upload)])
            self.verify_image(sha)
            release = self.root / "releases" / sha
            if not release.exists():
                stage = Path(temporary) / "source"
                stage.mkdir()
                self.fetch_source(sha, stage)
                os.chmod(stage, 0o755)
                os.replace(stage, release)
            write_json(self.pending, {"sha": sha, "image": self.image(sha), "queuedAt": time.time()})
            print(f"Queued release {sha}; the server will publish when rooms are idle", flush=True)

    def status(self, sha):
        validate_sha(sha)
        if (self.root / "deployed-version").read_text().strip() == sha:
            return "deployed"
        if self.pending.exists() and json.loads(self.pending.read_text())["sha"] == sha:
            return "queued"
        if self.result.exists():
            result = json.loads(self.result.read_text())
            if result["sha"] == sha and result["status"] in ("failed", "rollback_failed"):
                return result["status"]
        return "superseded"

    def switch(self, directory, override):
        next_link = self.root / "current.next"
        next_link.unlink(missing_ok=True)
        next_link.symlink_to(directory, target_is_directory=True)
        os.replace(next_link, self.root / "current")
        atomic_write(self.root / "compose.production.yaml", override)

    def start(self):
        self.compose("up", "-d", "--no-build", "--no-deps", "--wait",
                     "--wait-timeout", "90", "app")

    def backup(self, sha):
        directory = self.root / "backups"
        directory.mkdir(mode=0o700, exist_ok=True)
        path = directory / (time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + sha + ".dump")
        with path.open("xb") as file:
            self.database('exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc', stdout=file)
        os.chmod(path, 0o600)
        if path.stat().st_size == 0:
            raise RuntimeError("Database backup is empty")
        with path.open("rb") as file:
            self.database('exec pg_restore --list', stdin=file, stdout=subprocess.DEVNULL)
        return str(path)

    def health(self):
        values = {}
        for line in (self.root / ".env").read_text().splitlines():
            key, separator, value = line.partition("=")
            if separator and key in ("BASE_URL", "APP_PORT"):
                values[key] = value.strip().strip("\"'")
        port = int(values.get("APP_PORT", "8080"))
        base = values.get("BASE_URL", "http://localhost:8080").rstrip("/")
        if not base.startswith(("http://", "https://")):
            raise ValueError("Invalid public BASE_URL")
        for url in (f"http://127.0.0.1:{port}/healthz", base + "/healthz"):
            request = urllib.request.Request(url, headers={"User-Agent": "RIVER deployment health check"})
            with urllib.request.urlopen(request, timeout=20) as response:
                if json.load(response).get("status") != "ok":
                    raise RuntimeError("Health check failed")

    def record(self, sha, status, **details):
        write_json(self.result, {"sha": sha, "status": status, "at": time.time(), **details})

    def recover(self):
        if not self.transaction.exists():
            return
        transaction = json.loads(self.transaction.read_text())
        sha = validate_sha(transaction["sha"])
        if self.result.exists() and json.loads(self.result.read_text()).get("status") == "deployed" and self.status(sha) == "deployed":
            self.maintenance.unlink(missing_ok=True)
        else:
            previous = Path(transaction["previousSource"]).resolve()
            if previous.parent != (self.root / "releases").resolve():
                raise RuntimeError("Recovery source must stay inside the release directory")
            atomic_write(self.maintenance, "Deployment recovery in progress\n", mode=0o644)
            self.switch(previous, transaction["previousOverride"])
            self.start()
            self.maintenance.unlink(missing_ok=True)
            try:
                self.health()
            except Exception:
                atomic_write(self.maintenance, "Deployment recovery requires attention\n", mode=0o644)
                raise
            atomic_write(self.root / "deployed-version", transaction["previousVersion"] + "\n")
            atomic_write(self.root / "deployed-at", transaction["previousAt"])
            self.record(sha, "failed", previousVersion=transaction["previousVersion"],
                        backup=transaction.get("backup"), error="Recovered an interrupted deployment")
        if self.pending.exists() and json.loads(self.pending.read_text())["sha"] == sha:
            self.pending.unlink()
        self.transaction.unlink()
        print("Recovered interrupted deployment", flush=True)

    def deploy(self):
        with self.locked():
            self.recover()
            if not self.pending.exists():
                return "empty"
            sha = validate_sha(json.loads(self.pending.read_text())["sha"])
            if (self.root / "deployed-version").read_text().strip() == sha:
                self.pending.unlink()
                return "unchanged"
            self.verify_image(sha)
            release = self.root / "releases" / sha
            if not (release / "compose.yaml").is_file():
                raise RuntimeError("Queued source is missing")
            if self.busy():
                self.record(sha, "waiting_for_idle")
                print("Deployment deferred: online players or active hands", flush=True)
                return "busy"
            old_source = (self.root / "current").resolve(strict=True)
            old_override = (self.root / "compose.production.yaml").read_text()
            old_version = (self.root / "deployed-version").read_text().strip()
            old_at = (self.root / "deployed-at").read_text() if (self.root / "deployed-at").exists() else ""
            backup = None
            stopped = False
            try:
                # Nginx reads this marker before forwarding new HTTP/WebSocket requests.
                atomic_write(self.maintenance, "Deployment in progress\n", mode=0o644)
                time.sleep(2)
                if self.busy():
                    self.record(sha, "waiting_for_idle")
                    return "busy"
                write_json(self.transaction, {"sha": sha, "previousSource": str(old_source),
                           "previousOverride": old_override, "previousVersion": old_version, "previousAt": old_at})
                stopped = True
                self.compose("stop", "app")
                # Any connection or hand that arrived before the maintenance gate must be settled first.
                if self.busy():
                    self.start()
                    stopped = False
                    self.transaction.unlink()
                    self.record(sha, "waiting_for_idle")
                    return "busy"
                backup = self.backup(sha)
                transaction = json.loads(self.transaction.read_text())
                transaction["backup"] = backup
                write_json(self.transaction, transaction)
                self.switch(release, "services:\n  app:\n    image: " + self.image(sha) + "\n")
                self.start()
                self.maintenance.unlink(missing_ok=True)
                self.health()
                atomic_write(self.root / "deployed-version", sha + "\n")
                atomic_write(self.root / "deployed-at", time.strftime("%Y-%m-%dT%H:%M:%SZ\n", time.gmtime()))
                self.record(sha, "deployed", previousVersion=old_version, backup=backup)
                self.pending.unlink()
                self.transaction.unlink()
                print(f"Deployed {sha}", flush=True)
                return "deployed"
            except Exception as error:
                if stopped:
                    try:
                        atomic_write(self.maintenance, "Deployment recovery in progress\n", mode=0o644)
                        self.switch(old_source, old_override)
                        self.start()
                        self.maintenance.unlink(missing_ok=True)
                        self.health()
                        atomic_write(self.root / "deployed-version", old_version + "\n")
                        atomic_write(self.root / "deployed-at", old_at)
                    except Exception as rollback_error:
                        atomic_write(self.maintenance, "Deployment recovery requires attention\n", mode=0o644)
                        self.record(sha, "rollback_failed", previousVersion=old_version, backup=backup,
                                    error=str(error), rollbackError=str(rollback_error))
                        self.pending.unlink(missing_ok=True)
                        raise RuntimeError("Deployment and rollback failed; maintenance remains enabled") from rollback_error
                self.record(sha, "failed", previousVersion=old_version, backup=backup, error=str(error))
                self.pending.unlink(missing_ok=True)
                self.transaction.unlink(missing_ok=True)
                raise
            finally:
                # Keep maintenance enabled only if neither version could be started.
                if not self.result.exists() or json.loads(self.result.read_text()).get("status") != "rollback_failed":
                    self.maintenance.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("receive", "deploy", "status"))
    parser.add_argument("--root", default="/opt/river")
    parser.add_argument("--project", default="river")
    args = parser.parse_args()
    deployment = Deployment(args.root, args.project)
    if args.command == "receive":
        deployment.receive(os.environ.get("SSH_ORIGINAL_COMMAND", ""), sys.stdin.buffer)
    elif args.command == "deploy":
        deployment.deploy()
    else:
        for path in (deployment.pending, deployment.result):
            if path.exists():
                print(path.name + ": " + path.read_text())


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print("CD error: " + str(error), file=sys.stderr)
        sys.exit(1)
