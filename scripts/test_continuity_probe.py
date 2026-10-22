import datetime
import unittest

from continuity_probe import assess


class AssessmentTests(unittest.TestCase):
    def fixture(self):
        participants = [{"id": str(i), "name": str(i), "product": str(i % 2),
                         "version": "1", "official_url": "https://example.org",
                         "configuration": "test only"} for i in range(3)]
        events, challenges, replies = [], [], {}
        def event(actor, kind, second, data=None):
            seq = len(events) + 1
            events.append({"seq": seq, "actor": actor, "type": kind, "data": data,
                           "at": datetime.datetime.fromtimestamp(second, datetime.timezone.utc).isoformat()})
            return seq
        event("human", "start", 0)
        for p in participants:
            actor = p["id"]
            for second in range(0, 3601, 25):
                event(actor, "call", second, {"op": "wait_for_messages"})
            event(actor, "wait_timeout", 25)
            for n in range(10):
                key = actor + "-" + str(n)
                challenges.append({"id": key, "agent": actor, "answer": 42,
                                   "message_id": key, "deadline": 3600})
                event(actor, "ack", n * 360, key)
                replies[key] = {"answer": 42, "elapsed": n * 360, "seq": event(actor, "message", n * 360)}
                event(actor, "call", n * 360 + 1, {"op": "wait_for_messages"})
        event("human", "close", 3600)
        return events, participants, challenges, replies

    def test_protocol_success_is_never_certification(self):
        result = assess(*self.fixture(), 3600, 3600)
        self.assertTrue(result["protocol_checks_passed"])
        self.assertEqual(result["certification"], "pending_human_attestation")

    def test_go_nanosecond_timestamps(self):
        events, ps, challenges, replies = self.fixture()
        for event in events:
            event["at"] = event["at"].replace("+00:00", ".123456789Z")
        self.assertTrue(assess(events, ps, challenges, replies, 3600, 3600)["protocol_checks_passed"])

    def test_short_run_and_wrong_answer_fail(self):
        data = self.fixture()
        self.assertFalse(assess(*data, 60, 60)["protocol_checks_passed"])
        data[3]["0-0"]["answer"] = -1
        self.assertFalse(assess(*data, 3600, 3600)["protocol_checks_passed"])

    def test_stopped_agent_and_placeholder_metadata_fail(self):
        events, ps, challenges, replies = self.fixture()
        events = [e for e in events if not (e["actor"] == "0" and e["at"] > "1970-01-01T00:55:00")]
        self.assertFalse(assess(events, ps, challenges, replies, 3600, 3600)["protocol_checks_passed"])
        data = self.fixture()
        data[1][0]["configuration"] = "REPLACE"
        self.assertFalse(assess(*data, 3600, 3600)["protocol_checks_passed"])

    def test_missing_ack_or_post_work_wait_fails(self):
        events, ps, challenges, replies = self.fixture()
        events = [e for e in events if not (e["actor"] == "0" and e["type"] == "ack")]
        self.assertFalse(assess(events, ps, challenges, replies, 3600, 3600)["protocol_checks_passed"])


if __name__ == "__main__":
    unittest.main()
