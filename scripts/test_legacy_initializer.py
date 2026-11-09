import json
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
            TIDB_CLUSTER_NAME="test-cluster", TIDB_CLUSTER_NAMESPACE="",
            TIDB_VERSION="pingcap/tidb:v3.0.0",
            GF_PROVISIONING_PATH=str(self.root / "grafana"),
            PROM_CONFIG_PATH=str(self.root / "prometheus"),
            GF_DATASOURCE_PATH="", GF_K8S_PROMETHEUS_URL="",
            GF_TIDB_PROMETHEUS_URL="", PROM_PERSISTENT_DIR="",
        )

    def run_initializer(self, source):
        # Run the complete script against real release assets in an isolated tree.
        for path in self.assets.iterdir():
            path.unlink()
        for kind in ("dashboards", "rules", "datasources"):
            for path in (source.parent / kind).iterdir():
                if path.is_file():
                    shutil.copyfile(path, self.assets / path.name)
        script = self.root / "init.sh"
        script.write_text(source.read_text().replace("/tmp/", f"{self.assets}/"))
        return subprocess.run(["sh", str(script)], env=self.env, cwd=self.root,
                              capture_output=True, text=True, timeout=20)

    def test_all_releases_keep_existing_output_format(self):
        self.assertEqual(len(INITIALIZERS), 36)
        self.assertEqual(len({p.read_bytes() for p in INITIALIZERS}), 1)
        datasources = self.root / "datasources"
        datasources.mkdir()
        for source in INITIALIZERS:
            for grafana, provision in ((False, False), (False, True), (True, False), (True, True)):
                with self.subTest(version=source.parent.name, grafana=grafana, datasources=provision):
                    dashboard_root = self.root / "grafana"
                    shutil.rmtree(dashboard_root, ignore_errors=True)
                    self.env.update(
                        GF_PROVISIONING_PATH=str(dashboard_root) if grafana else "",
                        GF_DATASOURCE_PATH=str(datasources) if provision else "",
                    )
                    result = self.run_initializer(source)
                    self.assertEqual((result.returncode, result.stderr), (0, ""))
                    for template in (source.parent / "rules").glob("*.rules.yml"):
                        expected = template.read_text().replace("ENV_LABELS_ENV", "test-cluster")
                        self.assertEqual((self.root / "prometheus/rules" / template.name).read_text(), expected)
                    self.assertEqual(dashboard_root.exists(), grafana)
                    if grafana:
                        for template in (source.parent / "dashboards").glob("*.json"):
                            if not provision and template.name in ("pods.json", "nodes.json"):
                                continue
                            expected = template.read_text().replace("Test-Cluster-", "test-cluster-")
                            if template.name == "pods.json":
                                expected = expected.replace("$namespace", "default")
                            actual = (dashboard_root / "dashboards" / template.name).read_text()
                            self.assertEqual(actual, expected)
                            json.loads(actual)
                    if provision:
                        for template in (source.parent / "datasources").glob("*.yaml"):
                            self.assertEqual((datasources / template.name).read_bytes(), template.read_bytes())

    def test_paths_urls_and_persisted_rules(self):
        for key in ("GF_PROVISIONING_PATH", "PROM_CONFIG_PATH", "GF_DATASOURCE_PATH", "PROM_PERSISTENT_DIR"):
            path = self.root / f"{key} with spaces"
            path.mkdir()
            self.env[key] = str(path)
        saved = Path(self.env["PROM_PERSISTENT_DIR"]) / "latest-rules/tidb:v3.0.0"
        saved.mkdir(parents=True)
        (saved / "tidb.rules.yml").write_text("saved rules\n")
        victim = self.root / "victim"
        victim.write_text("untouched\n")
        url = rf"https://metrics.example/query?a=1&b=2,three=3|four=\value;w{victim}"
        self.env.update(GF_K8S_PROMETHEUS_URL=url, GF_TIDB_PROMETHEUS_URL=url, TIDB_CLUSTER_NAMESPACE="tenant-a")
        source = INITIALIZERS[0]
        result = self.run_initializer(source)
        self.assertEqual((result.returncode, result.stderr), (0, ""))
        self.assertEqual(victim.read_text(), "untouched\n")
        self.assertEqual((Path(self.env["PROM_CONFIG_PATH"]) / "rules/tidb.rules.yml").read_text(), "saved rules\n")
        pods = Path(self.env["GF_PROVISIONING_PATH"]) / "dashboards/pods.json"
        expected = (source.parent / "dashboards/pods.json").read_text().replace("Test-Cluster-", "test-cluster-").replace("$namespace", "tenant-a")
        self.assertEqual(pods.read_text(), expected)
        for name, original in (("k8s", "http://prometheus-k8s.monitoring.svc:9090"), ("tidb-cluster", "http://127.0.0.1:9090")):
            filename = f"{name}-datasource.yaml"
            expected = (source.parent / "datasources" / filename).read_text().replace(original, url)
            self.assertEqual((Path(self.env["GF_DATASOURCE_PATH"]) / filename).read_text(), expected)

    def test_unsafe_values_fail_before_any_output_changes(self):
        existing = []
        for relative in ("grafana/dashboards/existing.json", "prometheus/rules/existing.yml"):
            path = self.root / relative
            path.parent.mkdir(parents=True)
            path.write_text("keep\n")
            existing.append(path)
        victim = self.root / "victim"
        victim.write_text("untouched\n")
        payload = f"X -e s/Test-Cluster-TiDB/PWNED/w{victim} -e s/q/"
        cases = {key: [payload, "&", "-", r"team\qa", 'team"qa', "team'qa", '{{ "other-cluster" }}', "team\nqa", "team\rqa", "team\tqa", "team\u0085qa"]
                 for key in ("TIDB_CLUSTER_NAME", "TIDB_CLUSTER_NAMESPACE")}
        for key in ("GF_K8S_PROMETHEUS_URL", "GF_TIDB_PROMETHEUS_URL"):
            cases[key] = ["https://example.com" + c + "    jsonData: {tlsSkipVerify: true}" for c in ("\n", "\r", "\u0085", "\u2028", "\u2029")]
            cases[key] += ["https://example.com\n", "https://example.com\t", "https://example.com:", "file:///tmp/rules", "https://"]
        cases["PROM_CONFIG_PATH"] = [""]
        for key, values in cases.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    old = self.env[key]
                    self.env[key] = value
                    result = self.run_initializer(INITIALIZERS[0])
                    self.env[key] = old
                    self.assertNotEqual(result.returncode, 0)
                    for path in existing:
                        self.assertEqual(path.read_text(), "keep\n")
                    self.assertEqual(victim.read_text(), "untouched\n")
                    self.assertEqual(sorted((self.root / "grafana/dashboards").iterdir()), existing[:1])
                    self.assertEqual(sorted((self.root / "prometheus/rules").iterdir()), existing[1:])


if __name__ == "__main__":
    unittest.main()
