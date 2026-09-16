import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('baseline', Path(__file__).with_name('baseline.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


class SnapshotTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'check.sh').write_text('#!/bin/sh\nexit 0\n')
        (self.root / 'check.sh').chmod(0o755)
        self.contract = {'files': ['check.sh', 'missing.txt']}
        self.source = {'run_id': '42', 'sha': 'abc'}

    def snapshot(self):
        return b.snapshot(self.root, self.contract, self.source)

    def test_roundtrip_preserves_mode_and_absence(self):
        document = self.snapshot()
        target = self.root / 'restored'
        b.materialize(document, self.contract, target, self.source)
        self.assertEqual((target / 'check.sh').read_bytes(), (self.root / 'check.sh').read_bytes())
        self.assertEqual((target / 'check.sh').stat().st_mode & 0o777, 0o755)
        self.assertFalse((target / 'missing.txt').exists())
        with self.assertRaises(FileExistsError):
            b.materialize(document, self.contract, target, self.source)

    def test_rejects_unsafe_and_credential_paths(self):
        for name in ['../escape', '/tmp/x', 'a/../b', './a', 'a//b', '.', '.git/config', '.env',
                     '.env.production', '.ssh/id_rsa', '.aws/credentials', 'a.key', 'config\nname', 'a\\b', '**']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                b.safe_path(name)

    def test_rejects_links_and_special_files(self):
        (self.root / 'link').symlink_to('check.sh')
        (self.root / 'directory').symlink_to(self.root, target_is_directory=True)
        os.link(self.root / 'check.sh', self.root / 'hardlink')
        os.mkfifo(self.root / 'fifo')
        for name in ['link', 'directory/check.sh', 'hardlink', 'fifo']:
            with self.subTest(name=name), self.assertRaises((OSError, ValueError)):
                b.read_selected(self.root, name)

    def test_rejects_private_key_and_size_limit(self):
        (self.root / 'secret.txt').write_text('-----BEGIN RSA PRIVATE KEY-----')
        with self.assertRaises(ValueError):
            b.read_selected(self.root, 'secret.txt')
        with patch.object(b, 'MAX_BYTES', 4), self.assertRaises(ValueError):
            self.snapshot()

    def test_rejects_tampering_and_wrong_provenance(self):
        original = self.snapshot()
        mutations = [lambda d: d.update(schema='future'),
                     lambda d: d['contract'].update(files=['other']),
                     lambda d: d['source'].update(run_id='99'),
                     lambda d: d['entries'][0].update(path='../escape'),
                     lambda d: d['entries'][0].update(sha256='0' * 64),
                     lambda d: d['entries'][0].update(mode=0o4777),
                     lambda d: d['entries'][0].update(data='%%%'),
                     lambda d: d['entries'].append(d['entries'][0]),
                     lambda d: d['entries'][1].update(path='check.sh')]
        for mutate in mutations:
            document = copy.deepcopy(original)
            mutate(document)
            with self.subTest(mutate=mutate), self.assertRaises((ValueError, KeyError)):
                b.validate(document, self.contract, self.source)

    def test_overlapping_paths_and_json_limit(self):
        contract = {'files': ['check.sh', 'check.sh/child']}
        document = {'schema': b.SCHEMA, 'contract': contract,
                    'entries': [{'path': x, 'absent': True} for x in contract['files']]}
        with self.assertRaises(ValueError):
            b.validate(document, contract)
        path = self.root / 'large.json'
        path.write_text('{}')
        with patch.object(b, 'MAX_JSON', 1), self.assertRaises(ValueError):
            b.load(path)


@unittest.skipUnless(os.environ.get('WORLDBISECT_TEST_BINARY'), 'set WORLDBISECT_TEST_BINARY for real engine E2E')
class RealEngineTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.workspace = self.root / 'workspace'
        self.workspace.mkdir()
        (self.workspace / 'check.sh').write_text('#!/bin/sh\ngrep -qx "mode=good" config.txt\n')
        (self.workspace / 'check.sh').chmod(0o755)
        (self.workspace / 'config.txt').write_text('mode=good\n')
        self.env = {'RUNNER_TEMP': str(self.root), 'GITHUB_WORKSPACE': str(self.workspace),
                    'GITHUB_REPOSITORY': 'acme/test', 'GITHUB_WORKFLOW_REF': 'acme/test/.github/workflows/ci.yml@refs/heads/main',
                    'GITHUB_JOB': 'test', 'GITHUB_RUN_ID': '42', 'GITHUB_SHA': 'good-sha',
                    'GITHUB_EVENT_NAME': 'push', 'GITHUB_OUTPUT': str(self.root / 'outputs'),
                    'INPUT_COMMAND': '["./check.sh"]', 'INPUT_FILES': 'check.sh\nconfig.txt',
                    'INPUT_TIMEOUT': '2', 'INPUT_DIAGNOSTIC_TIMEOUT': '15',
                    'WORLDBISECT_BINARY': os.environ['WORLDBISECT_TEST_BINARY'],
                    'SECRET_TOKEN': 'must-not-be-inherited'}
        self.patcher = patch.dict(os.environ, self.env)
        self.patcher.start()
        self.addCleanup(self.patcher.stop)

    def prepare(self):
        Path(os.environ['GITHUB_OUTPUT']).write_text('')
        b.prepare()
        outputs = dict(line.split('=', 1) for line in Path(os.environ['GITHUB_OUTPUT']).read_text().splitlines())
        return Path(outputs['root'])

    def pair(self):
        good = self.prepare()
        self.assertEqual(b.load(good / 'state.json')['status'], 'PASSED')
        (self.workspace / 'config.txt').write_text('mode=bad\n')
        os.environ['GITHUB_RUN_ID'] = '43'
        os.environ['GITHUB_SHA'] = 'bad-sha'
        bad = self.prepare()
        (bad / 'download').mkdir()
        (bad / 'download' / 'baseline.json').write_bytes((good / 'inputs.json').read_bytes())
        os.environ.update(BASELINE_LOOKUP='success', BASELINE_DOWNLOAD='success',
                          BASELINE_RUN_ID='42', BASELINE_SHA='good-sha')
        return good, bad

    def test_real_file_cause_and_redacted_reports(self):
        good, bad = self.pair()
        b.diagnose(bad)
        state = b.load(bad / 'state.json')
        self.assertEqual(state['status'], 'PROVEN')
        self.assertEqual(state['command_exit'], 1)
        b.finish(bad)
        for path in (bad / 'artifacts').iterdir():
            self.assertNotIn('must-not-be-inherited', path.read_text())
            self.assertNotIn('mode=bad', path.read_text())
        self.assertIn('actions/runs/42', (bad / 'artifacts/summary.md').read_text())
        # Originals have not been modified by capture or intervention.
        self.assertEqual((self.workspace / 'config.txt').read_text(), 'mode=bad\n')

    def test_missing_baseline_keeps_failure(self):
        (self.workspace / 'config.txt').write_text('mode=bad\n')
        root = self.prepare()
        with patch.dict(os.environ, {'BASELINE_LOOKUP': 'success', 'BASELINE_RUN_ID': ''}):
            b.diagnose(root)
        self.assertEqual(b.load(root / 'state.json')['status'], 'BASELINE_MISSING')
        self.assertEqual(b.load(root / 'state.json')['command_exit'], 1)

    def test_baseline_no_longer_passes(self):
        _, bad = self.pair()
        document = b.load(bad / 'download/baseline.json')
        # Valid snapshot whose formerly working script now depends on unavailable host state.
        data = b'#!/bin/sh\nexit 1\n'
        document['entries'][0]['data'] = b.base64.b64encode(data).decode()
        document['entries'][0]['sha256'] = b.hashlib.sha256(data).hexdigest()
        b.save(bad / 'download/baseline.json', document)
        b.diagnose(bad)
        self.assertEqual(b.load(bad / 'state.json')['status'], 'BASELINE_NOT_REPRODUCIBLE')

    def test_repeated_failure_disappears(self):
        _, bad = self.pair()
        # Synthetic host-dependent behavior: fail once, pass on repeat.
        flag = self.root / 'once'
        script = f'#!/bin/sh\nif test -f "{flag}"; then exit 0; fi\ntouch "{flag}"\nexit 1\n'
        document = b.load(bad / 'inputs.json')
        data = script.encode()
        document['entries'][0]['data'] = b.base64.b64encode(data).decode()
        document['entries'][0]['sha256'] = b.hashlib.sha256(data).hexdigest()
        b.save(bad / 'inputs.json', document)
        flag.touch()
        b.diagnose(bad)
        self.assertEqual(b.load(bad / 'state.json')['status'], 'FAILURE_NOT_REPRODUCIBLE')

    def test_wrong_baseline_and_permission_failure(self):
        _, bad = self.pair()
        with patch.dict(os.environ, {'BASELINE_SHA': 'untrusted'}):
            b.diagnose(bad)
        self.assertEqual(b.load(bad / 'state.json')['status'], 'BASELINE_INVALID')
        state = b.load(bad / 'state.json')
        state['status'] = 'BASELINE_MISSING'
        b.save(bad / 'state.json', state)
        with patch.dict(os.environ, {'BASELINE_LOOKUP': 'failure'}):
            b.diagnose(bad)
        self.assertEqual(b.load(bad / 'state.json')['status'], 'BASELINE_UNAVAILABLE')

    def test_timeout_and_missing_executable(self):
        (self.workspace / 'check.sh').write_text('#!/bin/sh\nsleep 10\n')
        root = self.prepare()
        self.assertEqual(b.load(root / 'state.json')['status'], 'COMMAND_TIMEOUT')
        os.environ['INPUT_COMMAND'] = '["./not-present"]'
        root = self.prepare()
        self.assertEqual(b.load(root / 'state.json')['status'], 'COMMAND_ERROR')

    def test_no_inherited_secrets(self):
        (self.workspace / 'check.sh').write_text('#!/bin/sh\ntest -z "${SECRET_TOKEN:-}"\n')
        root = self.prepare()
        self.assertEqual(b.load(root / 'state.json')['status'], 'PASSED')


if __name__ == '__main__':
    unittest.main()
