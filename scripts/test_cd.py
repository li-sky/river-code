"""Exercise release gates, rollback and crash recovery without production credentials."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("river_cd", Path(__file__).with_name("cd.py"))
cd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cd)
OLD = "1" * 40
NEW = "2" * 40


class DeploymentTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for sha in (OLD, NEW):
            directory = self.root / "releases" / sha
            directory.mkdir(parents=True)
            (directory / "compose.yaml").write_text("services: {}\n")
        try:
            (self.root / "current").symlink_to(self.root / "releases" / OLD, target_is_directory=True)
        except OSError:
            self.skipTest("Directory symlinks require Linux or Windows developer mode")
        (self.root / "deployed-version").write_text(OLD + "\n")
        (self.root / "deployed-at").write_text("before\n")
        self.override = "services:\n  app:\n    image: river:previous\n"
        (self.root / "compose.production.yaml").write_text(self.override)
        self.deployment = cd.Deployment(self.root)
        self.deployment.locked = contextlib.nullcontext
        self.deployment.verify_image = Mock()
        self.deployment.compose = Mock()
        self.deployment.start = Mock()
        self.deployment.backup = Mock(return_value="private-backup.dump")
        self.deployment.health = Mock()
        self.deployment.busy = Mock(return_value=False)
        self.sleep = patch.object(cd.time, "sleep")
        self.sleep.start()
        self.addCleanup(self.sleep.stop)
        cd.write_json(self.deployment.pending, {"sha": NEW})

    def result(self):
        return json.loads(self.deployment.result.read_text())

    def assert_old(self):
        self.assertEqual((self.root / "current").resolve(), self.root / "releases" / OLD)
        self.assertEqual((self.root / "compose.production.yaml").read_text(), self.override)
        self.assertEqual((self.root / "deployed-version").read_text().strip(), OLD)

    def test_busy_rooms_do_not_stop_or_back_up_the_app(self):
        self.deployment.busy.return_value = True
        self.assertEqual(self.deployment.deploy(), "busy")
        self.deployment.compose.assert_not_called()
        self.deployment.backup.assert_not_called()
        self.assertTrue(self.deployment.pending.exists())
        self.assertFalse(self.deployment.maintenance.exists())
        self.assert_old()

    def test_connection_arriving_before_maintenance_defers_release(self):
        self.deployment.busy.side_effect = [False, True]
        self.assertEqual(self.deployment.deploy(), "busy")
        self.deployment.compose.assert_not_called()
        self.assertFalse(self.deployment.maintenance.exists())
        self.assert_old()

    def test_connection_arriving_during_shutdown_restarts_old_app(self):
        self.deployment.busy.side_effect = [False, False, True]
        self.assertEqual(self.deployment.deploy(), "busy")
        self.deployment.start.assert_called_once()
        self.deployment.backup.assert_not_called()
        self.assertFalse(self.deployment.transaction.exists())
        self.assertTrue(self.deployment.pending.exists())
        self.assert_old()

    def test_idle_release_updates_version_and_retains_backup(self):
        self.assertEqual(self.deployment.deploy(), "deployed")
        self.assertEqual((self.root / "current").resolve(), self.root / "releases" / NEW)
        self.assertIn("river:" + NEW, (self.root / "compose.production.yaml").read_text())
        self.assertEqual(self.deployment.status(NEW), "deployed")
        self.assertEqual(self.result()["backup"], "private-backup.dump")
        self.assertFalse(self.deployment.pending.exists())
        self.assertFalse(self.deployment.transaction.exists())
        self.assertFalse(self.deployment.maintenance.exists())

    def test_backup_failure_restores_old_app_without_switching(self):
        self.deployment.backup.side_effect = RuntimeError("backup failed")
        with self.assertRaisesRegex(RuntimeError, "backup failed"):
            self.deployment.deploy()
        self.assert_old()
        self.assertEqual(self.result()["status"], "failed")
        self.assertFalse(self.deployment.pending.exists())
        self.assertFalse(self.deployment.maintenance.exists())

    def test_health_failure_rolls_back_and_preserves_version(self):
        self.deployment.health.side_effect = [RuntimeError("new health failed"), None]
        with self.assertRaisesRegex(RuntimeError, "new health failed"):
            self.deployment.deploy()
        self.assert_old()
        self.assertEqual(self.deployment.start.call_count, 2)
        self.assertEqual(self.deployment.status(NEW), "failed")
        self.assertFalse(self.deployment.maintenance.exists())

    def test_failed_rollback_keeps_maintenance_and_recovery_record(self):
        self.deployment.start.side_effect = RuntimeError("no healthy app")
        with self.assertRaisesRegex(RuntimeError, "Deployment and rollback failed"):
            self.deployment.deploy()
        self.assertEqual(self.result()["status"], "rollback_failed")
        self.assertTrue(self.deployment.maintenance.exists())
        self.assertTrue(self.deployment.transaction.exists())
        self.assertFalse(self.deployment.pending.exists())

    def test_interrupted_switch_is_recovered_before_next_release(self):
        cd.write_json(self.deployment.transaction, {"sha": NEW,
                      "previousSource": str(self.root / "releases" / OLD),
                      "previousOverride": self.override, "previousVersion": OLD, "previousAt": "before\n"})
        self.deployment.switch(self.root / "releases" / NEW, "services: {}\n")
        self.deployment.maintenance.write_text("interrupted")
        self.assertEqual(self.deployment.deploy(), "empty")
        self.assert_old()
        self.assertEqual(self.result()["status"], "failed")
        self.assertFalse(self.deployment.transaction.exists())
        self.assertFalse(self.deployment.maintenance.exists())

    def test_rollback_public_health_failure_reenables_maintenance(self):
        self.deployment.health.side_effect = RuntimeError("public health failed")
        with self.assertRaisesRegex(RuntimeError, "Deployment and rollback failed"):
            self.deployment.deploy()
        self.assertTrue(self.deployment.maintenance.exists())
        self.assertTrue(self.deployment.transaction.exists())
        self.assertEqual(self.result()["status"], "rollback_failed")

    def test_interrupted_recovery_health_failure_retains_maintenance(self):
        cd.write_json(self.deployment.transaction, {"sha": NEW,
                      "previousSource": str(self.root / "releases" / OLD),
                      "previousOverride": self.override, "previousVersion": OLD, "previousAt": "before\n"})
        self.deployment.health.side_effect = RuntimeError("recovery health failed")
        with self.assertRaisesRegex(RuntimeError, "recovery health failed"):
            self.deployment.deploy()
        self.assertTrue(self.deployment.maintenance.exists())
        self.assertTrue(self.deployment.transaction.exists())

    def test_receive_rejects_shell_and_partial_revision(self):
        self.deployment.run = Mock()
        for command in ("sh", "deploy main", "deploy " + NEW + "; id", "status ../../etc/passwd"):
            with self.subTest(command=command), self.assertRaises(ValueError):
                self.deployment.receive(command, io.BytesIO(b"image"))
        self.deployment.run.assert_not_called()

    def test_receive_queues_only_after_image_validation(self):
        self.deployment.run = Mock()
        self.deployment.receive("deploy " + NEW, io.BytesIO(b"image"))
        self.deployment.verify_image.assert_called_once_with(NEW)
        self.assertEqual(json.loads(self.deployment.pending.read_text())["sha"], NEW)

    def test_invalid_image_cannot_replace_existing_queue(self):
        self.deployment.run = Mock()
        self.deployment.verify_image.side_effect = RuntimeError("revision mismatch")
        with self.assertRaisesRegex(RuntimeError, "revision mismatch"):
            self.deployment.receive("deploy " + OLD, io.BytesIO(b"image"))
        self.assertEqual(json.loads(self.deployment.pending.read_text())["sha"], NEW)

    def test_unknown_database_state_is_not_treated_as_idle(self):
        self.deployment.database = Mock(return_value=Mock(stdout="unknown\n"))
        with self.assertRaisesRegex(RuntimeError, "Could not determine"):
            cd.Deployment.busy(self.deployment)

    def test_busy_sql_checks_private_snapshots_and_active_hands(self):
        self.assertIn("river_rooms", cd.BUSY_SQL)
        self.assertIn("connected", cd.BUSY_SQL)
        self.assertIn("jsonb_typeof", cd.BUSY_SQL)
        self.assertIn("<> 'complete'", cd.BUSY_SQL)


if __name__ == "__main__":
    unittest.main()
