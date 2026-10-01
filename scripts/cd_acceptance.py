#!/usr/bin/env python3
"""Verify CD against isolated Docker/PostgreSQL; never use a production project."""
import argparse
import asyncio
import secrets
import shutil
import socket
import subprocess
import tempfile
from pathlib import Path

from cd import Deployment, write_json


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


async def verify(image):
    with tempfile.TemporaryDirectory(prefix="river-cd-acceptance-") as temporary:
        root = Path(temporary)
        project = "river-cd-acceptance-" + secrets.token_hex(4)
        old, good, bad = (secrets.token_hex(20) for _ in range(3))
        deployment = Deployment(root, project)
        port = free_port()
        environment = ("POSTGRES_PASSWORD=" + secrets.token_hex(32) + "\n"
                       f"POSTGRES_PORT={free_port()}\nAPP_PORT={port}\nAPP_BIND=127.0.0.1\n"
                       f"BASE_URL=http://127.0.0.1:{port}\n")
        (root / ".env").write_text(environment)
        source = Path(__file__).resolve().parents[1] / "compose.yaml"
        for sha in (old, good, bad):
            release = root / "releases" / sha
            release.mkdir(parents=True)
            shutil.copyfile(source, release / "compose.yaml")
        (root / "current").symlink_to(root / "releases" / old, target_is_directory=True)
        (root / "compose.production.yaml").write_text(f"services:\n  app:\n    image: {image}\n")
        (root / "deployed-version").write_text(old + "\n")
        (root / "deployed-at").write_text("acceptance baseline\n")
        # Use the same application with a different revision, then a deliberately failing application.
        for sha, fail in ((good, False), (bad, True)):
            dockerfile = f"FROM {image}\nLABEL org.opencontainers.image.revision={sha}\n"
            if fail:
                dockerfile += 'ENTRYPOINT ["/bin/sh", "-c", "exit 1"]\n'
                dockerfile += 'HEALTHCHECK --interval=1s --timeout=1s --start-period=1s --retries=1 CMD exit 1\n'
            subprocess.run(["docker", "build", "-t", deployment.image(sha), "-"],
                           input=dockerfile, text=True, check=True, stdout=subprocess.DEVNULL)
        try:
            deployment.compose("up", "-d", "--no-build", "--wait", "--wait-timeout", "120", "db", "app")
            # Import after choosing the test-only URL; smoke.Client uses its module BASE.
            import smoke
            smoke.BASE = f"http://127.0.0.1:{port}"
            client = smoke.Client()
            client.request("POST", "/api/auth/guest", {"name": "CD acceptance", "password": ""})
            user = client.request("GET", "/api/me")
            room = client.request("POST", "/api/rooms", {"name": "Private CD gate", "settings": {
                "visibility": "private", "smallBlind": 5, "bigBlind": 10, "buyIn": 1000,
                "maxPlayers": 9, "actionSeconds": 30, "voiceEnabled": False,
                "chatEnabled": True, "reactionsEnabled": True}})["id"]
            await client.connect(room)
            write_json(deployment.pending, {"sha": good})
            old_id = deployment.compose("ps", "-q", "app", text=True, capture_output=True).stdout.strip()
            assert deployment.deploy() == "busy", "Online private-room member did not defer release"
            assert deployment.compose("ps", "-q", "app", text=True, capture_output=True).stdout.strip() == old_id
            await client.send(type="leave")
            await asyncio.wait_for(client.ws.wait_closed(), timeout=8)
            await client.ws.close()
            # A private snapshot with an active hand must also block release, even without connected players.
            deployment.database('exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1',
                                input="UPDATE river_rooms SET snapshot=jsonb_set(snapshot,'{hand}','{\"phase\":\"preflop\"}');", text=True,
                                stdout=subprocess.DEVNULL)
            assert deployment.deploy() == "busy", "Active hand did not defer release"
            deployment.database('exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1',
                                input="UPDATE river_rooms SET snapshot=jsonb_set(snapshot,'{hand}','null');", text=True,
                                stdout=subprocess.DEVNULL)
            assert deployment.deploy() == "deployed"
            assert client.request("GET", "/api/me")["id"] == user["id"], "Session lost after deployment"
            assert (root / ".env").read_text() == environment
            assert len(list((root / "backups").glob("*.dump"))) == 1
            write_json(deployment.pending, {"sha": bad})
            try:
                deployment.deploy()
                raise AssertionError("An unhealthy image was accepted")
            except subprocess.CalledProcessError:
                pass
            assert deployment.status(bad) == "failed"
            assert (root / "deployed-version").read_text().strip() == good
            assert (root / "current").resolve() == root / "releases" / good
            assert client.request("GET", "/api/me")["id"] == user["id"], "Session lost after rollback"
            assert not deployment.maintenance.exists() and not deployment.transaction.exists()
            assert len(list((root / "backups").glob("*.dump"))) == 2
            print("PASS: private-room and active-hand gates, real backup, deployment, persistent session and unhealthy-image rollback")
        finally:
            deployment.compose("down", "-v")
            subprocess.run(["docker", "image", "rm", deployment.image(good), deployment.image(bad)], check=False,
                           stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    asyncio.run(verify(parser.parse_args().image))
