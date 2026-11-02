"""Produce the emulator acceptance HAP from the distribution's unsigned HAP.

The distribution HAP must keep the TLS requirement and keep ArkWeb debugging
off; the emulator harness needs both relaxed. Rebuilding the whole project is
expensive (the LibRaw native library alone takes minutes and often wedges), so
this rewrites a copy of the built HAP instead:

  1. rawfile ``memoh_client.json``: point the seed at the emulator gateway,
     padded back to its fixed slot length;
  2. rawfile ``assets/index-*.js``: the IPv4 private-address (LAN) test
     ``t===10`` becomes ``t===-1``, so the packaged validator accepts the
     plain-HTTP gateway ``10.0.2.2``. Same length, no padding;
  3. ``ets/modules.abc``: the same ``first === 10`` term of the native
     validator (``ServiceUrl.ets``) becomes an unreachable constant, and the
     shell's ``setWebDebuggingAccess(false)`` loads ``true`` instead. Both are
     same-size instruction operands located through ``ark_disasm`` (never a
     fixed offset); the panda header checksum is recomputed.

Byte lengths must stay identical: the shell verifies each packaged resource
against ``WebResourceManifest.ets`` and refuses a load whose length drifted,
which shows up as a blank page rather than an error.

The input must be the distribution's own unsigned HAP (pristine). An already
patched copy is refused because its anchors are gone.

Signing uses the SDK's OpenHarmony test material and a debug profile bound to
one emulator UDID -- never the production Huawei key.

Usage:
  python acceptance-hap.py <unsigned.hap> <work-dir> <stub-port> <udid>
"""
from __future__ import annotations

import json
import os
import re
import struct
import subprocess
import sys
import tempfile
import zipfile
import zlib
from pathlib import Path

BUNDLE = 'cloud.begonia.workassistant'
DEVICE_HOST = '10.0.2.2'
KEYSTORE_PASS = '123456'
APP_KEY_ALIAS = 'openharmony application release'
PROFILE_ALIAS = 'openharmony application profile debug'
CA_ALIAS = 'openharmony application ca'
ROOT_CA_ALIAS = 'openharmony application root ca'
CA_SUBJECT = 'C=CN,O=OpenHarmony,OU=OpenHarmony Team,CN=OpenHarmony Application CA'
WEB_PREFIX = 'resources/rawfile/client-web/'
SEED = WEB_PREFIX + 'memoh_client.json'
ABC = 'ets/modules.abc'

STUDIO_LIB = Path(os.environ.get(
    'ACCEPTANCE_STUDIO_LIB',
    r'C:\Program Files\Huawei\DevEco Studio\sdk\default\openharmony\toolchains\lib'))
DISASM = Path(os.environ.get('ACCEPTANCE_DISASM', STUDIO_LIB.parent / 'ark_disasm.exe'))
JAVA = Path(os.environ.get(
    'ACCEPTANCE_JAVA',
    r'C:\Users\Begonia\AppData\Local\Programs\HoKit\resources\assets\tools\jre\bin\java.exe'))
KEYTOOL = JAVA.with_name('keytool.exe')
SIGN_JAR = STUDIO_LIB / 'hap-sign-tool.jar'
KEYSTORE = STUDIO_LIB / 'OpenHarmony.p12'
PROFILE_TEMPLATE = STUDIO_LIB / 'UnsgnedDebugProfileTemplate.json'
PROFILE_CERT = STUDIO_LIB / 'OpenHarmonyProfileDebug.pem'

# Pristine anchor from the distribution bundle. The acceptance edit must start
# from exactly this text, otherwise the file was already modified.
IPV4_ANCHOR = 'if(o=t===10||t===172&&r>=16&&r<=31||t===192&&r===168,'
IPV4_EXEMPT = IPV4_ANCHOR.replace('t===10||', 't===-1||', 1)

# Disassembly line: "<mnemonic> <operands>  # offset: 0x..., [FORMAT]...[0x.. 0x..]".
DISASM_LINE = re.compile(r'^\s*(?P<op>\S+)(?P<args>.*?)#\s*offset:\s*0x(?P<off>[0-9a-f]+),.*\[(?P<raw>0x[0-9a-f]{2}(?: 0x[0-9a-f]{2})*)\]\s*$')


def run(*args: object) -> subprocess.CompletedProcess:
    return subprocess.run([str(arg) for arg in args], capture_output=True, text=True,
                          encoding='utf-8', errors='replace')


def patch_bundle(data: bytes) -> tuple[bytes, str]:
    text = data.decode('utf-8')
    if text.count(IPV4_ANCHOR) != 1:
        raise SystemExit('distribution bundle is not pristine, or the IPv4 branch moved; '
                         'rebuild it from the reviewed source first')
    assert len(IPV4_EXEMPT) == len(IPV4_ANCHOR)
    patched = text.replace(IPV4_ANCHOR, IPV4_EXEMPT)
    assert len(patched) == len(text)
    return patched.encode('utf-8'), IPV4_EXEMPT


def disassemble(data: bytes) -> list[tuple[str, str, int, bytes]]:
    with tempfile.TemporaryDirectory() as scratch:
        abc, listing = Path(scratch, 'modules.abc'), Path(scratch, 'modules.txt')
        abc.write_bytes(data)
        result = run(DISASM, '--verbose', abc, listing)
        if result.returncode or not listing.exists():
            raise SystemExit(f'ark_disasm failed: {result.stdout}{result.stderr}')
        lines = listing.read_text(encoding='utf-8', errors='replace').splitlines()
    out = []
    for line in lines:
        match = DISASM_LINE.match(line)
        if match:
            raw = bytes(int(b, 16) for b in match['raw'].split())
            out.append((match['op'], match['args'].strip(), int(match['off'], 16), raw))
    return out


def abc_sites(code: list[tuple[str, str, int, bytes]]) -> dict[str, tuple[int, bytes, bytes]]:
    """Locate the two acceptance operands; each must be unique."""
    debug = [code[i - 3] for i in range(3, len(code))
             if code[i][0] == 'callthis1withname' and '"setWebDebuggingAccess"' in code[i][1]
             and code[i - 3][0] in ('ldfalse', 'ldtrue')]
    # ServiceUrl: lan = first === 10 || (first === 172 && ...) || (first === 192 && second === 168)
    lan = [code[i] for i in range(len(code) - 8)
           if code[i][0] == 'ldai' and code[i][1] == '0xa' and code[i + 1][0] == 'stricteq'
           and any(c[0] == 'ldai' and c[1] == '0xac' for c in code[i + 2:i + 9])]
    if len(debug) != 1 or debug[0][0] != 'ldfalse':
        raise SystemExit(f'setWebDebuggingAccess(false) site not unique/pristine: {debug}')
    if len(lan) != 1:
        raise SystemExit(f'native IPv4 LAN term not unique: {lan}')
    return {'webDebuggingAccess': (debug[0][2], b'\x03', b'\x02'),
            'nativeLanFirstOctet': (lan[0][2], bytes.fromhex('620a000000'), bytes.fromhex('62ffffffff'))}


def patch_abc(data: bytes) -> tuple[bytes, dict]:
    if data[:8] != b'PANDA\x00\x00\x00' or struct.unpack('<I', data[8:12])[0] != zlib.adler32(data[12:]):
        raise SystemExit('modules.abc header/checksum unexpected')
    sites = abc_sites(disassemble(data))
    patched = bytearray(data)
    for offset, before, after in sites.values():
        if bytes(patched[offset:offset + len(before)]) != before:
            raise SystemExit(f'unexpected bytes at 0x{offset:x}')
        patched[offset:offset + len(after)] = after
    patched[8:12] = struct.pack('<I', zlib.adler32(bytes(patched[12:])))
    # Re-read the result: debugging now loads true, the 10/8 term is gone.
    check = disassemble(bytes(patched))
    debug = [code for code in check if code[2] == sites['webDebuggingAccess'][0]]
    lan = [code for code in check if code[2] == sites['nativeLanFirstOctet'][0]]
    if not debug or debug[0][0] != 'ldtrue' or not lan or lan[0][3] != sites['nativeLanFirstOctet'][2]:
        raise SystemExit(f'abc re-read mismatch: {debug} {lan}')
    return bytes(patched), {name: f'0x{offset:x}' for name, (offset, _, _) in sites.items()}


def patch_seed(data: bytes, port: int) -> bytes:
    seed = json.loads(data.decode('utf-8'))
    seed['apiOrigin'] = f'http://{DEVICE_HOST}:{port}'
    body = json.dumps(seed, separators=(',', ':')).encode('utf-8')
    if len(body) > len(data):
        raise SystemExit(f'seed slot too small: {len(body)} > {len(data)}')
    return body + b' ' * (len(data) - len(body))


def rewrite(hap: Path, out: Path, port: int) -> dict:
    edits = {}
    with zipfile.ZipFile(hap) as source, zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as target:
        for entry in source.infolist():
            data = source.read(entry.filename)
            name = entry.filename
            if name == SEED:
                patched = patch_seed(data, port)
                edits[name] = {'kind': 'seed', 'bytes': len(data)}
                data = patched
            elif name.startswith(WEB_PREFIX) and name.endswith('.js') and IPV4_ANCHOR.encode() in data:
                data, applied = patch_bundle(data)
                edits[name] = {'kind': 'lan-exemption', 'bytes': len(data), 'text': applied}
            elif name == ABC:
                size = len(data)
                data, sites = patch_abc(data)
                assert len(data) == size
                edits[name] = {'kind': 'abc-operands', 'bytes': size, 'sites': sites}
            target.writestr(entry, data)
    kinds = sorted(edit['kind'] for edit in edits.values())
    if kinds != ['abc-operands', 'lan-exemption', 'seed']:
        raise SystemExit(f'expected exactly three acceptance edits, applied {sorted(edits)}')
    return edits


def make_profile(work: Path, udid: str) -> Path:
    sign = work / 'sign'
    sign.mkdir(parents=True, exist_ok=True)
    sub, root_cert = sign / 'sub.cer', sign / 'root.cer'
    for alias, out in ((CA_ALIAS, sub), (ROOT_CA_ALIAS, root_cert)):
        result = run(KEYTOOL, '-exportcert', '-alias', alias, '-keystore', KEYSTORE, '-storetype', 'PKCS12',
                     '-storepass', KEYSTORE_PASS, '-rfc', '-file', out)
        if result.returncode:
            raise SystemExit(result.stderr)
    chain = sign / 'app-debug.cer'
    result = run(JAVA, '-jar', SIGN_JAR, 'generate-app-cert',
                 '-keyAlias', APP_KEY_ALIAS, '-keyPwd', KEYSTORE_PASS,
                 '-issuer', CA_SUBJECT, '-issuerKeyAlias', CA_ALIAS, '-issuerKeyPwd', KEYSTORE_PASS,
                 '-issuerKeystoreFile', KEYSTORE, '-issuerKeystorePwd', KEYSTORE_PASS,
                 '-subject', 'C=CN,O=OpenHarmony,OU=OpenHarmony Team,CN=Acceptance Debug',
                 '-validity', '365', '-signAlg', 'SHA256withECDSA',
                 '-keystoreFile', KEYSTORE, '-keystorePwd', KEYSTORE_PASS,
                 '-rootCaCertFile', root_cert, '-subCaCertFile', sub,
                 '-outForm', 'certChain', '-outFile', chain)
    if result.returncode:
        raise SystemExit(result.stdout + result.stderr)
    leaf = chain.read_text(encoding='utf-8').strip().split('-----END CERTIFICATE-----')[0] + '-----END CERTIFICATE-----'
    profile = json.loads(PROFILE_TEMPLATE.read_text(encoding='utf-8'))
    profile['bundle-info']['bundle-name'] = BUNDLE
    profile['bundle-info']['development-certificate'] = leaf + '\n'
    profile['debug-info']['device-ids'] = [udid]
    profile['acls']['allowed-acls'] = ['ohos.permission.READ_IMAGEVIDEO']
    profile['validity'] = {'not-before': 1700000000, 'not-after': 1900000000}
    unsigned = sign / 'profile.json'
    unsigned.write_text(json.dumps(profile, indent=2), encoding='utf-8')
    signed = sign / 'profile.p7b'
    result = run(JAVA, '-jar', SIGN_JAR, 'sign-profile', '-mode', 'localSign',
                 '-keyAlias', PROFILE_ALIAS, '-keyPwd', KEYSTORE_PASS,
                 '-profileCertFile', PROFILE_CERT, '-inFile', unsigned, '-signAlg', 'SHA256withECDSA',
                 '-keystoreFile', KEYSTORE, '-keystorePwd', KEYSTORE_PASS, '-outFile', signed)
    if result.returncode:
        raise SystemExit(result.stdout + result.stderr)
    return signed


def sign(work: Path, hap: Path) -> Path:
    signed = work / 'acceptance-signed.hap'
    result = run(JAVA, '-jar', SIGN_JAR, 'sign-app', '-mode', 'localSign',
                 '-keyAlias', APP_KEY_ALIAS, '-keyPwd', KEYSTORE_PASS,
                 '-appCertFile', work / 'sign/app-debug.cer',
                 '-profileFile', work / 'sign/profile.p7b',
                 '-inFile', hap, '-signAlg', 'SHA256withECDSA',
                 '-keystoreFile', KEYSTORE, '-keystorePwd', KEYSTORE_PASS,
                 '-outFile', signed, '-compatibleVersion', '26', '-signCode', '1')
    if result.returncode:
        raise SystemExit(result.stdout + result.stderr)
    return signed


def verify(signed: Path, port: int) -> dict:
    with zipfile.ZipFile(signed) as archive:
        seed = json.loads(archive.read(SEED))
        names = [n for n in archive.namelist() if n.startswith(WEB_PREFIX)]
        bundle = next(n for n in names if n.endswith('.js') and IPV4_EXEMPT.encode() in archive.read(n))
        abc = archive.read(ABC)
    return {'seedOrigin': seed['apiOrigin'], 'webFiles': len(names), 'bundle': bundle,
            'abcChecksumOk': struct.unpack('<I', abc[8:12])[0] == zlib.adler32(abc[12:])}


def main() -> None:
    if len(sys.argv) < 5:
        raise SystemExit(__doc__)
    hap, work, port, udid = Path(sys.argv[1]), Path(sys.argv[2]), int(sys.argv[3]), sys.argv[4]
    work.mkdir(parents=True, exist_ok=True)
    patched = work / 'acceptance-unsigned.hap'
    edits = rewrite(hap, patched, port)
    profile = make_profile(work, udid)
    signed = sign(work, patched)
    print(json.dumps({'patched': str(patched), 'profile': str(profile), 'signed': str(signed),
                      'edits': edits, 'verify': verify(signed, port),
                      'origin': f'http://{DEVICE_HOST}:{port}'}, indent=2))


if __name__ == '__main__':
    main()
