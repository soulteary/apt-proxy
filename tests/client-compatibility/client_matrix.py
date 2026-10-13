#!/usr/bin/env python3
"""Plan trusted Docker clients, verify cache evidence, and gate releases."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
FAMILIES = ('apt', 'dnf', 'apk', 'pacman', 'zypper')


def clients(family='all'):
    if family not in ('all', *FAMILIES):
        raise ValueError('unknown client family: ' + family)
    rows = json.loads((ROOT / 'tests/client-compatibility/clients.json').read_text())
    ids, images = set(), set()
    for row in rows:
        if set(row) != {'id', 'family', 'image'}:
            raise ValueError('invalid client manifest fields')
        if not re.fullmatch(r'[a-z0-9][a-z0-9-]{0,63}', row['id']):
            raise ValueError('invalid client identifier')
        if row['family'] not in FAMILIES or not re.fullmatch(r'[a-z0-9][a-z0-9./:_-]+', row['image']):
            raise ValueError('invalid client image or family')
        if row['id'] in ids or row['image'] in images:
            raise ValueError('duplicate client identifier or image')
        ids.add(row['id'])
        images.add(row['image'])
    selected = [row for row in rows if family == 'all' or row['family'] == family]
    if not selected:
        raise ValueError('empty client matrix')
    return selected


def source_sha():
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()


def cache_proof(proof):
    first, second = proof.get('first_origin_package_gets'), proof.get('second_origin_package_gets')
    if type(first) is not int or type(second) is not int or first < 1 or second != first:
        raise ValueError('origin package GET counts do not prove a warm proxy cache hit')
    if proof.get('client_cache_cleared') is not True or proof.get('payload_matches') is not True:
        raise ValueError('client cache cleanup or installed payload was not verified')
    return proof


def run_client(client_id, evidence_root):
    row = next((row for row in clients() if row['id'] == client_id), None)
    if row is None:
        raise ValueError('unknown client identifier: ' + client_id)
    # Only remove this client's disposable output. Never clean arbitrary input roots.
    output = evidence_root.resolve() / client_id
    if output.is_symlink():
        raise ValueError('refusing symlink evidence directory')
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)
    report = {**row, 'source_sha': source_sha(), 'run_id': os.environ.get('GITHUB_RUN_ID', 'local'),
              'run_attempt': int(os.environ.get('GITHUB_RUN_ATTEMPT', '1')), 'status': 'failed'}
    result = 1
    try:
        env = dict(os.environ)
        env[row['family'].upper() + '_CLIENT_EVIDENCE_DIR'] = str(output / 'fixture')
        with (output / 'host.log').open('w') as log:
            process = subprocess.Popen(['bash', f"tests/{row['family']}-client/matrix.sh", row['image']],
                                       cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                       text=True, errors='replace')
            for line in process.stdout:
                print(line, end='', flush=True)
                log.write(line)
                log.flush()
            result = process.wait()
        if result:
            raise ValueError(f'client fixture exited with status {result}')
        stamps = list((output / 'fixture').glob('*/result.txt'))
        if len(stamps) != 1 or not stamps[0].read_text().startswith('PASS:'):
            raise ValueError('missing successful end-to-end fixture evidence')
        report['cache_hit'] = cache_proof(json.loads((stamps[0].parent / 'cache-hit.json').read_text()))
        report['status'] = 'passed'
        result = 0
    except (OSError, ValueError, TypeError, AttributeError) as error:
        report['error'] = str(error)
        print(str(error), file=sys.stderr)
        result = result or 1
    finally:
        (output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return result


def collect_reports(rows, report_root, sha, run_id, matrix_result='success'):
    """Choose the latest attempt per client from this run and exact source commit."""
    selected = {}
    invalid = []
    expected = {row['id']: row for row in rows}
    for path in sorted(report_root.glob('**/report.json')):
        try:
            report = json.loads(path.read_text())
            if report.get('run_id') != run_id or report.get('source_sha') != sha:
                continue
            row = expected.get(report.get('id'))
            if row is None:
                continue
            if any(report.get(key) != value for key, value in row.items()):
                raise ValueError('manifest mismatch')
            attempt = report.get('run_attempt')
            if type(attempt) is not int or attempt < 1:
                raise ValueError('invalid attempt')
            previous = selected.get(row['id'])
            if previous is not None and previous['run_attempt'] == attempt:
                raise ValueError('duplicate report for one client attempt')
            if previous is None or previous['run_attempt'] < attempt:
                selected[row['id']] = report
        except (OSError, ValueError, TypeError, AttributeError) as error:
            invalid.append(f'{path.name}: {error}')
    outcomes = []
    for row in rows:
        report = selected.get(row['id'])
        error = 'missing evidence for this source and run'
        if report is not None:
            error = str(report.get('error') or 'client did not pass')
            if report.get('status') == 'passed':
                try:
                    cache_proof(report.get('cache_hit', {}))
                    error = ''
                except (ValueError, TypeError, AttributeError) as failure:
                    error = str(failure)
        outcomes.append({**row, 'status': 'failed' if error else 'passed', 'error': error,
                         'run_attempt': report.get('run_attempt') if report else None})
    return {'source_sha': sha, 'run_id': run_id, 'matrix_result': matrix_result,
            'clients': outcomes, 'invalid_reports': invalid,
            'passed': matrix_result == 'success' and not invalid
            and all(row['status'] == 'passed' for row in outcomes)}


def summarize(args):
    report = collect_reports(clients(args.family), Path(args.reports), args.source_sha,
                             args.run_id, args.matrix_result)
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    (output / 'summary.json').write_text(json.dumps(report, indent=2) + '\n')
    lines = ['# Client repository cache compatibility', '',
             f"Source: `{args.source_sha}`. Workflow run: `{args.run_id}`.",
             f"Matrix job result: **{args.matrix_result}**. Overall gate: **{'PASS' if report['passed'] else 'FAIL'}**.", '',
             'PASS requires signed update/install, client cache cleanup, unchanged origin package GETs, payload verification, and family rejection checks.', '']
    for row in report['clients']:
        detail = f" — {row['error']}" if row['error'] else ''
        lines.append(f"- **{row['status'].upper()}** `{row['image']}` ({row['family']}, attempt {row['run_attempt']}){detail}")
    lines.extend(['', *report['invalid_reports'], '',
                  'Local signed fixtures verify client caching; they do not certify every public mirror, installer, or operating system.'])
    markdown = '\n'.join(lines) + '\n'
    (output / 'summary.md').write_text(markdown)
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as summary:
            summary.write(markdown)
    print(markdown)
    return 0 if report['passed'] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    plan = sub.add_parser('plan')
    plan.add_argument('--family', choices=('all', *FAMILIES), default='all')
    plan.add_argument('--github-output')
    run = sub.add_parser('run')
    run.add_argument('client_id')
    run.add_argument('--evidence-root', default='client-compatibility-evidence')
    summary = sub.add_parser('summary')
    summary.add_argument('--family', choices=('all', *FAMILIES), default='all')
    summary.add_argument('--reports', required=True)
    summary.add_argument('--source-sha', required=True)
    summary.add_argument('--run-id', required=True)
    summary.add_argument('--matrix-result', choices=('success', 'failure', 'cancelled', 'skipped'), default='success')
    summary.add_argument('--output', default='client-compatibility-summary')
    args = parser.parse_args()
    if args.command == 'plan':
        matrix = json.dumps({'include': clients(args.family)}, separators=(',', ':'))
        print(matrix)
        if args.github_output:
            with open(args.github_output, 'a') as output:
                output.write(f'matrix={matrix}\nsource_sha={source_sha()}\nfamily={args.family}\n')
        return 0
    if args.command == 'run':
        return run_client(args.client_id, Path(args.evidence_root))
    return summarize(args)


if __name__ == '__main__':
    sys.exit(main())
