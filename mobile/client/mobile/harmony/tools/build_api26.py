"""Compile the formal Harmony shared client with API26; never sign or publish it."""
from pathlib import Path
import argparse
import datetime
import uuid
import hashlib
import json
import os
import subprocess
import sys
import zipfile
from materials import checked, evidence_output, verify_snapshot, verify_rawfile_resources

ROOT = Path(__file__).resolve().parents[3]
BASE = ROOT / 'mobile/harmony'
TOOLCHAIN = ROOT / 'validation/harmony/tools/toolchain.py'
def verify_hap_rawfiles(archive, resources):
    expected = {'resources/rawfile/' + name: digest
                for name, digest in resources.items()}
    names = [entry.filename for entry in archive.infolist()
             if entry.filename.startswith('resources/rawfile/') and not entry.is_dir()]
    assert len(names) == len(set(names)), 'Duplicate HAP rawfile entry'
    assert set(names) == set(expected), 'HAP rawfile set drift'
    assert {name: hashlib.sha256(archive.read(name)).hexdigest() for name in names} == expected, 'HAP rawfile digest drift'

def verify_raw_dependency():
    lock = json.loads((BASE / 'raw-codec.lock.json').read_text(encoding='utf-8'))
    directory = checked(ROOT / lock['directory'])
    assert directory.resolve().is_relative_to(BASE / '.build')
    assert not any(p.is_symlink() or p.is_junction() for p in directory.rglob('*'))
    actual = {p.relative_to(directory).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
              for p in directory.rglob('*') if p.is_file()}
    assert actual == lock['files'], 'RAW dependency changed or missing during build'
def build(args, OUT, report):
    report['phase'] = 'source_preflight'
    report_path = OUT / 'build-report.json'
    def save():
        report_path.write_text(json.dumps(report, indent=2), encoding='utf-8')
    save()
    metadata = checked(BASE / 'materials/source-build.json')
    snapshot = json.loads(metadata.read_text(encoding='utf-8'))
    report.update(sourceIdentitySha256=snapshot['sourceSha256'], expectedSourceFiles=snapshot['files'])
    save()
    owned = verify_snapshot(snapshot)
    verify_raw_dependency()
    verify_snapshot(snapshot)
    report['phase'] = 'toolchain_preflight'
    save()
    tools_root = (args.toolchain_root or ROOT / 'validation/harmony/.build').absolute()
    clt = tools_root / 'commandline-tools/command-line-tools'
    tool_inputs = ([clt / 'bin/hvigorw', clt / 'bin/ohpm', clt / 'tool/node/bin/node'] if args.entry == 'hvigor' else [
        tools_root / 'deveco/node_modules/@deveco/deveco-cli/dist/cli.js',
        tools_root / 'deveco/node_modules/@deveco/deveco-cli/package.json',
        tools_root / 'deveco/package-lock.json']) + [
        clt / 'sdk/default/openharmony/ets/oh-uni-package.json']
    tool_sha = {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in tool_inputs}
    report.update(readOnlyToolchainSource=str(tools_root), toolchainMetadataSHA256=tool_sha)
    sdk_package = json.loads(tool_inputs[3].read_text(encoding='utf-8'))
    report.update(buildEntry=args.entry, sdkVersion=sdk_package['version'])
    if args.entry == 'cli':
        cli_package = json.loads(tool_inputs[1].read_text(encoding='utf-8'))
        cli_lock = json.loads(tool_inputs[2].read_text(encoding='utf-8'))
        report['officialCLIVersion'] = cli_package['version']
        assert cli_package['version'] == cli_lock['packages']['node_modules/@deveco/deveco-cli']['version']
    save()
    assert str(sdk_package['apiVersion']) == '26' and sdk_package['platformVersion'] == '26.0.0'
    assert sdk_package['version'] == '26.0.0.105'
    shared_tools = ['--toolchain-root', str(args.toolchain_root.absolute())] if args.toolchain_root else []
    report['phase'] = 'official_build'
    save()
    command = ([sys.executable, '-B', str(TOOLCHAIN), 'hvigor', '--clt-path', str(clt)] if args.entry == 'hvigor' else
               [sys.executable, '-B', '-X', 'utf8', str(TOOLCHAIN), 'cli', *shared_tools, '--project', 'production', 'build', '--product', 'default',
                '--modules', 'entry@default', '--build-mode', 'debug'])
    result = subprocess.run(command,
                            cwd=BASE / 'app', env=dict(os.environ, DEVECO_CLI_DEBUG='1'), capture_output=True,
                            text=True, encoding='utf-8', errors='replace')
    log = OUT / 'api26-build.log'
    log.write_text(result.stdout + '\n' + result.stderr, encoding='utf-8')
    report.update(exitCode=result.returncode, officialApi26Compile=result.returncode == 0,
                  buildLog=str(log.relative_to(ROOT)),
                  buildLogSha256=hashlib.sha256(log.read_bytes()).hexdigest(), phase='post_build_integrity')
    save()
    # Save the observed metadata before assertions, including missing files.
    after = {str(path): hashlib.sha256(path.read_bytes()).hexdigest() if path.is_file() else None
             for path in tool_inputs}
    report['toolchainMetadataAfterSHA256'] = after
    report['toolchainMetadataMismatches'] = [name for name, sha in tool_sha.items() if after[name] != sha]
    save()
    report['phase'] = 'post_build_source_integrity'
    save()
    verify_snapshot(snapshot)
    report['phase'] = 'post_build_dependency_integrity'
    save()
    verify_raw_dependency()
    report['phase'] = 'post_build_toolchain_integrity'
    save()
    assert not report['toolchainMetadataMismatches'], 'Shared toolchain metadata changed during build'
    hap = BASE / 'app/entry/build/default/outputs/default/entry-default-unsigned.hap'
    package_path = checked(ROOT / 'mobile/.build/memoh-web/resource-package.json')
    package = json.loads(package_path.read_text(encoding='utf-8'))
    abc = {}
    native_libraries = {}
    report['phase'] = 'hap_validation'
    save()
    if result.returncode == 0:
        with zipfile.ZipFile(hap) as z:
            verify_hap_rawfiles(z, verify_rawfile_resources(package))
            abc = {name: {'bytes': len(z.read(name)), 'sha256': hashlib.sha256(z.read(name)).hexdigest()}
                   for name in z.namelist() if name.endswith('.abc')}
            assert abc, 'HAP must contain the compiled ArkTS ABC'
            for architecture, machine in [('arm64-v8a', 183), ('x86_64', 62)]:
                name = 'libs/' + architecture + '/libbegoniaraw.so'
                data = z.read(name)
                assert data[:6] == b'\x7fELF\x02\x01' and int.from_bytes(data[18:20], 'little') == machine
                native_libraries[name] = {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest(), 'elfMachine': machine}
            license_path = BASE / 'app/entry/src/main/resources/rawfile/raw-codec-license.txt'
            assert z.read('resources/rawfile/raw-codec-license.txt') == license_path.read_bytes()
            module = json.loads(z.read('module.json'))
            assert module['app']['bundleName'] == 'cloud.begonia.workassistant'
            assert not module['module'].get('skillProfiles')
            assert json.loads(z.read('resources/rawfile/source-build.json')) == snapshot
            assert not any(n.endswith('/contract.json') or n.endswith('/coverage-matrix.json') or '/demo-result.' in n or '/skills/' in n for n in z.namelist())
            verify_snapshot(snapshot)
    report.update(hapResourceDigestsVerified=result.returncode == 0, hapAbc=abc,
                  nativeLibraries=native_libraries,
                  resourcePackageSha256=hashlib.sha256(package_path.read_bytes()).hexdigest(),
                  files={str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in owned},
                  unsignedIntermediate={'path': str(hap.relative_to(ROOT)),
                                        'sha256': hashlib.sha256(hap.read_bytes()).hexdigest(),
                                        'bytes': hap.stat().st_size} if result.returncode == 0 else None,
                  phase='complete')

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('evidence', nargs='?')
    parser.add_argument('--toolchain-root', type=Path)
    parser.add_argument('--entry', choices=['hvigor', 'cli'], default='hvigor' if sys.platform.startswith('linux') else 'cli')
    args = parser.parse_args()
    run_name = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + uuid.uuid4().hex[:8]
    output = ROOT / args.evidence if args.evidence else ROOT / 'docs/evidence/development-runs' / ('api26-' + run_name)
    OUT = evidence_output(output, ['api26-build.log', 'build-report.json'])
    OUT.mkdir(parents=True, exist_ok=True)
    current_report = checked(BASE / '.build/build-report.json')
    report = {'exitCode': None, 'officialApi26Compile': False, 'phase': 'pending',
              'intermediateScope': 'formal chat-only HAP; isolated Xiaoyi demonstrations are not included',
              'signed': False, 'deliveryReplaced': False, 'frozenMaterialsRefreshed': False,
              'nativeRuntime': 'NOT_EXECUTED', 'xiaoyi': 'NOT_EXECUTED', 'productionPath': 'NOT_EXECUTED',
              'readyForHumanAcceptance': False, 'hapResourceDigestsVerified': False,
              'buildEntrySHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    try:
        build(args, OUT, report)
        report['qualificationExitCode'] = report['exitCode']
    except Exception as error:
        report.update(qualificationExitCode=1, failurePhase=report['phase'],
                      errorType=type(error).__name__, error=str(error))
        raise
    finally:
        report_path = OUT / 'build-report.json'
        report_path.write_text(json.dumps(report, indent=2), encoding='utf-8')
        current_report.parent.mkdir(parents=True, exist_ok=True)
        current_report.write_bytes(report_path.read_bytes())
        print(json.dumps({'exitCode': report.get('exitCode'),
                          'qualificationExitCode': report.get('qualificationExitCode'),
                          'hapResourceDigestsVerified': report['hapResourceDigestsVerified'],
                          'report': str(report_path.relative_to(ROOT))}))
    raise SystemExit(report['qualificationExitCode'])

if __name__ == '__main__': main()
