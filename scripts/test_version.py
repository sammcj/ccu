import subprocess
import tempfile
import unittest
from pathlib import Path

from version import stamp_changelog

HEAD = "# Changelog\n\n<!-- AI agents: x -->\n\n"
OLD = "## [0.1.0] - 2026-01-01\n\n- old\n"


def git(root: Path, *args: str) -> None:
    subprocess.run(
        ["git", "-c", "user.email=t@t", "-c", "user.name=t", *args],
        cwd=root,
        check=True,
        capture_output=True,
    )


class StampChangelogTest(unittest.TestCase):
    def setUp(self) -> None:
        self._dir = tempfile.TemporaryDirectory()
        self.root = Path(self._dir.name)
        git(self.root, "init", "-q")
        # v0.2.0 shipped entry A.
        self.write("## [Unreleased]\n\n### Added\n\n- A\n\n")
        git(self.root, "add", ".")
        git(self.root, "commit", "-qm", "release")
        git(self.root, "tag", "v0.2.0")
        # B and C were added after the tag.
        self.write("## [Unreleased]\n\n### Added\n\n- A\n- B\n\n### Fixed\n\n- C\n\n")

    def tearDown(self) -> None:
        self._dir.cleanup()

    def write(self, body: str) -> None:
        (self.root / "CHANGELOG.md").write_text(HEAD + body + OLD, encoding="utf-8")

    def read(self) -> str:
        return (self.root / "CHANGELOG.md").read_text(encoding="utf-8")

    def section(self, heading: str) -> str:
        text = self.read()
        start = text.index(heading)
        end = text.find("\n## ", start + 1)
        return text[start:end]

    def test_only_entries_present_at_ref_are_stamped(self) -> None:
        self.assertTrue(stamp_changelog(self.root, "0.2.0", False, "v0.2.0"))
        unreleased = self.section("## [Unreleased]")
        stamped = self.section("## [0.2.0]")
        self.assertIn("- B", unreleased)
        self.assertIn("- C", unreleased)
        self.assertIn("### Fixed", unreleased)
        self.assertNotIn("- A", unreleased)
        self.assertIn("### Added\n\n- A\n", stamped)
        self.assertNotIn("- B", stamped)
        self.assertNotIn("### Fixed", stamped)
        self.assertIn(OLD, self.read())

    def test_nothing_released_is_a_no_op(self) -> None:
        self.write("## [Unreleased]\n\n### Added\n\n- B\n\n")
        before = self.read()
        self.assertFalse(stamp_changelog(self.root, "0.2.0", False, "v0.2.0"))
        self.assertEqual(before, self.read())

    def test_existing_version_heading_is_a_no_op(self) -> None:
        self.write("## [Unreleased]\n\n### Added\n\n- A\n\n## [0.2.0] - 2026-01-02\n\n- A\n\n")
        before = self.read()
        self.assertFalse(stamp_changelog(self.root, "0.2.0", False, "v0.2.0"))
        self.assertEqual(before, self.read())

    def test_without_ref_everything_is_stamped(self) -> None:
        self.assertTrue(stamp_changelog(self.root, "0.3.0", False))
        self.assertIn("## [Unreleased]\n\n## [0.3.0]", self.read())
        stamped = self.section("## [0.3.0]")
        for entry in ("- A", "- B", "- C"):
            self.assertIn(entry, stamped)


if __name__ == "__main__":
    unittest.main()
