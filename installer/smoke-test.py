"""Exercise the installer only in an isolated Windows build/test account."""
import argparse
import ctypes
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid
import winreg


def run(args, **kwargs):
    return subprocess.run([str(a) for a in args], capture_output=True, text=True,
                          encoding='utf-8', errors='replace', timeout=120,
                          creationflags=subprocess.CREATE_NO_WINDOW, check=True, **kwargs)


def shell_folder(identifier):
    buffer = ctypes.create_unicode_buffer(32768)
    if ctypes.windll.shell32.SHGetFolderPathW(None, identifier, None, 0, buffer):
        raise RuntimeError('Cannot locate the test account shell folder')
    return Path(buffer.value)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--installer', required=True)
    parser.add_argument('--dist', required=True)
    parser.add_argument('--report', required=True)
    args = parser.parse_args()
    installer, dist = Path(args.installer).resolve(), Path(args.dist).resolve()
    report = Path(args.report).resolve()
    version = '0.1.8'
    work = Path(tempfile.mkdtemp(prefix='agent-vault-installer-'))
    install = work / '安装目录 with spaces'
    fixture = work / 'isolated-vault'
    start_link = shell_folder(2) / 'Agent Vault' / 'Agent Vault.lnk'
    desktop_link = shell_folder(16) / 'Agent Vault.lnk'
    registry_path = r'Software\Microsoft\Windows\CurrentVersion\Uninstall\{2E596CFA-58F4-4FC9-926F-4CD2219DD0A8}_is1'
    if start_link.exists() or desktop_link.exists():
        raise RuntimeError('Smoke test requires an isolated account without an existing Agent Vault shortcut')
    try:
        existing = winreg.OpenKey(winreg.HKEY_CURRENT_USER, registry_path, 0,
                                 winreg.KEY_READ | winreg.KEY_WOW64_64KEY)
    except FileNotFoundError:
        pass
    else:
        existing.Close()
        raise RuntimeError('Smoke test will not replace an existing installation')

    data = Path(os.environ['LOCALAPPDATA']) / 'AgentVault'
    data.mkdir(parents=True, exist_ok=True)
    marker = data / ('installer-preservation-' + uuid.uuid4().hex + '.txt')
    marker.write_text('installer preservation fixture', encoding='utf-8')
    steps = []

    def install_package(log):
        run([installer, '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART',
             '/NOCLOSEAPPLICATIONS', '/NORESTARTAPPLICATIONS', '/SP-',
             '/LANG=chinesesimplified', '/TASKS=desktopicon', '/DIR=' + str(install),
             '/LOG=' + str(work / log)])

    def stop_fixture_hosts():
        # Filter by the exact test-installed image path; never kill by basename.
        script = "$p = '" + str(install / 'agent-vault.exe').replace("'", "''") + "'; Get-CimInstance Win32_Process -Filter \"Name='agent-vault.exe'\" | Where-Object { $_.ExecutablePath -eq $p } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction Stop }"
        run(['powershell.exe', '-NoProfile', '-NonInteractive', '-Command', script])

    try:
        install_package('install.log')
        for source in dist.rglob('*'):
            if source.is_file():
                installed = install / source.relative_to(dist)
                if not installed.is_file() or hashlib.sha256(source.read_bytes()).digest() != hashlib.sha256(installed.read_bytes()).digest():
                    raise RuntimeError('Installed payload differs: ' + source.relative_to(dist).as_posix())
        if not start_link.is_file() or not desktop_link.is_file():
            raise RuntimeError('Expected shortcuts were not created')
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, registry_path, 0,
                            winreg.KEY_READ | winreg.KEY_WOW64_64KEY) as key:
            if winreg.QueryValueEx(key, 'DisplayVersion')[0] != version:
                raise RuntimeError('Incorrect uninstall registration version')
            if Path(winreg.QueryValueEx(key, 'InstallLocation')[0]).resolve() != install:
                raise RuntimeError('Incorrect uninstall registration directory')
        steps.append('per-user install, Unicode/spaces path, payload hashes, shortcuts and uninstall registration')

        exe = install / 'agent-vault.exe'
        if not run([exe, 'help']).stdout.startswith('Agent Vault ' + version):
            raise RuntimeError('Installed CLI version mismatch')
        messages = [dict(jsonrpc='2.0', id=1, method='initialize', params={}),
                    dict(jsonrpc='2.0', method='notifications/initialized'),
                    dict(jsonrpc='2.0', id=2, method='tools/list')]
        output = run([exe, '--data-dir', str(fixture), 'mcp'],
                     input=''.join(json.dumps(m) + '\n' for m in messages)).stdout
        replies = [json.loads(line) for line in output.splitlines()]
        if len(replies) != 2 or replies[0]['result']['serverInfo']['version'] != version or len(replies[1]['result']['tools']) != 11:
            raise RuntimeError('Installed MCP protocol check failed')
        if '0.39.1' not in run([install / 'runtime/agent-browser.exe', '--version']).stdout:
            raise RuntimeError('Installed browser version mismatch')
        steps.append('installed CLI, MCP initialization/11 tools and pinned browser executable')

        run([install / 'AgentVault.exe', '--data-dir', str(fixture), 'serve', '--no-browser'])
        deadline = time.monotonic() + 15
        while True:
            try:
                status = json.loads(run([exe, '--data-dir', str(fixture), 'status']).stdout)
                if status.get('ok'):
                    break
            except subprocess.CalledProcessError:
                pass
            if time.monotonic() >= deadline:
                raise RuntimeError('Desktop launcher did not start the installed host')
            time.sleep(0.25)
        stop_fixture_hosts()
        steps.append('desktop launcher starts a working local host without a console')

        unmanaged = install / 'user-created-file.txt'
        unmanaged.write_text('keep this file', encoding='utf-8')
        install_package('reinstall.log')
        if not marker.is_file() or not unmanaged.is_file():
            raise RuntimeError('Reinstallation removed user-owned files')
        steps.append('same-version reinstall preserves user data and unmanaged files')

        run([install / 'unins000.exe', '/VERYSILENT', '/SUPPRESSMSGBOXES',
             '/NORESTART', '/LOG=' + str(work / 'uninstall.log')])
        deadline = time.monotonic() + 15
        while exe.exists() and time.monotonic() < deadline:
            time.sleep(0.25)
        if exe.exists() or (install / 'AgentVault.exe').exists() or start_link.exists() or desktop_link.exists():
            raise RuntimeError('Uninstaller left installed programs or shortcuts')
        try:
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, registry_path, 0,
                                winreg.KEY_READ | winreg.KEY_WOW64_64KEY):
                raise RuntimeError('Uninstall registration was not removed')
        except FileNotFoundError:
            pass
        if not marker.is_file() or not unmanaged.is_file():
            raise RuntimeError('Uninstaller removed user-owned files')
        steps.append('uninstall removes registered program/shortcuts and preserves user data')
        result = dict(status='passed', version=version, revision=os.environ.get('GITHUB_SHA'),
                      installer=installer.name, sha256=hashlib.sha256(installer.read_bytes()).hexdigest(),
                      checks=steps, environment='isolated GitHub Windows runner',
                      limitations=['Does not certify Smart App Control or SmartScreen acceptance on customer devices',
                                   'Real production authentication is not exercised by installer tests'])
        report.parent.mkdir(parents=True, exist_ok=True)
        report.write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False))
    finally:
        stop_fixture_hosts()


if __name__ == '__main__':
    main()
