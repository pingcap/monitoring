import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent


class BuildSecurityTests(unittest.TestCase):
    def test_release_tag(self):
        for ref in ("reloader/v1.2.3", "reloader/v1.2.3-rc.1"):
            result = subprocess.run(["bash", str(SCRIPTS / "reloader_image_tag.sh"), ref], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), ref.removeprefix("reloader/"))
        for ref in ("reloader/v1.0,pingcap/tidb", "reloader/v1.2.3\nother/image", "reloader/v1.2.3+meta", "other/v1.2.3", "reloader/v1", "reloader/v1.2.3-" + "a" * 128):
            result = subprocess.run(["bash", str(SCRIPTS / "reloader_image_tag.sh"), ref], capture_output=True)
            self.assertNotEqual(result.returncode, 0, ref)
            self.assertEqual(result.stdout, b"")

    def test_grafana_build_verifies_before_extracting(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(SCRIPTS, root / "scripts")
            (root / "dependencies.yaml").write_text("grafana: v7.5.17\n")
            dashboards = root / "output/dashboards/tiup"
            dashboards.mkdir(parents=True)
            (dashboards / "dashboard.json").write_text("{}")
            fixture = root / "fixture.tar.gz"
            with tarfile.open(fixture, "w:gz") as archive:
                member = tarfile.TarInfo("grafana-7.5.17/bin/grafana-server")
                member.size = 6
                archive.addfile(member, io.BytesIO(b"binary"))
            # Replace the network only; exercise both real build and verification scripts.
            bin_dir = root / "bin"
            bin_dir.mkdir()
            curl = bin_dir / "curl"
            curl.write_text('#!/usr/bin/env bash\nset -eu\nwhile [[ "$1" != --output ]]; do shift; done\ncp "$FIXTURE" "$2"\n')
            curl.chmod(0o755)
            env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}", FIXTURE=str(fixture), TARGET_OS="linux", TARGET_ARCH="amd64")
            checksum_file = root / "scripts/artifact-checksums.sha256"
            artifact = root / "output/grafana-linux-amd64.tar.gz"
            extracted = root / "output/grafana-linux-amd64/grafana/bin/grafana-server"
            filename = "grafana-7.5.17.linux-amd64.tar.gz"
            for checksum in ("", "0" * 64):
                checksum_file.write_text(f"{checksum}  {filename}\n")
                result = subprocess.run(["bash", "scripts/build_tiup_grafana.sh"], cwd=root, env=env, capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(artifact.exists())
                self.assertFalse(extracted.exists())
            digest = hashlib.sha256(fixture.read_bytes()).hexdigest()
            checksum_file.write_text(f"{digest}  {filename}\n")
            result = subprocess.run(["bash", "scripts/build_tiup_grafana.sh"], cwd=root, env=env, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(extracted.read_bytes(), b"binary")
            self.assertTrue(artifact.exists())

    def test_prometheus_build_verifies_before_extracting(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(SCRIPTS, root / "scripts")
            (root / "dependencies.yaml").write_text("prometheus: v2.49.1\n")
            for location in ("monitor-snapshot/release-2.49/operator/rules", "platform-monitoring/ansible/rule"):
                rules = root / location
                rules.mkdir(parents=True)
                (rules / "test.rules.yml").write_text("groups: []\n")
            fixture = root / "fixture.tar.gz"
            dirname = "prometheus-2.49.1.linux-amd64"
            with tarfile.open(fixture, "w:gz") as archive:
                directory_entry = tarfile.TarInfo(dirname)
                directory_entry.type = tarfile.DIRTYPE
                directory_entry.mode = 0o755
                archive.addfile(directory_entry)
                member = tarfile.TarInfo(f"{dirname}/prometheus")
                member.size = 6
                archive.addfile(member, io.BytesIO(b"binary"))
            bin_dir = root / "bin"
            bin_dir.mkdir()
            for name, script in {
                "curl": '#!/usr/bin/env bash\nset -eu\nwhile [[ "$1" != --output ]]; do shift; done\ncp "$FIXTURE" "$2"\n',
                "git": '#!/usr/bin/env bash\necho v2.49.1\n',
            }.items():
                executable = bin_dir / name
                executable.write_text(script)
                executable.chmod(0o755)
            env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}", FIXTURE=str(fixture), TARGET_OS="linux", TARGET_ARCH="amd64")
            checksum_file = root / "scripts/artifact-checksums.sha256"
            extracted = root / "output/prometheus/prometheus"
            for digest in ("0" * 64, hashlib.sha256(fixture.read_bytes()).hexdigest()):
                checksum_file.write_text(f"{digest}  {dirname}.tar.gz\n")
                result = subprocess.run(["bash", "scripts/prepare_prometheus.sh"], cwd=root, env=env, capture_output=True)
                if digest == "0" * 64:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse(extracted.exists())
                else:
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(extracted.read_bytes(), b"binary")


if __name__ == "__main__":
    unittest.main()
