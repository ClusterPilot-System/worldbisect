#!/usr/bin/env python3
"""Example bridge for one PROVEN regular-file cause; see docs/integrations/reviewdog.md."""
import argparse
import copy
import json
from pathlib import Path, PurePosixPath
import re
import sys
from urllib.parse import quote


MAX_BYTES = 8 * 1024 * 1024
PROOF_KEYS = ('forward_verified', 'reverse_verified', 'minimal_in_model')


def read_json(path):
    with path.open('rb') as stream:
        data = stream.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise ValueError('input exceeds the 8 MiB example limit')
    value = json.loads(data)
    if not isinstance(value, dict):
        raise ValueError('expected a JSON object')
    return value


def prepare(analysis, sarif, workspace):
    if analysis.get('format') != 'worldbisect.analysis-report.v1' or analysis.get('schema_version') != 1:
        raise ValueError('expected a WorldBisect analysis report v1')
    if analysis.get('status') != 'PROVEN' or any(analysis.get('proof', {}).get(k) is not True for k in PROOF_KEYS):
        raise ValueError('no inline annotation: a complete PROVEN result is required; retain the full diagnosis')
    causes = analysis.get('cause', [])
    if len(causes) != 1 or causes[0].get('type') != 'workspace':
        raise ValueError('no inline annotation: this example supports exactly one workspace-file cause')
    key = causes[0].get('key', '')
    if not isinstance(key, str) or not key or any(ord(c) < 32 for c in key):
        raise ValueError('cause path is invalid')
    parts = key.split('/')
    if PurePosixPath(key).is_absolute() or any(p in ('', '.', '..') for p in parts) or '\\' in key or ':' in key:
        raise ValueError('cause path must be a normalized relative POSIX path')
    root = workspace.resolve(strict=True)
    path = root
    for part in parts:
        path = path / part
        if path.is_symlink():
            raise ValueError('symlink causes or parent directories are not supported by this example')
    if not path.is_file() or not path.resolve().is_relative_to(root):
        raise ValueError('cause must be an existing regular file inside the selected workspace')
    analysis_id = analysis.get('analysis_id', '')
    if not isinstance(analysis_id, str) or not re.fullmatch(r'ana_[a-zA-Z0-9_-]+', analysis_id):
        raise ValueError('analysis ID is invalid')
    output = copy.deepcopy(sarif)
    runs = output.get('runs', [])
    if output.get('version') != '2.1.0' or len(runs) != 1 or len(runs[0].get('results', [])) != 1:
        raise ValueError('expected one WorldBisect SARIF 2.1.0 result')
    result = runs[0]['results'][0]
    props = result.get('properties', {})
    if result.get('ruleId') != 'worldbisect/PROVEN' or props.get('status') != 'PROVEN' or props.get('analysis_id') != analysis_id:
        raise ValueError('SARIF and analysis report do not describe the same PROVEN result')
    if any(props.get(k) is not True for k in PROOF_KEYS):
        raise ValueError('SARIF proof fields are incomplete')
    expected_uri = 'worldbisect://analysis/' + analysis_id
    locations = result.get('locations', [])
    if len(locations) != 1 or locations[0].get('physicalLocation', {}).get('artifactLocation', {}).get('uri') != expected_uri:
        raise ValueError('expected the original WorldBisect analysis location')
    original_message = result.get('message', {}).get('text')
    if not isinstance(original_message, str) or original_message != analysis.get('explanation'):
        raise ValueError('SARIF and analysis explanations do not match')
    boundaries = analysis.get('boundaries', []) + analysis.get('limitations', [])
    if not all(isinstance(item, str) for item in boundaries):
        raise ValueError('analysis boundaries must be strings')
    result['locations'] = [{'physicalLocation': {
        'artifactLocation': {'uri': quote(key, safe='/')},
        'region': {'startLine': 1},
    }}]
    result['message'] = {'text': (
        'WorldBisect PROVEN: ' + key + '\n' + original_message + '\n\n'
        'File-level diagnosis. Line 1 is a display anchor, not a proven faulty line. '
        'Confidence is bounded to the selected inputs and tested model.\n'
        'Analysis: ' + analysis_id + '\n'
        + '\n'.join('Boundary: ' + item for item in boundaries)
    )}
    props['original_analysis_uri'] = expected_uri
    props['annotation_scope'] = 'file; line 1 is a display anchor only'
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--analysis', type=Path, required=True)
    parser.add_argument('--sarif', type=Path, required=True)
    parser.add_argument('--workspace', type=Path, required=True)
    args = parser.parse_args()
    try:
        output = prepare(read_json(args.analysis), read_json(args.sarif), args.workspace)
    except (ValueError, OSError, KeyError, TypeError, AttributeError) as error:
        print('reviewdog example: ' + str(error), file=sys.stderr)
        return 2
    print(json.dumps(output, indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
