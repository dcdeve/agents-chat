"""Process-crash and CLI lifecycle checks; no model clients are launched."""
import json
import pathlib
import subprocess
import tempfile
import time
import unittest

from test_stdio import BINARY, Peer


@unittest.skipUnless(BINARY.exists(), "build bin/collab first")
class PersistenceTests(unittest.TestCase):
    def invoke(self, *args, success=True):
        result = subprocess.run([str(BINARY), *map(str, args)], capture_output=True,
                                text=True, timeout=10)
        if success:
            self.assertEqual(result.returncode, 0, result.stderr)
        return result

    def call(self, config, op, **params):
        result = self.invoke("call", "--config", config, "--op", op,
                             "--json", json.dumps(params))
        return json.loads(result.stdout)

    def start(self, run):
        process = subprocess.Popen([str(BINARY), "serve", "--dir", str(run)],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        for _ in range(100):
            if process.poll() is not None:
                self.fail(process.stderr.read().decode())
            result = self.invoke("session", "show", "--config", run / "admin.json", success=False)
            if result.returncode == 0:
                return process
            time.sleep(.02)
        process.terminate()
        process.wait(timeout=5)
        self.fail("daemon did not become ready")

    def stop(self, process, crash=False):
        if process.poll() is None:
            process.kill() if crash else process.terminate()
        process.wait(timeout=5)
        process.stderr.close()

    def test_sigkill_preserves_mailbox_ack_replay_and_live_bridge(self):
        with tempfile.TemporaryDirectory(prefix="ac-", dir="/tmp") as directory:
            root = pathlib.Path(directory)
            metadata = root / "participants.json"
            metadata.write_text(json.dumps([
                {"name": str(n), "product": "simulated", "version": "1",
                 "official_url": "https://example.org", "configuration": "test"}
                for n in range(3)]))
            run = root / "run"
            self.invoke("init", "--dir", run, "--participants", metadata)
            process = self.start(run)
            peer = None
            try:
                admin = run / "admin.json"
                state = self.call(admin, "get_session_state")
                recipient = state["participants"][1]["id"]
                peer = Peer(run / "agent-2.json")
                sent = self.call(run / "agent-1.json", "send_message", to=recipient,
                                 body="survives abrupt exit", idempotency_key="once")
                # A second daemon must neither become a writer nor unlink the socket.
                second = self.invoke("serve", "--dir", run, success=False)
                self.assertNotEqual(second.returncode, 0)
                self.assertIn("another daemon", second.stderr)
                self.stop(process, crash=True)
                process = self.start(run)
                retry = self.call(run / "agent-1.json", "send_message", to=recipient,
                                  body="survives abrupt exit", idempotency_key="once")
                self.assertEqual(retry, sent)
                inbox = peer.tool("check_inbox")
                self.assertEqual(inbox["messages"][0]["id"], sent["message_id"])
                peer.tool("acknowledge_message", message_id=sent["message_id"], idempotency_key="ack")
                self.stop(process, crash=True)
                process = self.start(run)
                self.assertNotIn("messages", peer.tool("check_inbox"))
                exported = json.loads(self.invoke("export", "--config", admin).stdout)
                self.assertEqual(sum(e["type"] == "message" for e in exported["events"]), 1)
                self.assertEqual([e["seq"] for e in exported["events"]], list(range(1, len(exported["events"]) + 1)))
            finally:
                if peer:
                    peer.close()
                self.stop(process)

    def test_session_and_agent_commands(self):
        with tempfile.TemporaryDirectory(prefix="ac-", dir="/tmp") as directory:
            root = pathlib.Path(directory)
            run = root / "run"
            self.invoke("session", "create", "--dir", run, "--objective", "test lifecycle")
            process = self.start(run)
            try:
                admin = run / "admin.json"
                for n in range(3):
                    args = {"idempotency_key": "enroll-" + str(n), "expected_version": n + 1,
                            "participant": {"name": str(n), "product": "simulated", "version": "1",
                                            "official_url": "https://example.org", "configuration": "test"}}
                    commands = ["agent", "register", "--config", admin, "--output", root / (str(n) + ".json"), "--json", json.dumps(args)]
                    first = self.invoke(*commands)
                    retry = self.invoke(*commands)
                    self.assertEqual(json.loads(first.stdout), json.loads(retry.stdout))
                started = self.invoke("session", "start", "--config", admin, "--json",
                                      '{"idempotency_key":"start","expected_version":4}')
                self.assertEqual(json.loads(started.stdout)["status"], "active")
                state = json.loads(self.invoke("agent", "list", "--config", admin).stdout)
                revoked = state["participants"][0]["id"]
                self.invoke("agent", "revoke", "--config", admin, "--json", json.dumps({
                    "idempotency_key": "revoke", "expected_version": 5, "agent_id": revoked}))
                denied = self.invoke("agent", "reconnect", "--config", root / "0.json", success=False)
                self.assertNotEqual(denied.returncode, 0)
                self.assertEqual(json.loads(denied.stdout)["error"], "unauthorized")
            finally:
                self.stop(process)


if __name__ == "__main__":
    unittest.main()
