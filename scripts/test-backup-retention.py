#!/usr/bin/env python3
"""Exercise destructive retention selection only inside a disposable directory."""
from pathlib import Path
import importlib.util
import os
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('bwb_backup_retention', Path(__file__).with_name('backup-local.py'))
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)


class RetentionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.original = backup.BACKUPS
        backup.BACKUPS = Path(self.temp.name)
        self.addCleanup(setattr, backup, 'BACKUPS', self.original)

    def create(self, name):
        path = backup.BACKUPS / name
        path.write_bytes(b'encrypted-fixture')
        path.chmod(0o600)
        return path

    def test_latest_per_day_and_fourteen_days(self):
        for day in range(1, 17):
            self.create(f'bwb-202610{day:02}T120000Z-aaaaaaaa.bwb')
            self.create(f'bwb-202610{day:02}T130000Z-bbbbbbbb.bwb')
        unrelated = self.create('keep-unrelated.bwb')
        partial = self.create('bwb-20261016T140000Z-cccccccc.bwb.partial')
        self.assertEqual(backup.retain_daily(), 14)
        retained = sorted(p.name for p in backup.BACKUPS.iterdir() if backup.NAME.fullmatch(p.name))
        self.assertEqual(len(retained), 14)
        self.assertTrue(all('T130000Z' in name for name in retained))
        self.assertIn('20261003', retained[0])
        self.assertIn('20261016', retained[-1])
        self.assertTrue(unrelated.exists())
        self.assertTrue(partial.exists())

    def test_refuses_symlink_candidate(self):
        target = self.create('unrelated-private-file')
        link = backup.BACKUPS / 'bwb-20261016T120000Z-aaaaaaaa.bwb'
        os.symlink(target, link)
        with self.assertRaises(RuntimeError):
            backup.retain_daily()
        self.assertTrue(target.exists())


if __name__ == '__main__':
    unittest.main()
