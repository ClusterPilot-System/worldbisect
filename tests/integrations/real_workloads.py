#!/usr/bin/env python3
"""Controlled regressions in real tooling and a pinned upstream test suite.

These are reproducible integration checks, not customer incident reports.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[2]
UPSTREAM_SHA = '096c8d42545d3b68ea21a4f890fb2b2d8979c0bd'
spec = importlib.util.spec_from_file_location('baseline', REPO / 'actions/ci/baseline.py')
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


def write(root, name, text, executable=False):
    path = root / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    if executable:
        path.chmod(0o755)


def run_case(name, workspace, command, changed_file, bad_content, binary, scratch):
    files = sorted(str(path.relative_to(workspace)) for path in workspace.rglob('*') if path.is_file())
    env = {'RUNNER_TEMP': str(scratch), 'GITHUB_WORKSPACE': str(workspace),
           'GITHUB_REPOSITORY': 'worldbisect/integration-tests',
           'GITHUB_WORKFLOW_REF': 'worldbisect/integration-tests/.github/workflows/ci.yml@refs/heads/main',
           'GITHUB_JOB': name, 'GITHUB_EVENT_NAME': 'push', 'GITHUB_RUN_ID': '100', 'GITHUB_SHA': 'good',
           'GITHUB_OUTPUT': str(scratch / 'outputs'), 'INPUT_COMMAND': json.dumps(command),
           'INPUT_FILES': '\n'.join(files), 'INPUT_TIMEOUT': '30', 'INPUT_DIAGNOSTIC_TIMEOUT': '180',
           'INPUT_BASELINE_KEY': name, 'WORLDBISECT_BINARY': str(binary),
           'BASELINE_LOOKUP': 'success', 'BASELINE_DOWNLOAD': 'success',
           'BASELINE_RUN_ID': '100', 'BASELINE_SHA': 'good'}
    start = time.monotonic()
    with patch.dict(os.environ, env):
        def prepare():
            Path(env['GITHUB_OUTPUT']).write_text('')
            b.prepare()
            values = dict(line.split('=', 1) for line in Path(env['GITHUB_OUTPUT']).read_text().splitlines())
            return Path(values['root'])
        good = prepare()
        if b.load(good / 'state.json')['status'] != 'PASSED':
            raise RuntimeError(name + ': reference check did not pass')
        stdout = b.load(good / 'initial.json')['result'].get('stdout', '')
        counts = re.findall(r'(\d+) passed', stdout)
        initial_tests = int(counts[-1]) if counts else None
        write(workspace, changed_file, bad_content)
        os.environ.update(GITHUB_RUN_ID='101', GITHUB_SHA='bad')
        bad = prepare()
        if b.load(bad / 'state.json')['status'] != 'BASELINE_MISSING':
            raise RuntimeError(name + ': controlled regression did not fail')
        (bad / 'download').mkdir()
        shutil.copyfile(good / 'inputs.json', bad / 'download/baseline.json')
        b.diagnose(bad)
        b.finish(bad)
        state = b.load(bad / 'state.json')
        report = b.load(bad / 'artifacts/report.json')
        keys = [item['key'] for item in report['cause']]
        if state['status'] != 'PROVEN' or state['command_exit'] != 1 or keys != [changed_file]:
            raise RuntimeError(name + ': wrong proof, factor or preserved exit outcome')
        result = {'case': name, 'status': state['status'], 'factor': changed_file,
                  'experiments': report['evidence']['experiment_count'],
                  'elapsed_seconds': round(time.monotonic() - start, 2),
                  'initial_upstream_tests_passed': initial_tests,
                  'scope': 'controlled regression; not a customer incident'}
        print(json.dumps(result), flush=True)
        return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--upstream', type=Path, required=True)
    parser.add_argument('--python', type=Path, required=True)
    parser.add_argument('--report', type=Path, required=True)
    args = parser.parse_args()
    commit = subprocess.check_output(['git', '-C', str(args.upstream), 'rev-parse', 'HEAD'], text=True).strip()
    if commit != UPSTREAM_SHA:
        raise ValueError('Unexpected upstream revision')
    with tempfile.TemporaryDirectory(prefix='worldbisect-workloads-') as temp:
        scratch = Path(temp)
        results = []
        node = scratch / 'node'
        node.mkdir()
        write(node, 'package.json', '{"type":"module"}\n')
        write(node, 'totals.js', 'export const total = items => items.reduce((sum, item) => sum + item.price * item.quantity, 0);\n')
        write(node, 'totals.test.js', "import test from 'node:test';\nimport assert from 'node:assert/strict';\nimport {total} from './totals.js';\ntest('empty cart', () => assert.equal(total([]), 0));\ntest('cart total in cents', () => assert.equal(total([{price: 299, quantity: 3}]), 897));\n")
        results.append(run_case('node-module-config', node, [shutil.which('node'), '--test', 'totals.test.js'],
                                'package.json', '{"type":"commonjs"}\n', args.binary, scratch))

        compiler = scratch / 'compiler'
        compiler.mkdir()
        write(compiler, 'compiler.flags', '-std=c11\n-pedantic-errors\n')
        write(compiler, 'main.c', 'int main(void) { int total = 0; for (int i = 0; i < 4; i++) total += i; return total == 6 ? 0 : 1; }\n')
        write(compiler, 'check.sh', '#!/usr/bin/env bash\nset -euo pipefail\nwork=$(mktemp -d)\ntrap \'rm -rf "$work"\' EXIT\nmapfile -t flags < compiler.flags\ngcc "${flags[@]}" main.c -o "$work/app"\n"$work/app"\n', True)
        results.append(run_case('c-language-standard', compiler, ['./check.sh'], 'compiler.flags',
                                '-std=c89\n-pedantic-errors\n', args.binary, scratch))

        upstream = scratch / 'itsdangerous'
        upstream.mkdir()
        for directory in ('src', 'tests'):
            shutil.copytree(args.upstream / directory, upstream / directory,
                            ignore=shutil.ignore_patterns('__pycache__', '*.pyc'))
        shutil.copyfile(args.upstream / 'LICENSE.txt', upstream / 'LICENSE.txt')
        write(upstream, 'pytest.ini', '[pytest]\npythonpath = src\ntestpaths = tests\n')
        results.append(run_case('itsdangerous-pytest-config', upstream,
                                [str(args.python), '-B', '-m', 'pytest', '-p', 'no:cacheprovider', '-q'],
                                'pytest.ini', '[pytest]\npythonpath = missing-source\ntestpaths = tests\n', args.binary, scratch))
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps({'upstream': {'repository': 'pallets/itsdangerous', 'commit': commit},
                                          'results': results}, indent=2) + '\n')


if __name__ == '__main__':
    main()
