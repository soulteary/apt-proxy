#!/usr/bin/env python3
"""Exercise the real matrix entrypoint without building images or running APT."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


STUB = r'''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import re
import sys

name = Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["MATRIX_CALLS"], "a") as calls:
    calls.write(json.dumps([name] + args) + "\n")
stage = os.environ.get("MATRIX_FAIL_STAGE")
if name == "go":
    if stage == "go":
        sys.exit(71)
    Path(args[args.index("-o") + 1]).write_text("stub binary")
elif args[0] == "info":
    if stage == "info":
        sys.exit(72)
    print("amd64")
elif args[0] == "build":
    target = args[args.index("-t") + 1]
    tag = target.split(":", 1)[1]
    if re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", tag) is None:
        print("invalid Docker tag: " + target, file=sys.stderr)
        sys.exit(73)
    if stage == "build":
        sys.exit(74)
elif args[0] == "run":
    mount = args[args.index("--mount") + 1]
    source = next(part[4:] for part in mount.split(",") if part.startswith("src="))
    Path(source, "result.txt").write_text("PASS\n")
else:
    raise AssertionError(args)
'''


class MatrixTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="apt-matrix-identifiers-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.repo = self.root / "repository"
        fixtures = self.repo / "tests" / "apt-client"
        fixtures.mkdir(parents=True)
        script = Path(os.environ.get("MATRIX_SCRIPT", Path(__file__).with_name("matrix.sh")))
        self.script = fixtures / "matrix.sh"
        shutil.copyfile(script, self.script)
        for name in ("Dockerfile", "fixture.py", "run.sh"):
            (fixtures / name).write_text("unused stub fixture\n")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for name in ("docker", "go"):
            executable = self.bin / name
            executable.write_text(STUB)
            executable.chmod(0o755)
        self.calls_file = self.root / "calls.jsonl"
        self.evidence = self.root / "evidence"
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        MATRIX_CALLS=str(self.calls_file), APT_CLIENT_EVIDENCE_DIR=str(self.evidence))

    def run_matrix(self, *images, cwd=None, **environment):
        return subprocess.run(["/bin/bash", str(self.script), *images], cwd=cwd or self.root,
                              env=dict(self.env, **environment), text=True, capture_output=True)

    def calls(self):
        if not self.calls_file.exists():
            return []
        return [json.loads(line) for line in self.calls_file.read_text().splitlines()]

    def assert_successful_images(self, result, images):
        self.assertEqual(result.returncode, 0, result.stderr)
        builds = [call for call in self.calls() if call[:2] == ["docker", "build"]]
        runs = [call for call in self.calls() if call[:2] == ["docker", "run"]]
        self.assertEqual(len(builds), len(images))
        self.assertEqual(len(runs), len(images))
        ids = []
        for build, run, image in zip(builds, runs, images):
            self.assertIn("APT_CLIENT_IMAGE=" + image, build)
            target = build[build.index("-t") + 1]
            image_id = target.removeprefix("apt-proxy-client-test:")
            self.assertRegex(image_id, r"^[a-z0-9_][a-z0-9_.-]{0,127}$")
            self.assertEqual(run[-1], target)
            self.assertIn("src=" + str(self.evidence / image_id) + ",", run[run.index("--mount") + 1])
            self.assertEqual((self.evidence / image_id / "result.txt").read_text(), "PASS\n")
            ids.append(image_id)
        return ids

    def test_default_image_ids_stay_stable(self):
        images = ("debian:bookworm-slim", "debian:trixie-slim", "ubuntu:22.04", "ubuntu:24.04", "ubuntu:26.04")
        ids = self.assert_successful_images(self.run_matrix(), images)
        self.assertEqual(ids, ["debian-bookworm-slim", "debian-trixie-slim", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04"])

    def test_digest_and_registry_port_references(self):
        digest = "a" * 64
        images = ("debian:bookworm-slim@sha256:" + digest,
                  "registry.example.com:5000/team/debian:bookworm@sha256:" + digest,
                  "[2001:db8::1]:5000/team/debian:bookworm")
        ids = self.assert_successful_images(self.run_matrix(*images), images)
        self.assertNotIn("@", ids[0])
        self.assertNotIn(":", ids[1])
        self.assertTrue(ids[2].startswith("image-"))

    def test_long_references_keep_distinct_bounded_ids(self):
        prefix = "registry.example.com/" + "a" * 90 + "/package:"
        images = (prefix + "one@sha256:" + "a" * 64, prefix + "two@sha256:" + "b" * 64)
        ids = self.assert_successful_images(self.run_matrix(*images), images)
        self.assertEqual(len(ids[0]), 128)
        self.assertEqual(len(ids[1]), 128)
        self.assertNotEqual(ids[0], ids[1])

    def test_collisions_fail_before_evidence_cleanup(self):
        cases = (("registry/team:image", "registry-team:image", "registry-team-image"),
                 ("repo:tag", "repo:TAG", "repo-tag"),
                 ("repo:tag", "repo:tag", "repo-tag"))
        for first, second, image_id in cases:
            with self.subTest(first=first, second=second):
                evidence = self.evidence / image_id
                evidence.mkdir(parents=True, exist_ok=True)
                previous = evidence / "result.txt"
                previous.write_text("previous PASS\n")
                result = self.run_matrix(first, second)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("share an evidence identifier", result.stderr)
                self.assertEqual(previous.read_text(), "previous PASS\n")
                self.assertEqual(self.calls(), [])

    def test_failures_clear_only_selected_previous_evidence(self):
        for stage in ("info", "go", "build"):
            with self.subTest(stage=stage):
                selected = self.evidence / "debian-bookworm-slim"
                unrelated = self.evidence / "ubuntu-24.04"
                selected.mkdir(parents=True, exist_ok=True)
                unrelated.mkdir(parents=True, exist_ok=True)
                for name in ("result.txt", "proxy.log", "offline.log"):
                    (selected / name).write_text("old PASS/log\n")
                (unrelated / "result.txt").write_text("unrelated PASS\n")
                result = self.run_matrix("debian:bookworm-slim", MATRIX_FAIL_STAGE=stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(list(selected.iterdir()), [])
                self.assertEqual((unrelated / "result.txt").read_text(), "unrelated PASS\n")

    def test_relative_evidence_root_is_mounted_from_callers_directory(self):
        caller = self.root / "outside repository"
        caller.mkdir()
        self.evidence = caller / "evidence with spaces"
        result = self.run_matrix("debian:bookworm-slim", cwd=caller,
                                 APT_CLIENT_EVIDENCE_DIR="evidence with spaces")
        self.assert_successful_images(result, ("debian:bookworm-slim",))

    def test_invalid_empty_or_dot_identifiers_stop_before_cleanup(self):
        for image in ("", ".", ".."):
            with self.subTest(image=image):
                self.assertNotEqual(self.run_matrix(image).returncode, 0)
                self.assertEqual(self.calls(), [])


if __name__ == "__main__":
    unittest.main()
