"""Short local HTTP/file workload; run with Python 3 and a built agent.

Usage: python scripts/checks/measure-agent.py path/to/agent
Measures parent-process RSS/CPU only, not a production service or journalctl.
Uses disposable config/state and never installs a service.
"""
import ctypes
import datetime
import http.server
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import threading
import time


def memory(pid):
    if os.name != "nt":
        fields = Path(f"/proc/{pid}/stat").read_text().split()
        return int(fields[23]) * os.sysconf("SC_PAGE_SIZE"), (int(fields[13]) + int(fields[14])) / os.sysconf("SC_CLK_TCK")
    from ctypes import wintypes
    class Counters(ctypes.Structure):
        _fields_ = [("cb", wintypes.DWORD), ("faults", wintypes.DWORD)] + [(n, ctypes.c_size_t) for n in ("peak", "rss", "peakPaged", "paged", "peakNonPaged", "nonPaged", "pagefile", "peakPagefile")]
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel.GetProcessTimes.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(ctypes.c_ulonglong)] * 4
    psapi = ctypes.WinDLL("psapi", use_last_error=True)
    psapi.GetProcessMemoryInfo.argtypes = [wintypes.HANDLE, ctypes.POINTER(Counters), wintypes.DWORD]
    handle = kernel.OpenProcess(0x410, False, pid)
    if not handle:
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        counters = Counters()
        counters.cb = ctypes.sizeof(counters)
        times = [ctypes.c_ulonglong() for _ in range(4)]
        if not psapi.GetProcessMemoryInfo(handle, ctypes.byref(counters), counters.cb) or not kernel.GetProcessTimes(handle, *(ctypes.byref(t) for t in times)):
            raise ctypes.WinError(ctypes.get_last_error())
        return counters.rss, (times[2].value + times[3].value) / 10_000_000
    finally:
        kernel.CloseHandle(handle)


def measure(exe, burst):
    events = set()
    health = {}
    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        def log_message(self, *_):
            pass
        def handle(self):
            try:
                super().handle()
            except (ConnectionResetError, BrokenPipeError):
                pass  # The measured child is terminated after the final sample.
        def do_GET(self):
            self.send_response(404)
            self.send_header("Content-Length", "0")
            self.end_headers()
        def do_POST(self):
            data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if self.path.endswith("/batch"):
                before = len(events)
                events.update(e["event_id"] for e in data["events"])
                result = {"accepted": len(data["events"]), "inserted": len(events) - before, "duplicates": len(data["events"]) - len(events) + before}
            else:
                health.update(data)
                result = {"received": True}
            body = json.dumps({"data": result}).encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix="sshguard-perf-") as temp:
            root = Path(temp)
            log = root / "auth.log"
            log.touch()
            config = root / "agent.json"
            config.write_text(json.dumps({"endpoint": f"http://127.0.0.1:{server.server_port}", "allow_insecure_http": True, "token": "a" * 64, "server_id": "perf-fixture", "report_interval_seconds": 1, "log_file": str(log)}))
            state = root / "state"
            with (root / "agent.log").open("w") as output:
                process = subprocess.Popen([str(exe), "-config", str(config), "-state-dir", str(state)], stdout=output, stderr=output)
                try:
                    time.sleep(5)
                    rss = []
                    _, cpu_before = memory(process.pid)
                    start = time.monotonic()
                    for i in range(60):
                        if process.poll() is not None:
                            raise RuntimeError((root / "agent.log").read_text())
                        if burst and i % 4 == 0:
                            stamp = datetime.datetime.now(datetime.timezone.utc).isoformat()
                            with log.open("a") as stream:
                                for n in range(100):
                                    stream.write(f"{stamp} host sshd[1]: Accepted publickey for fixture from 192.0.2.1 port 22 ssh2\n")
                                for n in range(400):
                                    stream.write(f"{stamp} ignored " + "x" * 2048 + "\n")
                        time.sleep(.25)
                        current, cpu_after = memory(process.pid)
                        rss.append(current)
                    elapsed = time.monotonic() - start
                    result = {"binary": exe.name, "scenario": "500 lines/s" if burst else "idle file", "seconds": round(elapsed, 2), "mean_rss_mib": round(statistics.mean(rss) / 2**20, 2), "peak_rss_mib": round(max(rss) / 2**20, 2), "cpu_percent_one_core": round(100 * (cpu_after - cpu_before) / elapsed, 2)}
                    deadline = time.monotonic() + 15
                    while burst and len(events) < 1500 and time.monotonic() < deadline:
                        time.sleep(.25)
                    result.update(received=len(events), collector_state=health.get("collector_state"), database_bytes=(state / "state.db").stat().st_size)
                    if burst and len(events) != 1500:
                        raise RuntimeError(f"lost events: {result}")
                    print(json.dumps(result), flush=True)
                finally:
                    process.terminate()
                    process.wait(timeout=10)
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    executable = Path(sys.argv[1]).resolve()
    for active in (False, True):
        measure(executable, active)
