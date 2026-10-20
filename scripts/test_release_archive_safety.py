import io
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
CHECK = ROOT / "scripts" / "check-release-archives.sh"


def archive(path, member):
    content = b"release fixture\n"
    with tarfile.open(path, "w:gz") as output:
        info = tarfile.TarInfo(member)
        info.size = len(content)
        output.addfile(info, io.BytesIO(content))


class ReleaseArchiveSafetyTests(unittest.TestCase):
    def run_check(self, *paths):
        return subprocess.run(
            [str(CHECK), *(str(path) for path in paths)],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

    def test_accepts_complete_safe_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            safe = Path(directory) / "safe.tar.gz"
            archive(safe, "worldbisect/bin/worldbisect")
            result = self.run_check(safe)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_rejects_parent_traversal(self):
        with tempfile.TemporaryDirectory() as directory:
            unsafe = Path(directory) / "unsafe.tar.gz"
            archive(unsafe, "../escape")
            result = self.run_check(unsafe)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("unsafe archive path", result.stderr)

    def test_rejects_truncated_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            complete = Path(directory) / "complete.tar.gz"
            truncated = Path(directory) / "truncated.tar.gz"
            archive(complete, "worldbisect/bin/worldbisect")
            data = complete.read_bytes()
            truncated.write_bytes(data[: len(data) // 2])
            result = self.run_check(truncated)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("unreadable or truncated", result.stderr)


if __name__ == "__main__":
    unittest.main()
