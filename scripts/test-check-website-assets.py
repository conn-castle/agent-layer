"""Exercise the asset checker through its CLI using generated-site fixtures."""

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

CHECKER = Path(__file__).with_name("check-website-assets.py")


class WebsiteAssetsTest(unittest.TestCase):
    def run_check(self, html, assets=()):
        """Build a small site and return the checker process result."""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "docs").mkdir()
            (root / "docs/index.html").write_text(html)
            for asset in assets:
                path = root / asset
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("asset")
            return subprocess.run(
                [sys.executable, str(CHECKER), str(root)],
                capture_output=True, text=True, check=False,
            )

    def test_existing_relative_absolute_and_external_assets(self):
        result = self.run_check('''
            <img src="../img/logo%20mark.svg?v=2&amp;x=1#mark">
            <script src="https://agent-layer.dev/app.js"></script>
            <iframe src="/planner/"></iframe>
            <img src="https://external.example/missing.svg">
            <img src="data:image/png;base64,AAAA">
            <a href="/not-an-asset">ordinary link</a>
        ''', ["img/logo mark.svg", "app.js", "planner/index.html"])
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_missing_assets_report_page_and_reference(self):
        for html, reference in [
            ('<img src="/img/logos/grok.svg">', '/img/logos/grok.svg'),
            ('<script src="/app.js"></script>', '/app.js'),
            ('<link rel="stylesheet" href="/style.css">', '/style.css'),
            ('<iframe src="/planner/"></iframe>', '/planner/'),
            ('<meta property="og:image" content="/social.png">', '/social.png'),
            ('<video poster="/poster.png"></video>', '/poster.png'),
        ]:
            with self.subTest(reference=reference):
                result = self.run_check(html)
                self.assertEqual(result.returncode, 1)
                self.assertIn('docs/index.html', result.stderr)
                self.assertIn(reference, result.stderr)

    def test_base_url_is_respected(self):
        result = self.run_check('<base href="/media/"><img src="logo.svg">',
                                ['media/logo.svg'])
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_canonical_route_controls_relative_resolution(self):
        result = self.run_check(
            '<link rel="canonical" href="https://agent-layer.dev/docs">'
            '<img src="logo.svg">', ['logo.svg'])
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_absent_build_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run([sys.executable, str(CHECKER), directory],
                                    capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn('no generated HTML', result.stderr)


if __name__ == '__main__':
    unittest.main()
