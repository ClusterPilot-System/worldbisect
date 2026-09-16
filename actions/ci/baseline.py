#!/usr/bin/env python3
"""Bounded CI input snapshots and orchestration; no new causal proof semantics."""
import base64
import hashlib
import html
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile

MAX_FILES = 256
MAX_BYTES = 16 * 1024 * 1024
MAX_JSON = 24 * 1024 * 1024
SCHEMA = 'worldbisect.ci-inputs.v1'
MESSAGES = {
    'PASSED': 'The command passed. A trusted default-branch push can retain these selected inputs as a baseline.',
    'BASELINE_MISSING': 'No recent compatible baseline was available. Run this workflow successfully on the default branch first. Check actions: read, retention, file selection and baseline-key.',
    'BASELINE_UNAVAILABLE': 'Baseline lookup or download failed. Check Actions permissions and artifact availability; the original command still failed.',
    'BASELINE_INVALID': 'The downloaded baseline failed integrity, provenance or compatibility validation. Rebuild it with a successful default-branch run.',
    'BASELINE_NOT_REPRODUCIBLE': 'The previous successful inputs no longer pass on this runner. Check installed dependencies, external services and command stability. No cause was proven.',
    'FAILURE_NOT_REPRODUCIBLE': 'The failing inputs passed when repeated. Investigate flaky tests, timing and external dependencies. No cause was proven.',
    'COMMAND_TIMEOUT': 'The command exceeded its timeout. Check the command and runner resources before retrying; no cause was proven.',
    'COMMAND_ERROR': 'The command could not run normally. Check executable permissions, the selected files and runner setup.',
    'DIAGNOSTIC_ERROR': 'The diagnostic process failed or exceeded its budget. The original failure is preserved; check runner setup and the diagnostic timeout.',
}


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def safe_path(value):
    if not isinstance(value, str) or not value or len(value) > 512:
        raise ValueError('invalid selected path')
    path = PurePosixPath(value)
    if not path.parts or str(path) != value or path.is_absolute() or any(p in ('', '.', '..') for p in path.parts):
        raise ValueError('selected paths must be canonical relative file paths')
    if re.search(r'[\x00-\x1f\x7f\\*?\[\]]', value):
        raise ValueError('globs, backslashes and control characters are not supported')
    for part in path.parts:
        name = part.lower()
        if (name in ('.git', '.ssh', '.aws', '.kube', '.npmrc', '.netrc', 'credentials', 'kubeconfig')
                or name == '.env' or name.startswith('.env.')
                or name.startswith(('id_rsa', 'id_ed25519'))
                or name.endswith(('.pem', '.key', '.token', '.p12', '.pfx'))):
            raise ValueError('credential-like paths cannot be selected')
    return path


def reject_credentials(data):
    # Conservative recognizable formats, not a claim to detect every secret.
    patterns = [rb'-----BEGIN [A-Z ]*PRIVATE KEY-----',
                rb'(?<![A-Za-z0-9])(?:AKIA|ASIA)[A-Z0-9]{16}(?![A-Za-z0-9])',
                rb'(?<![A-Za-z0-9])gh[pousr]_[A-Za-z0-9]{36,}',
                rb'github_pat_[A-Za-z0-9_]{60,}',
                rb'xox[baprs]-[A-Za-z0-9-]{20,}']
    if any(re.search(pattern, data) for pattern in patterns):
        raise ValueError('recognizable credential material cannot be retained')


def read_selected(root, name):
    """Open every component relative to directory descriptors, without following links."""
    parts = safe_path(name).parts
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        for part in parts[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
            os.close(directory)
            directory = child
        fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        with os.fdopen(fd, 'rb') as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1 or before.st_size > MAX_BYTES:
                raise ValueError('selected inputs must be bounded regular files without hard links')
            data = stream.read(MAX_BYTES + 1)
            after = os.fstat(stream.fileno())
            if (len(data) > MAX_BYTES or (before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns)
                    != (after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)):
                raise ValueError('selected file changed while being read')
            reject_credentials(data)
            return {'path': name, 'mode': stat.S_IMODE(before.st_mode) & 0o777,
                    'data': base64.b64encode(data).decode(), 'sha256': hashlib.sha256(data).hexdigest()}
    except FileNotFoundError:
        return {'path': name, 'absent': True}
    finally:
        os.close(directory)


def snapshot(root, contract, source):
    entries, total = [], 0
    for name in contract['files']:
        entry = read_selected(root, name)
        total += len(base64.b64decode(entry.get('data', '')))
        if total > MAX_BYTES:
            raise ValueError('selected inputs exceed 16 MiB')
        entries.append(entry)
    return {'schema': SCHEMA, 'contract': contract, 'source': source, 'entries': entries}


def validate(document, contract, source=None):
    if (document.get('schema') != SCHEMA or document.get('contract') != contract
            or (source is not None and document.get('source') != source)):
        raise ValueError('baseline compatibility or provenance mismatch')
    entries = document.get('entries')
    if not isinstance(entries, list) or len(entries) != len(contract['files']) or len(entries) > MAX_FILES:
        raise ValueError('invalid file count')
    names, total, decoded = [], 0, []
    for item in entries:
        name = item['path']
        safe_path(name)
        names.append(name)
        if item.get('absent') is True and set(item) == {'path', 'absent'}:
            continue
        if set(item) != {'path', 'mode', 'data', 'sha256'}:
            raise ValueError('invalid file record')
        mode = item['mode']
        if type(mode) is not int or mode < 0 or mode > 0o777:
            raise ValueError('invalid mode')
        data = base64.b64decode(item['data'], validate=True)
        total += len(data)
        if total > MAX_BYTES or hashlib.sha256(data).hexdigest() != item['sha256']:
            raise ValueError('invalid content digest or size')
        reject_credentials(data)
        decoded.append((name, mode, data))
    if names != contract['files'] or len(set(names)) != len(names):
        raise ValueError('file selection mismatch')
    # Prevent a file from being an ancestor of another selected path.
    for name in names:
        if any(str(p) in names for p in PurePosixPath(name).parents if str(p) != '.'):
            raise ValueError('overlapping file paths')
    return decoded


def materialize(document, contract, destination, source=None):
    entries = validate(document, contract, source)
    destination.mkdir(mode=0o700)  # fresh directory, never merge with existing files
    for name, mode, data in entries:
        path = destination / name
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open('xb') as stream:
            stream.write(data)
        path.chmod(mode)


def load(path):
    if path.stat().st_size > MAX_JSON or path.is_symlink():
        raise ValueError('invalid snapshot file')
    return json.loads(path.read_text())


def save(path, document):
    path.write_text(json.dumps(document, sort_keys=True) + '\n')
    path.chmod(0o600)


def output(**values):
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'], 'a') as stream:
            for key, value in values.items():
                stream.write(f'{key.replace("_", "-")}={value}\n')


def positive(name, default, maximum):
    value = int(os.environ.get(name, str(default)))
    if not 1 <= value <= maximum:
        raise ValueError('numeric input out of bounds')
    return value


def invoke(args, target, seconds, home):
    # No inherited GitHub tokens, secret environment variables or command output in logs.
    env = {'PATH': os.environ.get('PATH', '/usr/bin:/bin'), 'LANG': 'C.UTF-8', 'HOME': str(home)}
    with target.open('wb') as stream:
        process = subprocess.Popen(args, stdout=stream, stderr=subprocess.DEVNULL, env=env, start_new_session=True)
        try:
            return process.wait(timeout=seconds)
        except subprocess.TimeoutExpired:
            process.send_signal(signal.SIGINT)  # CLI cancels its runner process groups.
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            raise


def capture(root, state, workspace, label):
    path = root / f'{label}.json'
    invoke([state['binary'], 'capture', '--store', str(root / 'store'), '--workspace', str(workspace),
            '--format', 'json', '--timeout', f"{state['timeout']}s",
            '--max-workspace-files', '1024', '--max-workspace-bytes', str(MAX_BYTES),
            '--max-output-bytes', '65536', '--', *state['contract']['command']],
           path, state['timeout'] + 20, root / 'home')
    record = load(path)
    if not record.get('id') or 'oracle_result' not in record:
        raise ValueError('capture did not produce a valid result')
    return record


def command_status(record):
    result = record['result']
    if result.get('timed_out'):
        return 'COMMAND_TIMEOUT'
    if result.get('exit_code', -1) < 0:
        return 'COMMAND_ERROR'
    return 'PASSED' if record['oracle_result']['passed'] else 'FAILED'


def prepare():
    if sys.platform != 'linux' or os.environ.get('GITHUB_EVENT_NAME') == 'pull_request_target':
        raise ValueError('use a Linux push or pull_request job, never pull_request_target')
    files = sorted(set(line.strip() for line in os.environ['INPUT_FILES'].splitlines() if line.strip()))
    if not files or len(files) > MAX_FILES:
        raise ValueError('select between 1 and 256 regular files')
    for name in files:
        safe_path(name)
    command = json.loads(os.environ['INPUT_COMMAND'])
    if (not isinstance(command, list) or not command or len(command) > 128
            or any(not isinstance(arg, str) or '\0' in arg or len(arg) > 4096 for arg in command)
            or not command[0]):
        raise ValueError('command must be a JSON array of executable and arguments')
    key = os.environ.get('INPUT_BASELINE_KEY', 'default')
    if not re.fullmatch(r'[A-Za-z0-9_-]{1,64}', key):
        raise ValueError('baseline-key must contain 1-64 letters, numbers, underscores or hyphens')
    timeout = positive('INPUT_TIMEOUT', 60, 600)
    retention = positive('INPUT_RETENTION', 7, 30)
    contract = {'schema': SCHEMA, 'repository': os.environ['GITHUB_REPOSITORY'],
                'workflow': os.environ['GITHUB_WORKFLOW_REF'].split('@')[0],
                'job': os.environ['GITHUB_JOB'], 'key': key,
                'platform': sys.platform, 'architecture': os.uname().machine,
                'runner_image': os.environ.get('ImageOS', 'unspecified'),
                'command': command, 'files': files, 'timeout': timeout}
    root = Path(tempfile.mkdtemp(prefix='worldbisect-ci-', dir=os.environ['RUNNER_TEMP']))
    (root / 'home').mkdir()
    (root / 'artifacts').mkdir()
    source = {'run_id': os.environ['GITHUB_RUN_ID'], 'sha': os.environ['GITHUB_SHA']}
    document = snapshot(Path(os.environ['GITHUB_WORKSPACE']).resolve(), contract, source)
    save(root / 'inputs.json', document)
    state = {'contract': contract, 'timeout': timeout, 'retention': retention,
             'binary': os.environ['WORLDBISECT_BINARY'], 'source': source,
             'diagnostic_timeout': positive('INPUT_DIAGNOSTIC_TIMEOUT', 180, 900),
             'status': 'DIAGNOSTIC_ERROR', 'command_exit': 1}
    save(root / 'state.json', state)
    output(root=root, artifact_name='worldbisect-baseline-' + digest(contract)[:32], retention=retention)
    materialize(document, contract, root / 'current')
    record = capture(root, state, root / 'current', 'initial')
    state['status'] = command_status(record)
    state['command_exit'] = 0 if state['status'] == 'PASSED' else 1
    if state['status'] == 'FAILED':
        state['status'] = 'BASELINE_MISSING'
    save(root / 'state.json', state)
    output(command_passed=str(state['command_exit'] == 0).lower(), status=state['status'])


def diagnose(root):
    state = load(root / 'state.json')
    if state['status'] != 'BASELINE_MISSING':
        return
    if os.environ.get('BASELINE_LOOKUP') != 'success' or os.environ.get('BASELINE_DOWNLOAD') == 'failure':
        state['status'] = 'BASELINE_UNAVAILABLE'
    elif os.environ.get('BASELINE_RUN_ID'):
        try:
            document = load(root / 'download' / 'baseline.json')
            source = {'run_id': os.environ['BASELINE_RUN_ID'], 'sha': os.environ['BASELINE_SHA']}
            materialize(document, state['contract'], root / 'good', source)
            state['baseline_source'] = source
        except (OSError, ValueError, KeyError, TypeError):
            state['status'] = 'BASELINE_INVALID'
        else:
            state['status'] = 'DIAGNOSTIC_ERROR'
            save(root / 'state.json', state)
            good = capture(root, state, root / 'good', 'good')
            if command_status(good) != 'PASSED':
                state['status'] = 'BASELINE_NOT_REPRODUCIBLE'
            else:
                materialize(load(root / 'inputs.json'), state['contract'], root / 'bad')
                bad = capture(root, state, root / 'bad', 'bad')
                bad_status = command_status(bad)
                if bad_status == 'PASSED':
                    state['status'] = 'FAILURE_NOT_REPRODUCIBLE'
                elif bad_status != 'FAILED':
                    state['status'] = bad_status
                else:
                    report = root / 'artifacts' / 'report.json'
                    code = invoke([state['binary'], 'compare', '--store', str(root / 'store'),
                                   '--good', good['id'], '--bad', bad['id'], '--format', 'json',
                                   '--repetitions', '3', '--max-experiments', '64', '--max-factors', '256',
                                   '--max-output-bytes', '65536',
                                   '--certificate', str(root / 'artifacts' / 'result.wbc'),
                                   '--', *state['contract']['command']],
                                  report, state['diagnostic_timeout'], root / 'home')
                    if code != 0:
                        raise ValueError('compare failed')
                    result = load(report)
                    if result['status'] not in ('PROVEN', 'SUPPORTED', 'CORRELATED', 'UNPROVEN'):
                        raise ValueError('invalid proof status')
                    state['status'] = result['status']
                    state['analysis_id'] = result['analysis_id']
                    for fmt, filename in [('markdown', 'report.md'), ('junit', 'report.junit.xml'), ('sarif', 'report.sarif')]:
                        if invoke([state['binary'], 'explain', '--store', str(root / 'store'), '--format', fmt,
                                   result['analysis_id']], root / 'artifacts' / filename, 30, root / 'home') != 0:
                            raise ValueError('report generation failed')
    save(root / 'state.json', state)


def safe_text(value, limit=400):
    text = html.escape(str(value)[:limit], quote=False).replace('@', '@\u200b')
    return re.sub(r'([\\`*_{}\[\]()+#!|])', r'\\\1', text).replace('\n', ' ')


def summary_lines(state, result=None):
    status = state['status']
    lines = ['# WorldBisect CI diagnosis', '', f'**Status:** `{status}`', '']
    if result is None:
        lines += [MESSAGES.get(status, 'The diagnosis did not produce a complete report.'), '',
                  '**Finding:** No causal claim.',
                  '**Tested:** The selected command was attempted in staged inputs.',
                  '**Confidence:** A cause has not been established.',
                  '**Next step:** Follow the status guidance above.']
    else:
        causes = result.get('cause', [])
        finding = '; '.join(safe_text(item.get('description', item.get('key', 'unknown'))) for item in causes[:3])
        if len(causes) > 3:
            finding += f'; and {len(causes) - 3} more (see full report)'
        proof = result.get('proof', {})
        forward = 'passed' if proof.get('forward_verified') else 'not established'
        reverse = 'passed' if proof.get('reverse_verified') else 'not established'
        confidence = {
            'PROVEN': 'Confirmed within the selected inputs and tested model.',
            'SUPPORTED': 'Supported by experiments; full proof checks did not pass.',
            'CORRELATED': 'Associated difference; causal intervention did not establish proof.',
            'UNPROVEN': 'Insufficient controlled evidence; no cause proven.',
        }[status]
        count = result.get('evidence', {}).get('experiment_count', 0)
        steps = result.get('next_steps', [])
        lines += [f'**Finding:** {finding or "No confirmed factor."}',
                  f'**Tested:** {int(count)} experiments; repair: {forward}; reverse reproduction: {reverse}.',
                  f'**Confidence:** {confidence}',
                  '**Next step:** ' + safe_text(steps[0] if steps else 'Review the full diagnostic report.')]
        boundaries = result.get('limitations', [])
        if boundaries:
            lines += ['**Limit:** ' + safe_text(boundaries[0])]
    lines += ['', '**Scope:** Selected files on the current runner. Historical host, network, secrets and dependency state are not restored.']
    if state.get('baseline_source'):
        run = state['baseline_source']['run_id']
        repo = state['contract']['repository']
        if run.isdigit() and re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo):
            lines.extend(['', f'Baseline: [successful workflow run {run}](https://github.com/{repo}/actions/runs/{run})'])
    if result is not None:
        lines += ['', 'The diagnostic artifact contains the complete factors, proof checks, next steps and evidence boundaries.']
    return lines


def finish(root):
    state = load(root / 'state.json')
    result = None
    if state['status'] in ('PROVEN', 'SUPPORTED', 'CORRELATED', 'UNPROVEN'):
        result = load(root / 'artifacts' / 'report.json')
    summary_path = root / 'artifacts' / 'summary.md'
    summary_path.write_text('\n'.join(summary_lines(state, result)) + '\n')
    save(root / 'artifacts' / 'outcome.json', {k: state[k] for k in ('status', 'command_exit')})
    summary = os.environ.get('GITHUB_STEP_SUMMARY')
    if summary:
        with open(summary, 'a') as stream:
            stream.write(summary_path.read_text())
    output(status=state['status'], command_exit=state['command_exit'], analysis_id=state.get('analysis_id', ''),
           summary_path=summary_path, comment_key=digest(state['contract'])[:32])


if __name__ == '__main__':
    os.umask(0o077)
    try:
        phase = sys.argv[1]
        if phase == 'prepare':
            prepare()
        elif phase == 'diagnose':
            diagnose(Path(os.environ['CI_ROOT']))
        elif phase == 'finish':
            finish(Path(os.environ['CI_ROOT']))
        elif phase == 'cleanup':
            # Root is a generated path from prepare, not a user-selected directory.
            root = Path(os.environ['CI_ROOT'])
            if root.parent == Path(os.environ['RUNNER_TEMP']) and root.name.startswith('worldbisect-ci-'):
                shutil.rmtree(root)
        else:
            raise ValueError('unknown phase')
    except Exception as error:
        # Never echo arbitrary file contents, command arguments or credentials.
        print(f'WorldBisect CI {sys.argv[1]} failed ({type(error).__name__}); see the setup guide.', file=sys.stderr)
        sys.exit(1)
