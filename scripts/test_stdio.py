"""Real binary/stdio/socket tests with simulated MCP peers, never model CLIs."""
import json
import pathlib
import queue
import subprocess
import sys
import tempfile
import threading
import time
import unittest


BINARY = pathlib.Path(__file__).resolve().parents[1] / "bin" / "collab"


class Peer:
    def __init__(self, config):
        self.process = subprocess.Popen([str(BINARY), "mcp", "--config", str(config)],
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, text=True)
        self.incoming = queue.Queue()
        self.sequence = 0
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()
        self.send("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                 "clientInfo": {"name": "simulated-test", "version": "1"}})
        initialized = self.receive()
        if "result" not in initialized:
            raise RuntimeError(initialized)
        self.notify("notifications/initialized", {})

    def read(self):
        for line in self.process.stdout:
            self.incoming.put(json.loads(line))

    def send(self, method, params):
        self.sequence += 1
        self.process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": self.sequence,
                                            "method": method, "params": params}) + "\n")
        self.process.stdin.flush()

    def notify(self, method, params):
        self.process.stdin.write(json.dumps({"jsonrpc": "2.0", "method": method,
                                            "params": params}) + "\n")
        self.process.stdin.flush()

    def receive(self):
        while True:
            message = self.incoming.get(timeout=5)
            if "id" in message:
                return message

    def tool(self, name, **arguments):
        self.send("tools/call", {"name": name, "arguments": arguments})
        result = self.receive()["result"]
        if result.get("isError"):
            raise RuntimeError(result)
        return json.loads(result["content"][0]["text"])

    def close(self):
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                self.process.wait(timeout=5)
        self.reader.join(timeout=2)
        self.process.stdout.close()
        self.process.stderr.close()


@unittest.skipUnless(BINARY.exists(), "build bin/collab first for stdio integration")
class StdioTests(unittest.TestCase):
    def test_three_bridges_reconnect_timeout_and_close(self):
        with tempfile.TemporaryDirectory(prefix="ac-", dir="/tmp") as directory:
            root = pathlib.Path(directory)
            metadata = root / "participants.json"
            metadata.write_text(json.dumps([
                {"name": str(n), "product": "simulated", "version": "1",
                 "official_url": "https://example.org", "configuration": "test"}
                for n in range(3)]))
            run = root / "run"
            subprocess.run([str(BINARY), "init", "--dir", str(run), "--participants", str(metadata)],
                           check=True, capture_output=True, timeout=5)
            daemon = subprocess.Popen([str(BINARY), "serve", "--dir", str(run)],
                                      stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            peers = []
            try:
                for _ in range(100):
                    if (run / "daemon.sock").exists():
                        break
                    if daemon.poll() is not None:
                        self.fail(daemon.stderr.read().decode())
                    time.sleep(.02)
                for n in range(1, 4):
                    peer = Peer(run / ("agent-%d.json" % n))
                    peers.append(peer)
                state = peers[0].tool("get_session_state")
                self.assertEqual(len(state["participants"]), 3)
                recipient = state["participants"][1]["id"]
                peers[1].send("tools/call", {"name": "wait_for_messages", "arguments": {"timeout_ms": 2000}})
                sent = peers[0].tool("send_message", to=recipient, body="hello", idempotency_key="one")
                delivered = peers[1].receive()["result"]
                self.assertFalse(delivered.get("isError"))
                self.assertEqual(json.loads(delivered["content"][0]["text"])["messages"][0]["id"], sent["message_id"])
                peers[1].close()
                peers[1] = Peer(run / "agent-2.json")
                self.assertEqual(peers[1].tool("check_inbox")["messages"][0]["id"], sent["message_id"])
                peers[1].tool("acknowledge_message", message_id=sent["message_id"], idempotency_key="ack")
                self.assertNotIn("messages", peers[1].tool("wait_for_messages", timeout_ms=10))
                errors = queue.Queue()
                def simulated_loop(peer):
                    try:
                        while True:
                            incoming = peer.tool("wait_for_messages", timeout_ms=20)
                            if incoming["status"] == "closed":
                                return
                            for message in incoming.get("messages", []):
                                challenge = json.loads(message["body"])
                                peer.tool("acknowledge_message", message_id=message["id"], idempotency_key="ack-" + message["id"])
                                peer.tool("send_message", to="human", idempotency_key=challenge["challenge_id"], body=json.dumps({
                                    "kind": "continuity_result", "run_id": challenge["run_id"],
                                    "challenge_id": challenge["challenge_id"], "answer": challenge["a"] * challenge["b"]}))
                    except Exception as exc:
                        errors.put(exc)
                workers = [threading.Thread(target=simulated_loop, args=(p,), daemon=True) for p in peers]
                for worker in workers:
                    worker.start()
                driver = pathlib.Path(__file__).with_name("continuity_probe.py")
                evidence = root / "evidence"
                driven = subprocess.run([sys.executable, str(driver), "--binary", str(BINARY),
                                         "--config", str(run / "admin.json"), "--output", str(evidence),
                                         "--duration", "4", "--rounds", "2"],
                                        capture_output=True, text=True, timeout=15)
                for worker in workers:
                    worker.join(timeout=5)
                    self.assertFalse(worker.is_alive(), "closed session failed to wake bridge")
                self.assertTrue(errors.empty(), str(list(errors.queue)))
                self.assertEqual(driven.returncode, 1, driven.stdout + driven.stderr)
                report = json.loads((evidence / "report.json").read_text())
                self.assertEqual(report["certification"], "not_passed")
                self.assertIsNone(report["failure"])
                self.assertNotIn("export_error", report)
                for result in report["agents"].values():
                    self.assertEqual(result["correct_rounds"], 2)
                    self.assertEqual(result["work_wait_cycles"], 2)
                exported = subprocess.run([str(BINARY), "export", "--config", str(run / "admin.json")],
                                          check=True, capture_output=True, text=True, timeout=5)
                self.assertIn("wait_timeout", {e["type"] for e in json.loads(exported.stdout)["events"]})
            finally:
                for peer in peers:
                    peer.close()
                daemon.terminate()
                daemon.wait(timeout=5)
                daemon.stderr.close()


if __name__ == "__main__":
    unittest.main()
