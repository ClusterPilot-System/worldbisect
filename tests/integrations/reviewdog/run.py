#!/usr/bin/env python3
"""Run a bounded WorldBisect -> SARIF -> reviewdog example without network or tokens."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile

from prepare_sarif import prepare


def run(command, cwd, env, expected=0, stdin=None):
    result = subprocess.run([str(item) for item in command], cwd=cwd, env=env,
                            input=stdin, text=True, capture_output=True, timeout=90)
    if result.returncode != expected:
        raise RuntimeError(f'{command[0]}: expected exit {expected}, got {result.returncode}\n{result.stderr}')
    return result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--worldbisect', type=Path, required=True)
    parser.add_argument('--reviewdog', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True, help='new directory for sanitized evidence')
    args = parser.parse_args()
    wb, rd = args.worldbisect.resolve(strict=True), args.reviewdog.resolve(strict=True)
    args.output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix='worldbisect-reviewdog-') as temp:
        root = Path(temp)
        env = {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'HOME': str(root / 'home')}
        (root / 'home').mkdir()
        version = run([rd, '-version'], root, env).strip()
        if version != '0.21.2':
            raise RuntimeError('this integration contract is pinned to reviewdog 0.21.2')
        for label in ('good', 'bad'):
            workspace = root / label
            workspace.mkdir()
            (workspace / 'check.sh').write_text('#!/bin/sh\nset -eu\ngrep -qx mode=good config.txt\n')
            (workspace / 'check.sh').chmod(0o755)
            (workspace / 'config.txt').write_text('# WorldBisect reviewdog fixture\nmode=' + label + '\n')
            run([wb, 'capture', '--trace', 'off', '--timeout', '5s', '--max-output-bytes', '65536',
                 '--store', root / 'store', '--workspace', workspace, '--oracle', 'exit=0',
                 '--output', root / (label + '.wcap'), '--', './check.sh'], root, env,
                expected=0 if label == 'good' else 1)
        report = json.loads(run([wb, 'compare', '--store', root / 'store', '--good', root / 'good.wcap',
                                '--bad', root / 'bad.wcap', '--timeout', '5s', '--max-experiments', '16',
                                '--max-output-bytes', '65536', '--format', 'json', '--', './check.sh'], root, env))
        assert report['status'] == 'PROVEN', report
        assert [cause['key'] for cause in report['cause']] == ['config.txt'], report
        assert report['evidence']['experiment_count'] <= 16, report
        sarif = json.loads(run([wb, 'explain', '--store', root / 'store', '--format', 'sarif',
                               report['analysis_id']], root, env))
        bad = root / 'bad'
        # A diff is supplied explicitly, so no ambient Git repository or identity is needed.
        diff = root / 'change.diff'
        diff.write_text('diff --git a/config.txt b/config.txt\n--- a/config.txt\n+++ b/config.txt\n'
                        '@@ -2 +2 @@\n-mode=good\n+mode=bad\n')
        diff_arg = '-diff=cat ' + str(diff)
        common = [rd, '-f=sarif', '-reporter=rdjson']
        raw = json.loads(run(common + ['-filter-mode=nofilter'], bad, env, stdin=json.dumps(sarif)))
        raw_path = raw['diagnostics'][0]['location']['path']
        assert raw_path != 'config.txt', raw
        raw_filtered = json.loads(run(common + ['-filter-mode=file', diff_arg], bad, env, stdin=json.dumps(sarif)))
        assert not raw_filtered.get('diagnostics'), raw_filtered
        adapted = prepare(report, sarif, bad)
        adapted_text = json.dumps(adapted)
        added_filtered = json.loads(run(common + [diff_arg], bad, env, stdin=adapted_text))
        assert not added_filtered.get('diagnostics'), added_filtered
        rendered = json.loads(run(common + ['-filter-mode=file', diff_arg], bad, env, stdin=adapted_text))
        assert len(rendered['diagnostics']) == 1, rendered
        diagnostic = rendered['diagnostics'][0]
        assert diagnostic['location']['path'] == 'config.txt', diagnostic
        assert diagnostic['severity'] == 'ERROR', diagnostic
        assert diagnostic['code']['value'] == 'worldbisect/PROVEN', diagnostic
        assert 'not a proven faulty line' in diagnostic['message'], diagnostic
        assert all(text in diagnostic['message'] for text in report['boundaries']), diagnostic
        assert adapted['runs'][0]['results'][0]['properties']['forward_verified'] is True
        run(common + ['-filter-mode=file', diff_arg, '-fail-level=error'], bad, env,
            expected=1, stdin=adapted_text)
        limited = json.loads(run([wb, 'compare', '--store', root / 'store', '--good', root / 'good.wcap',
                                  '--bad', root / 'bad.wcap', '--timeout', '5s', '--max-experiments', '6',
                                  '--max-output-bytes', '65536', '--format', 'json', '--', './check.sh'], root, env))
        assert limited['status'] == 'UNPROVEN', limited
        assert limited['evidence']['experiment_count'] <= 6, limited
        limited_sarif = json.loads(run([wb, 'explain', '--store', root / 'store', '--format', 'sarif',
                                       limited['analysis_id']], root, env))
        try:
            prepare(limited, limited_sarif, bad)
        except ValueError:
            pass
        else:
            raise AssertionError('a real budget-limited analysis was promoted to an annotation')
        rejected = []
        for name in ('UNPROVEN', 'CORRELATED', 'SUPPORTED', 'multiple-causes', 'path-escape',
                     'symlink', 'missing-file', 'mismatched-analysis', 'missing-proof', 'missing-location'):
            candidate, candidate_sarif = copy.deepcopy(report), copy.deepcopy(sarif)
            if name in ('UNPROVEN', 'CORRELATED', 'SUPPORTED'):
                candidate['status'] = name
            elif name == 'multiple-causes':
                candidate['cause'].append(copy.deepcopy(candidate['cause'][0]))
            elif name == 'path-escape':
                candidate['cause'][0]['key'] = '../good/config.txt'
            elif name == 'symlink':
                (bad / 'alias.txt').symlink_to('config.txt')
                candidate['cause'][0]['key'] = 'alias.txt'
            elif name == 'missing-file':
                candidate['cause'][0]['key'] = 'removed.txt'
            elif name == 'mismatched-analysis':
                candidate['analysis_id'] = 'ana_different'
            elif name == 'missing-proof':
                candidate['proof']['reverse_verified'] = False
            elif name == 'missing-location':
                candidate_sarif['runs'][0]['results'][0]['locations'] = []
            try:
                prepare(candidate, candidate_sarif, bad)
            except ValueError:
                rejected.append(name)
            else:
                raise AssertionError('unsupported case was annotated: ' + name)
        # Exercise the user-facing adapter CLI and its explicit non-success path.
        for name, value in [('analysis.json', report), ('analysis.sarif', sarif)]:
            (root / name).write_text(json.dumps(value))
        adapter = Path(__file__).with_name('prepare_sarif.py').resolve()
        adapter_cmd = [sys.executable, adapter, '--analysis', root / 'analysis.json',
                       '--sarif', root / 'analysis.sarif', '--workspace', bad]
        assert json.loads(run(adapter_cmd, root, env)) == adapted
        unproven = copy.deepcopy(report)
        unproven['status'] = 'UNPROVEN'
        (root / 'analysis.json').write_text(json.dumps(unproven))
        assert run(adapter_cmd, root, env, expected=2) == ''
        evidence = {
            'scope': 'controlled Linux fixture; portable capture; no network, token or remote PR publication',
            'worldbisect_binary_sha256': hashlib.sha256(wb.read_bytes()).hexdigest(),
            'reviewdog_version': version,
            'status': report['status'], 'factor': 'config.txt',
            'experiments': report['evidence']['experiment_count'],
            'raw_custom_uri_maps_to_workspace_file': False,
            'raw_file_filter_diagnostic_count': 0,
            'adapted_default_added_filter_diagnostic_count': 0,
            'adapted_file_filter_diagnostic_count': 1,
            'error_fail_level_exit': 1, 'remote_github_reporter_tested': False,
            'budget_limited_status': limited['status'],
            'budget_limited_experiments': limited['evidence']['experiment_count'],
            'refused_annotation_cases': rejected,
        }
        for name, value in [('analysis.json', report), ('analysis.sarif', sarif),
                            ('reviewdog.sarif', adapted), ('reviewdog.json', rendered),
                            ('unproven.json', limited), ('evidence.json', evidence)]:
            (args.output / name).write_text(json.dumps(value, indent=2) + '\n')
        print(json.dumps(evidence, indent=2))


if __name__ == '__main__':
    main()
