"""Inspect HoKit and invoke the official CLI in a project-only environment."""
from __future__ import annotations
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3]
BASE = ROOT / 'validation/harmony'
HOKIT = Path.home() / 'AppData/Local/Programs/HoKit'


def linux_hvigor(clt: Path, project: Path):
    """Official CLT entry; never imports devecocli or changes HOME/vendor files."""
    clt = clt.absolute()
    metadata = clt / 'sdk/default/openharmony/ets/oh-uni-package.json'
    if not metadata.is_file():
        raise RuntimeError('Official Linux CLT/SDK not installed: missing SDK package metadata')
    package = json.loads(metadata.read_text())
    if str(package.get('apiVersion')) != '26' or package.get('version') != '26.0.0.105':
        raise RuntimeError('Expected official API 26 SDK version 26.0.0.105')
    node = clt / 'tool/node/bin/node'
    hvigor = clt / 'bin/hvigorw'
    ohpm = clt / 'bin/ohpm'
    for path in (node, hvigor, ohpm):
        if not path.is_file():
            raise RuntimeError('Official Linux CLT entry is missing: ' + path.name)
    with node.open('rb') as stream:
        if stream.read(4) != b'\x7fELF':
            raise RuntimeError('CLT Node must be a Linux executable; Windows SDK is not usable')
    profile = json.loads((project / 'build-profile.json5').read_text())
    if profile['app'].get('signingConfigs'):
        raise RuntimeError('Cloud build accepts unsigned configuration only')
    cache = ROOT / 'validation/harmony/.build'
    cache.mkdir(parents=True, exist_ok=True)
    environment = dict(os.environ,
                       PATH=str(node.parent) + os.pathsep + str(clt / 'bin') + os.pathsep + os.environ.get('PATH', ''),
                       HVIGOR_USER_HOME=str(cache / 'hvigor-user'),
                       OHPM_HOME=str(cache / 'ohpm-home'),
                       npm_config_cache=str(cache / 'npm-cache'))
    # Login/terms or permission failures are returned unchanged, never bypassed.
    result = subprocess.run([str(ohpm), 'install', '--all'], cwd=project, env=environment)
    if result.returncode:
        return result.returncode
    return subprocess.run([str(hvigor), 'assembleHap', '--mode', 'module',
                           '-p', 'product=default', '-p', 'module=entry@default',
                           '-p', 'buildMode=debug', '--no-daemon'],
                          cwd=project, env=environment).returncode


def main():
    if sys.argv[1:2] == ['hvigor']:
        parser = argparse.ArgumentParser(description='Official Linux CLT unsigned build entry')
        parser.add_argument('mode')
        parser.add_argument('--clt-path', type=Path, required=True)
        args = parser.parse_args()
        raise SystemExit(linux_hvigor(args.clt_path, ROOT / 'mobile/harmony/app'))
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode', choices=['doctor', 'cli'])
    parser.add_argument('--clt-path', type=Path, help='实际官方CommandLineTools根目录；不会下载安装或改全局配置')
    parser.add_argument('--studio-path', type=Path, help='实际DevEco Studio根目录')
    parser.add_argument('--toolchain-root', type=Path, help='只读复用已有项目工具链；缓存/项目/输出仍在当前工作树')
    cli_args = sys.argv[2:] if len(sys.argv) > 1 and sys.argv[1] == 'cli' else []
    project = ROOT / 'mobile/harmony/app'
    shared_tools = None
    while cli_args[:1] in (['--project'], ['--toolchain-root']):
        if len(cli_args) < 3:
            parser.error('CLI prefix requires its value and an official command')
        option, value = cli_args[:2]
        if option == '--project':
            if value != 'production':
                parser.error('--project must select production')
        else:
            shared_tools = Path(value)
        cli_args = cli_args[2:]
    args = parser.parse_args(['cli'] if cli_args else None)
    tools_root = (shared_tools or args.toolchain_root or BASE / '.build').absolute()
    assert tools_root.resolve() == tools_root
    for path in (tools_root, *tools_root.parents):
        assert not path.is_symlink() and not path.is_junction(), 'Linked shared toolchain root'
    isolated_clt = tools_root / 'commandline-tools/command-line-tools'
    if not args.clt_path and not args.studio_path and isolated_clt.is_dir():
        args.clt_path = isolated_clt
    cli = tools_root / 'deveco/node_modules/@deveco/deveco-cli/dist/cli.js'
    if args.mode == 'cli':
        if not cli.is_file() or not cli_args:
            parser.error('cli模式需要项目内已安装的官方CLI及明确命令参数')
        temp_dir = BASE / '.build/toolchain-temp'
        temp_dir.mkdir(parents=True, exist_ok=True)
        env = dict(os.environ, USERPROFILE=str(BASE / '.build/toolchain-user'),
                   LOCALAPPDATA=str(BASE / '.build/local-app-data'),
                   TEMP=str(temp_dir), TMP=str(temp_dir),
                   DEVECO_CLI_DATA_DIR=str(BASE / '.build/deveco-data'),
                   DEVECO_CLI_DISABLE_TELEMETRY='1', DEVECO_CLI_SKIP_VERSION_CHECK='1',
                   DEVECO_CLI_DISABLE_UPDATE='all', HVIGOR_USER_HOME=str(BASE / '.build/hvigor-user'),
                   OHPM_HOME=str(BASE / '.build/ohpm-home'))
        if args.clt_path:
            env['DEVECO_CLI_CLT_PATH'] = str(args.clt_path.resolve())
        if args.studio_path:
            env['DEVECO_CLI_STUDIO_PATH'] = str(args.studio_path.resolve())
        raise SystemExit(subprocess.run(['node', str(cli), *cli_args], cwd=project, env=env).returncode)
    report = {'hoKitDirectory': str(HOKIT), 'hoKitInstalled': HOKIT.is_dir(),
              'hdc': (HOKIT / 'resources/assets/tools/hdc/hdc.exe').is_file(),
              'java': (HOKIT / 'resources/assets/tools/jre/bin/java.exe').is_file(),
              'hapSigner': (HOKIT / 'resources/assets/tools/sign/libs/hap-sign-tool.jar').is_file(),
              'hoKitIsApi26Sdk': False, 'officialCliInstalled': cli.is_file(),
              'providedCltPath': str(args.clt_path) if args.clt_path else None,
              'providedStudioPath': str(args.studio_path) if args.studio_path else None,
              'nativeBuildVerified': False, 'signedHapAvailable': False}
    sdk_meta = args.clt_path / 'sdk/default/openharmony/ets/oh-uni-package.json' if args.clt_path else None
    report['api26SdkInstalled'] = bool(sdk_meta and sdk_meta.is_file() and json.loads(sdk_meta.read_text(encoding='utf-8')).get('apiVersion') == '26')
    if args.mode == 'doctor':
        evidence = ROOT / 'docs/evidence/harmony-validation/toolchain-inspection.json'
        evidence.write_text(json.dumps(report, indent=2), encoding='utf-8')
        print(json.dumps(report, indent=2)); return


if __name__ == '__main__':
    main()
