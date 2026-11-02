"""Produce the emulator acceptance HAP from an already-built unsigned HAP.

The distribution HAP must keep the TLS requirement and keep ArkWeb debugging
off; the emulator harness needs both relaxed. Rebuilding the whole project is
expensive (the LibRaw native library alone takes minutes and often wedges), so
this rewrites a copy of the built HAP instead:

  1. rawfile ``memoh_client.json``: point the seed at the emulator gateway,
     padded back to its fixed slot length;
  2. rawfile ``assets/index-*.js``: keep the gateway out of the private-address
     (LAN) set so the packaged validator accepts a plain-HTTP endpoint,
     padded back to the original span;
  3. the compiled ABC is NOT touched here. The shell's
     ``setWebDebuggingAccess`` flag and the native URL validator live in ArkTS,
     so an HAP that was not built with the acceptance source edits cannot be
     driven. ``--require-debuggable`` checks the device instead of guessing.

Byte lengths must stay identical: the shell verifies each packaged resource
against ``WebResourceManifest.ets`` and refuses a load whose length drifted,
which shows up as a blank page rather than an error.

Signing uses the SDK's OpenHarmony test material and a debug profile bound to
one emulator UDID -- never the production Huawei key.

Usage:
  python acceptance-hap.py <unsigned.hap> <work-dir> <stub-port> <udid>
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import zipfile
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

STUDIO_LIB = Path(os.environ.get(
    'ACCEPTANCE_STUDIO_LIB',
    r'C:\Program Files\Huawei\DevEco Studio\sdk\default\openharmony\toolchains\lib'))
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


def run(*args: object) -> subprocess.CompletedProcess:
    return subprocess.run([str(arg) for arg in args], capture_output=True, text=True,
                          encoding='utf-8', errors='replace')


def patch_bundle(data: bytes, port: int) -> tuple[bytes, str]:
    text = data.decode('utf-8')
    if text.count(IPV4_ANCHOR) != 1:
        raise SystemExit('distribution bundle is not pristine, or the IPv4 branch moved; '
                         'rebuild it from the reviewed source first')
    replacement = (f'if(o=(t===10||t===172&&r>=16&&r<=31||t===192&&r===168)'
                   f'&&a!==`{DEVICE_HOST}`,')
    if len(replacement) > len(IPV4_ANCHOR):
        raise SystemExit(f'replacement needs {len(replacement)} of {len(IPV4_ANCHOR)} bytes')
    padded = replacement + ' ' * (len(IPV4_ANCHOR) - len(replacement))
    patched = text.replace(IPV4_ANCHOR, padded)
    assert len(patched) == len(text)
    return patched.encode('utf-8'), padded


def patch_seed(data: bytes, port: int) -> bytes:
    seed = json.loads(data.decode('utf-8'))
    seed['apiOrigin'] = f'http://{DEVICE_HOST}:{port}'
    body = json.dumps(seed).encode('utf-8')
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
                data, applied = patch_bundle(data, port)
                edits[name] = {'kind': 'lan-exemption', 'bytes': len(data), 'text': applied.strip()}
            target.writestr(entry, data)
    expected = {SEED, *[k for k in edits if k != SEED]}
    if len(expected) != 2:
        raise SystemExit(f'expected exactly two acceptance edits, applied {sorted(edits)}')
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
        bundle = next(n for n in names if n.endswith('.js') and b'10.0.2.2' in archive.read(n))
        text = archive.read(bundle).decode('utf-8')
    return {'seedOrigin': seed['apiOrigin'], 'webFiles': len(names),
            'bundle': bundle, 'gatewayExempt': f'a!==`{DEVICE_HOST}`' in text}


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
                      'origin': f'http://{DEVICE_HOST}:{port}',
                      'note': 'shell debugging flag is compiled into the ABC; '
                              'this HAP is only drivable if it was built with the '
                              'acceptance source edit (see README)'}, indent=2))


if __name__ == '__main__':
    main()
