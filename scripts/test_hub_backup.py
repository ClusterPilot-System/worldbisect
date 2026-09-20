import importlib.util
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("hub_backup", Path(__file__).with_name("hub-backup.py"))
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)
AGE = shutil.which(os.environ.get("WORLDBISECT_TEST_AGE", "age"))
KEYGEN = shutil.which(os.environ.get("WORLDBISECT_TEST_AGE_KEYGEN", "age-keygen"))


class ArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.target = self.root / "restore"
        self.target.mkdir(mode=0o700)

    def archive(self, entries, extra=None):
        path = self.root / "archive.tar"
        with tarfile.open(path, "w", format=tarfile.USTAR_FORMAT) as file:
            for name, data in entries:
                backup.add_member(file, name, data)
            if extra:
                file.addfile(extra)
        return path

    def test_rejects_traversal_symlinks_duplicates_and_hardlinks(self):
        for name in ("../escape", "/tmp/escape", "data/../../escape", "unexpected/file", "data/.hub.lock"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                backup.extract_verified(self.archive([(name, b"x")]), self.target, 1000, 10)
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.FIFOTYPE):
            link = tarfile.TarInfo("data/file")
            link.type, link.linkname = kind, "../escape"
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                backup.extract_verified(self.archive([], link), self.target, 1000, 10)
        with self.assertRaises(ValueError):
            backup.extract_verified(self.archive([("config.json", b"{}"), ("config.json", b"{}")]), self.target, 1000, 10)
        self.assertFalse((self.root.parent / "escape").exists())

    def test_manifest_integrity_and_resource_limits(self):
        manifest = {"format": backup.FORMAT, "files": {"config.json": {"bytes": 2, "sha256": "0" * 64}}}
        with self.assertRaisesRegex(ValueError, "hashes"):
            backup.extract_verified(self.archive([("config.json", b"{}"), ("manifest.json", json.dumps(manifest).encode())]), self.target, 1000, 10)
        with self.assertRaisesRegex(ValueError, "limit"):
            backup.extract_verified(self.archive([("config.json", b"{}")]), self.target, 1, 10)

    def test_restore_publication_never_replaces_existing_directory(self):
        source = self.root / "source"
        source.mkdir(mode=0o700)
        fd = backup.directory(self.root, True)
        try:
            with self.assertRaises(FileExistsError):
                backup.publish_directory(source, fd, "restore")
            self.assertTrue(source.is_dir())
            self.assertTrue(self.target.is_dir())
        finally:
            os.close(fd)

    def test_descriptor_open_rejects_symlink_components(self):
        (self.root / "linked").symlink_to(self.target, target_is_directory=True)
        with self.assertRaises(OSError):
            backup.directory(self.root / "linked", True)


@unittest.skipUnless(AGE and KEYGEN, "install age and age-keygen to run real encryption tests")
class EncryptionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.config = self.root / "config.json"
        self.config.write_text('{"version":1}')
        self.config.chmod(0o600)
        self.data, self.audit = self.root / "data", self.root / "audit"
        self.data.mkdir(mode=0o700)
        self.audit.mkdir(mode=0o700)
        workspace = self.data / ("b" * 64)
        workspace.mkdir(mode=0o700)
        self.report = workspace / (("a" * 32) + ".json")
        self.report.write_text('{"finding":"private summary"}')
        self.report.chmod(0o600)
        event = self.audit / "events.jsonl"
        event.write_text('{"action":"report.create"}\n')
        event.chmod(0o600)
        self.identity = self.root / "identity.txt"
        subprocess.run([KEYGEN, "-o", str(self.identity)], check=True, capture_output=True)
        self.identity.chmod(0o600)
        self.recipient = subprocess.run([KEYGEN, "-y", str(self.identity)], check=True, capture_output=True, text=True).stdout.strip()
        self.encrypted = self.root / "backup.age"

    def create(self, **options):
        return backup.backup(self.config, self.data, self.audit, self.encrypted, self.recipient, age=AGE, **options)

    def recover(self, target="restored", **options):
        return backup.restore(self.encrypted, self.identity, self.root / target, age=AGE, **options)

    def test_real_encrypted_roundtrip_preserves_bytes_and_private_modes(self):
        result = self.create()
        self.assertEqual(result["files"], 3)
        self.assertNotIn(b"private summary", self.encrypted.read_bytes())
        self.assertEqual(self.encrypted.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.recover()["files"], 3)
        restored = self.root / "restored"
        self.assertEqual((restored / "config.json").read_bytes(), self.config.read_bytes())
        self.assertEqual((restored / "data" / self.report.relative_to(self.data)).read_bytes(), self.report.read_bytes())
        for entry in restored.rglob("*"):
            self.assertEqual(entry.stat().st_mode & 0o777, 0o700 if entry.is_dir() else 0o600)
        self.assertFalse((restored / "data/.hub.lock").exists())

    def test_ciphertext_tamper_and_wrong_identity_never_publish(self):
        self.create()
        original = self.encrypted.read_bytes()
        self.encrypted.write_bytes(original[:-1] + bytes([original[-1] ^ 1]))
        with self.assertRaisesRegex(ValueError, "age failed"):
            self.recover("tampered")
        self.assertFalse((self.root / "tampered").exists())
        self.encrypted.write_bytes(original)
        self.identity.unlink()
        subprocess.run([KEYGEN, "-o", str(self.identity)], check=True, capture_output=True)
        self.identity.chmod(0o600)
        with self.assertRaisesRegex(ValueError, "age failed"):
            self.recover("wrong-key")
        self.assertFalse((self.root / "wrong-key").exists())
        self.assertEqual(list(self.root.glob(".hub-restore-*")), [])

    def test_both_live_writer_locks_block_backup(self):
        for directory, name in ((self.data, ".hub.lock"), (self.audit, ".audit.lock")):
            fd = backup.directory(directory, True)
            locked = backup.lock_at(fd, name)
            try:
                with self.subTest(lock=name), self.assertRaisesRegex(ValueError, "hub is running"):
                    self.create()
                self.assertFalse(self.encrypted.exists())
            finally:
                os.close(locked)
                os.close(fd)

    def test_refuses_clobber_links_and_size_overflow(self):
        with self.assertRaises(ValueError):
            self.create(max_bytes=2)
        (self.data / "linked.json").symlink_to(self.config)
        with self.assertRaises(ValueError):
            self.create()
        (self.data / "linked.json").unlink()
        os.link(self.config, self.data / "hardlink.json")
        with self.assertRaises(ValueError):
            self.create()
        (self.data / "hardlink.json").unlink()
        self.create()
        original = self.encrypted.read_bytes()
        with self.assertRaisesRegex(ValueError, "already exists"):
            self.create()
        self.assertEqual(self.encrypted.read_bytes(), original)
        (self.root / "existing").mkdir(mode=0o700)
        with self.assertRaisesRegex(ValueError, "already exists"):
            self.recover("existing")

    def test_restored_ci_replay_state_is_quarantined_and_longer_deadline_preserved(self):
        config = {"version": 2, "ci_publishers": [{"subject_id": "ci-service"}]}
        self.config.write_text(json.dumps(config))
        self.create()
        before = datetime.now(timezone.utc)
        result = self.recover()
        stamp = datetime.fromisoformat(result["ci_quarantine_until"].replace("Z", "+00:00"))
        self.assertGreaterEqual(stamp, before + timedelta(minutes=11))
        restored = json.loads((self.root / "restored/config.json").read_text())
        self.assertEqual(restored["ci_publishers"], config["ci_publishers"])
        later = (datetime.now(timezone.utc) + timedelta(days=1)).isoformat()
        self.config.write_text(json.dumps({**config, "ci_quarantine_until": later}))
        self.encrypted.unlink()
        self.create()
        extended = self.recover("extended")
        self.assertEqual(datetime.fromisoformat(extended["ci_quarantine_until"].replace("Z", "+00:00")), datetime.fromisoformat(later))


if __name__ == "__main__":
    unittest.main()
