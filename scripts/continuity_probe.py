#!/usr/bin/env python3
"""Drive the phase-zero experiment; never launches or resumes agent CLIs."""
import argparse
import datetime
import json
import pathlib
import re
import secrets
import subprocess
import time
import uuid


def call(binary, config, op, **params):
    proc = subprocess.run(
        [binary, "call", "--config", config, "--op", op,
         "--json", json.dumps(params)],
        check=True, capture_output=True, text=True, timeout=35,
    )
    result = json.loads(proc.stdout)
    if result.get("error"):
        raise RuntimeError(result["error"])
    return result


def assess(events, participants, challenges, replies, duration, elapsed):
    """Evaluate protocol evidence, never assert absence of human intervention."""
    results = {}
    def timestamp(event):
        # Go emits RFC3339Nano; Python 3.9 accepts only microsecond precision.
        value = re.sub(r"\.(\d+)", lambda m: "." + m[1][:6].ljust(6, "0"), event["at"])
        return datetime.datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
    beginning = min((timestamp(e) for e in events), default=0)
    ending = max((timestamp(e) for e in events), default=elapsed)
    for participant in participants:
        actor = participant["id"]
        waits = [e for e in events if e["actor"] == actor and
                 e["type"] == "call" and e.get("data", {}).get("op") == "wait_for_messages"]
        timeouts = [e for e in events if e["actor"] == actor and e["type"] == "wait_timeout"]
        calls = sorted(timestamp(e) for e in events if e["actor"] == actor and e["type"] == "call")
        boundaries = [beginning] + calls + [ending]
        max_gap = max(b - a for a, b in zip(boundaries, boundaries[1:]))
        expected = [c for c in challenges if c["agent"] == actor]
        correct = sum(replies.get(c["id"], {}).get("answer") == c["answer"] and
                      replies[c["id"]]["elapsed"] <= c["deadline"] for c in expected)
        acked = {e.get("data") for e in events if e["actor"] == actor and e["type"] == "ack"}
        # Each distinct challenge must be acknowledged, answered, then followed
        # by a new wait: ten polls before a single work burst do not count.
        cycles = 0
        for c in expected:
            reply = replies.get(c["id"], {})
            if (reply.get("answer") == c["answer"] and
                    reply.get("elapsed", float("inf")) <= c["deadline"] and
                    c["message_id"] in acked and
                    any(w["seq"] > reply.get("seq", float("inf")) for w in waits)):
                cycles += 1
        results[actor] = {"name": participant["name"], "correct_rounds": correct,
                          "work_wait_cycles": cycles, "wait_calls": len(waits),
                          "empty_timeouts": len(timeouts), "max_call_gap_seconds": max_gap}
    products = {p["product"].strip().casefold() for p in participants}
    metadata_complete = all(all(p.get(k) and "REPLACE" not in p[k] for k in
                            ("product", "version", "official_url", "configuration")) for p in participants)
    passed = (metadata_complete and duration >= 3600 and elapsed >= 3600 and len(participants) >= 3 and
              len(products) >= 2 and all(r["work_wait_cycles"] >= 10 and
                                        r["empty_timeouts"] > 0 and r["max_call_gap_seconds"] <= 90 for r in results.values()))
    return {"protocol_checks_passed": passed, "agents": results,
            "certification": "pending_human_attestation" if passed else "not_passed",
            "required_attestation": "Three manually opened real CLIs; no manual turn reactivation; exact configurations and terminal evidence attached."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="./bin/collab")
    parser.add_argument("--config", required=True, help="admin.json")
    parser.add_argument("--output", required=True, help="new evidence directory")
    parser.add_argument("--duration", type=float, default=3600)
    parser.add_argument("--rounds", type=int, default=10)
    args = parser.parse_args()
    if args.duration <= 0 or args.rounds < 1:
        parser.error("duration and rounds must be positive")
    output = pathlib.Path(args.output)
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    run_id = str(uuid.uuid4())
    invoke = lambda op, **kw: call(args.binary, args.config, op, **kw)
    state = invoke("get_session_state")
    if state["status"] != "active":
        raise RuntimeError("session is not active")
    participants = state["participants"]
    start_seq = state["seq"]
    challenges, replies, seen = [], {}, set()
    started = time.monotonic()
    report = {"run_id": run_id, "participants": participants,
              "duration_seconds": args.duration, "rounds": args.rounds,
              "human_interventions": None, "terminal_evidence": [],
              "certification": "not_passed"}
    failure = None
    try:
        round_number = 0
        while time.monotonic() - started < args.duration:
            elapsed = time.monotonic() - started
            if round_number < args.rounds and elapsed >= round_number * args.duration / args.rounds:
                for p in participants:
                    a, b = secrets.randbelow(800) + 100, secrets.randbelow(800) + 100
                    challenge_id = str(uuid.uuid4())
                    body = {"kind": "continuity_probe", "run_id": run_id,
                            "challenge_id": challenge_id, "round": round_number + 1,
                            "a": a, "b": b,
                            "instruction": "Multiply a by b; ACK receipt; send human a JSON continuity_result with run_id, challenge_id, answer; then wait again."}
                    sent = invoke("send_message", idempotency_key=challenge_id,
                                  to=p["id"], body=json.dumps(body))
                    challenges.append({"id": challenge_id, "agent": p["id"],
                                       "answer": a * b, "message_id": sent["message_id"],
                                       "deadline": min(args.duration, elapsed + 120)})
                round_number += 1
                print("Dispatched round", round_number, flush=True)
            inbox = invoke("check_inbox")
            for message in inbox.get("messages", []):
                if message["id"] in seen:
                    continue
                seen.add(message["id"])
                try:
                    result = json.loads(message["body"])
                    if not isinstance(result, dict):
                        result = {}
                except (ValueError, TypeError):
                    result = {}
                challenge = next((c for c in challenges if c["id"] == result.get("challenge_id") and c["agent"] == message["sender"]), None)
                if challenge and result.get("run_id") == run_id and result.get("kind") == "continuity_result":
                    # First answer is the evidence; later retries cannot repair it.
                    replies.setdefault(challenge["id"], {"answer": result.get("answer"),
                                       "elapsed": time.monotonic() - started, "seq": message["seq"]})
                invoke("acknowledge_message", idempotency_key="ack-" + message["id"], message_id=message["id"])
            time.sleep(min(1, max(0, args.duration - (time.monotonic() - started))))
    except (Exception, KeyboardInterrupt) as exc:
        failure = str(exc) or type(exc).__name__
    finally:
        elapsed = time.monotonic() - started
        report.update(elapsed_seconds=elapsed, challenges=challenges, replies=replies, failure=failure)
        try:
            current = invoke("get_session_state")
            invoke("close_session", idempotency_key="close-" + run_id,
                   expected_version=current.get("version", 0))
            exported = invoke("export")
            events = [e for e in exported.get("events", []) if e["seq"] > start_seq]
            with (output / "events.json").open("x") as f:
                json.dump(exported, f, indent=2)
            report.update(assess(events, participants, challenges, replies, args.duration, elapsed))
            if failure:
                report.update(protocol_checks_passed=False, certification="not_passed")
        except Exception as exc:
            report["export_error"] = str(exc)
            report.update(protocol_checks_passed=False, certification="not_passed")
        with (output / "report.json").open("x") as f:
            json.dump(report, f, indent=2)
    print(json.dumps({"report": str(output / "report.json"), "certification": report["certification"]}))
    return 0 if report.get("protocol_checks_passed") else 1


if __name__ == "__main__":
    raise SystemExit(main())
