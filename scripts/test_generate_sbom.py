#!/usr/bin/env python3
"""Exercise the actual generator against an independent SPDX 2.3 fixture."""

import json
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest


class PackageVerificationCodeTest(unittest.TestCase):
    def test_known_contents_are_sorted_by_sha1_and_duplicates_are_retained(self):
        # SPDX 2.3 section 7.9: ASCII-sort SHA-1 values, concatenate, SHA-1.
        # https://spdx.github.io/spdx-spec/v2.3/package-information/#79-package-verification-code-field
        # SHA-1(abc) = a9993e364706816aba3e25717850c26c9cd0d89d
        # SHA-1(empty) = da39a3ee5e6b4b0d3255bfef95601890afd80709
        # This known vector includes abc twice; filename order differs from
        # checksum order. Its expected result was independently checked by sha1sum.
        expected_code = "c0590819f3dd230bd9f73c64441af32d82fb3f56"
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            # The generator determines its root from its own location. Keep
            # this copied executable in an excluded directory, outside the fixture.
            script = root / "build" / "generate-sbom.py"
            script.parent.mkdir()
            shutil.copyfile(pathlib.Path(__file__).with_name("generate-sbom.py"), script)
            output = root / "dist" / "fixture.spdx.json"
            for name, contents in (("a-empty", b""), ("m-duplicate", b"abc"), ("z-abc", b"abc")):
                (root / name).write_bytes(contents)

            def generate():
                subprocess.run(
                    [sys.executable, str(script), "--output", str(output), "--version", "fixture"],
                    check=True, capture_output=True, text=True,
                )
                return json.loads(output.read_text())

            document = generate()
            self.assertEqual(
                document["packages"][0]["packageVerificationCode"]["packageVerificationCodeValue"],
                expected_code,
            )
            self.assertEqual([entry["fileName"] for entry in document["files"]],
                             ["./a-empty", "./m-duplicate", "./z-abc"])
            abc_checksums = {entry["algorithm"]: entry["checksumValue"]
                             for entry in document["files"][2]["checksums"]}
            self.assertEqual(abc_checksums, {
                "SHA1": "a9993e364706816aba3e25717850c26c9cd0d89d",
                "SHA256": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
            })

            # File names and their iteration order must not change the code.
            (root / "a-empty").rename(root / "zz-empty")
            renamed = generate()
            self.assertEqual(
                renamed["packages"][0]["packageVerificationCode"]["packageVerificationCodeValue"],
                expected_code,
            )


if __name__ == "__main__":
    unittest.main()
