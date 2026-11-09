import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
INITIALIZERS = sorted((ROOT / "monitor").glob("v*/init.sh"))


class LegacyInitializerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.env = dict(os.environ)
        if sys.platform == "darwin":
            sed = shutil.which("gsed")
            if not sed:
                self.skipTest("GNU sed is required on macOS; deployed images use BusyBox sed")
            tools = self.root / "bin"
            tools.mkdir()
            (tools / "sed").symlink_to(sed)
            self.env["PATH"] = f"{tools}:{self.env['PATH']}"
        self.env.update(
            TIDB_CLUSTER_NAME="test-cluster",
            TIDB_CLUSTER_NAMESPACE="",
            TIDB_VERSION="pingcap/tidb:v3.0.0",
            GF_PROVISIONING_PATH=str(self.root / "grafana"),
            PROM_CONFIG_PATH=str(self.root / "prometheus"),
            GF_DATASOURCE_PATH="",
            GF_K8S_PROMETHEUS_URL="",
            GF_TIDB_PROMETHEUS_URL="",
            PROM_PERSISTENT_DIR="",
        )
        self.titles = {
            "tidb": "TiDB", "overview": "Overview", "pd": "PD",
            "tikv_details": "TiKV-Details", "tikv_summary": "TiKV-Summary",
            "tikv_trouble_shooting": "TiKV-Trouble-Shooting",
            "binlog": "Binlog", "lightning": "Lightning",
        }

    def run_initializer(self, source, pull=False):
        # Isolate only the image's /tmp assets; run the complete release script.
        for path in self.assets.iterdir():
            path.unlink()
        for name, title in self.titles.items():
            (self.assets / f"{name}.json").write_text(f"Test-Cluster-{title}\n")
        if pull:
            (self.assets / "tikv_pull.json").write_text("Test-Cluster-TiKV\n")
        (self.assets / "pods.json").write_text("Test-Cluster-Pods-Info $namespace\n")
        (self.assets / "nodes.json").write_text("nodes\n")
        for name in ("cluster", "persistent"):
            (self.assets / f"{name}.rules.yml").write_text("env: ENV_LABELS_ENV\n")
        for name in ("k8s", "tidb-cluster"):
            shutil.copyfile(source.parent / "datasources" / f"{name}-datasource.yaml",
                            self.assets / f"{name}-datasource.yaml")
        script = self.root / "init.sh"
        script.write_text(source.read_text().replace("/tmp/", f"{self.assets}/"))
        return subprocess.run(["sh", str(script)], env=self.env, cwd=self.root,
                              capture_output=True, text=True, timeout=20)

    def test_all_release_scripts_preserve_outputs(self):
        self.assertEqual(len(INITIALIZERS), 36)
        for source in INITIALIZERS:
            for configured in (False, True):
                with self.subTest(version=source.parent.name, configured=configured):
                    self.env.update(GF_DATASOURCE_PATH="", PROM_PERSISTENT_DIR="")
                    if configured:
                        datasources = self.root / "datasources"
                        datasources.mkdir(exist_ok=True)
                        persistent = self.root / "store/latest-rules/tidb:v3.0.0"
                        persistent.mkdir(parents=True, exist_ok=True)
                        (persistent / "persistent.rules.yml").write_text("saved rules\n")
                        self.env.update(
                            GF_DATASOURCE_PATH=str(datasources),
                            PROM_PERSISTENT_DIR=str(self.root / "store"),
                            TIDB_CLUSTER_NAMESPACE="tenant-a",
                            GF_K8S_PROMETHEUS_URL="http://k8s:9090",
                            GF_TIDB_PROMETHEUS_URL="http://tidb:9090",
                        )
                    result = self.run_initializer(source, pull=configured)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stderr, "")
                    dashboards = self.root / "grafana/dashboards"
                    for name, title in self.titles.items():
                        prefix = "Test-Cluster" if configured and name.startswith("tikv_") else "test-cluster"
                        self.assertEqual((dashboards / f"{name}.json").read_text(), f"{prefix}-{title}\n")
                    rules = self.root / "prometheus/rules"
                    self.assertEqual((rules / "cluster.rules.yml").read_text(), "env: test-cluster\n")
                    self.assertEqual((rules / "persistent.rules.yml").read_text(),
                                     "saved rules\n" if configured else "env: test-cluster\n")
                    if configured:
                        self.assertEqual((dashboards / "tikv_pull.json").read_text(), "test-cluster-TiKV\n")
                        self.assertEqual((dashboards / "pods.json").read_text(), "test-cluster-Pods-Info tenant-a\n")
                        self.assertIn("url: http://k8s:9090", (datasources / "k8s-datasource.yaml").read_text())

    def test_sed_arguments_cannot_write_another_file(self):
        victim = self.root / "victim"
        self.env["TIDB_CLUSTER_NAME"] = f"X -e s/Test-Cluster-TiDB/PWNED/w{victim} -e s/q/"
        for source in INITIALIZERS:
            with self.subTest(version=source.parent.name):
                victim.write_text("untouched\n")
                result = self.run_initializer(source)
                self.assertEqual(victim.read_text(), "untouched\n")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stderr, "")

    def test_replacements_and_paths_are_literal(self):
        source = INITIALIZERS[0]
        victim = self.root / "victim"
        victim.write_text("untouched\n")
        payload = f"X -e s/Test-Cluster-TiDB/PWNED/w{victim} -e s/q/"
        for cluster in ("normal-cluster", payload, r"name&with|slash/and\backslash"):
            with self.subTest(cluster=cluster):
                self.env.update(TIDB_CLUSTER_NAME=cluster)
                for key in ("GF_PROVISIONING_PATH", "PROM_CONFIG_PATH", "GF_DATASOURCE_PATH", "PROM_PERSISTENT_DIR"):
                    path = self.root / f"{key} with spaces"
                    path.mkdir(exist_ok=True)
                    self.env[key] = str(path)
                persistent = Path(self.env["PROM_PERSISTENT_DIR"]) / "latest-rules/tidb:v3.0.0"
                persistent.mkdir(parents=True, exist_ok=True)
                (persistent / "persistent.rules.yml").write_text("saved rules\n")
                url = r"https://metrics.example/query?one=1&two=2,three=3|four=\value"
                self.env.update(GF_K8S_PROMETHEUS_URL=url, GF_TIDB_PROMETHEUS_URL=url)
                result = self.run_initializer(source)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stderr, "")
                dashboards = Path(self.env["GF_PROVISIONING_PATH"]) / "dashboards"
                self.assertEqual((dashboards / "tidb.json").read_text(), f"{cluster}-TiDB\n")
                self.assertEqual((dashboards / "pods.json").read_text(), f"{cluster}-Pods-Info default\n")
                rules = Path(self.env["PROM_CONFIG_PATH"]) / "rules"
                self.assertEqual((rules / "cluster.rules.yml").read_text(), f"env: {cluster}\n")
                self.assertEqual((rules / "persistent.rules.yml").read_text(), "saved rules\n")
                for name in ("k8s", "tidb-cluster"):
                    data = Path(self.env["GF_DATASOURCE_PATH"]) / f"{name}-datasource.yaml"
                    self.assertIn(f"url: {url}", data.read_text())
                self.assertEqual(victim.read_text(), "untouched\n")

    def test_invalid_values_fail_before_modifying_output(self):
        dashboard = self.root / "grafana/dashboards/existing.json"
        dashboard.parent.mkdir(parents=True)
        dashboard.write_text("keep\n")
        for key in ("TIDB_CLUSTER_NAME", "TIDB_CLUSTER_NAMESPACE", "GF_K8S_PROMETHEUS_URL", "GF_TIDB_PROMETHEUS_URL"):
            for value in ("first\nsecond", "first\rsecond"):
                with self.subTest(key=key, value=value):
                    original = self.env[key]
                    self.env[key] = value
                    result = self.run_initializer(INITIALIZERS[0])
                    self.env[key] = original
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(dashboard.read_text(), "keep\n")
        for key in ("GF_PROVISIONING_PATH", "PROM_CONFIG_PATH"):
            with self.subTest(key=key):
                original = self.env[key]
                self.env[key] = ""
                result = self.run_initializer(INITIALIZERS[0])
                self.env[key] = original
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(dashboard.read_text(), "keep\n")


if __name__ == "__main__":
    unittest.main()
