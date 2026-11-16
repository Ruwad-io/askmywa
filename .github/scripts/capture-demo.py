#!/usr/bin/env python3
"""Capture real MCP results from an isolated, fictional archive. Never pairs or sends.

Usage: python3 .github/scripts/capture-demo.py /path/to/released/askmywa
Only read tools and send_message(confirm=false) are allowed by this script.
"""
import hashlib
import json
from pathlib import Path
import re
import selectors
import sqlite3
import subprocess
import sys
import tempfile
import unicodedata

ROOT = Path(__file__).resolve().parents[2]
GROUP = "120363000000001@g.us"
PHONES = {"Sophie": "99900000001", "Paul": "99900000002", "Inès": "99900000003", "Lucas": "99900000004"}
TEXT = {
    "fr": {
        "chat": "Immeuble — démo", "contact": "Sophie (démo)", "query": "ascenseur",
        "messages": [
            ("08:02", "Sophie", "Quelqu'un a des nouvelles de l'ascenseur ?"),
            ("08:34", "Paul", "Le syndic prévoit de répartir les frais au prorata des surfaces."),
            ("09:47", "Inès", "Ascenseur : intervention jeudi 19 novembre, de 9 h à 12 h. Confirmé."),
            ("09:55", "Sophie", "Je récupère les clés du rez-de-chaussée pour ouvrir au technicien."),
            ("10:41", "Paul", "La facture est prévue en décembre."),
            ("11:02", "Lucas", "Au fait, à qui est le vélo dans le hall ?"),
        ],
        "draft": "Merci Sophie, je serai là jeudi à 9 h pour le technicien.",
    },
    "en": {
        "chat": "Building — demo", "contact": "Sophie (demo)", "query": "elevator",
        "messages": [
            ("08:02", "Sophie", "Has anyone heard about the elevator?"),
            ("08:34", "Paul", "The building manager plans to split the cost by flat size."),
            ("09:47", "Inès", "Elevator: technician booked for Thursday 19 November, 9 am to noon. Confirmed."),
            ("09:55", "Sophie", "I'll collect the ground-floor keys to let the technician in."),
            ("10:41", "Paul", "The invoice is expected in December."),
            ("11:02", "Lucas", "By the way, whose bike is in the hall?"),
        ],
        "draft": "Thanks Sophie, I'll be there on Thursday at 9 am for the technician.",
    },
}


def fold(text):
    return "".join(c for c in unicodedata.normalize("NFD", text.lower()) if unicodedata.category(c) != "Mn")


def seed(directory, language):
    source = (ROOT / "whatsapp-go/internal/store/store.go").read_text()
    schema = re.search(r"const schema = `(.+?)`", source, re.S)[1]
    cfg = TEXT[language]
    with sqlite3.connect(directory / "messages.db") as db:
        db.executescript(schema)
        for name, phone in PHONES.items():
            display = cfg["contact"] if name == "Sophie" else name
            db.execute("INSERT INTO contacts (phone, name, name_fold, name_source, updated_at) VALUES (?,?,?,?,?)",
                       (phone, display, fold(display), "full_name", "2026-11-16T07:02:00Z"))
        db.execute("INSERT INTO chats VALUES (?,?,?,?,?)", (GROUP, cfg["chat"], fold(cfg["chat"]), 1, "2026-11-16T10:02:00Z"))
        direct = PHONES["Sophie"] + "@s.whatsapp.net"
        db.execute("INSERT INTO chats VALUES (?,?,?,?,?)", (direct, cfg["contact"], fold(cfg["contact"]), 0, None))
        for i, (clock, name, content) in enumerate(cfg["messages"], 1):
            hour, minute = map(int, clock.split(":"))  # Paris is UTC+1 in November.
            timestamp = f"2026-11-16T{hour-1:02}:{minute:02}:00Z"
            db.execute("INSERT INTO messages (id, chat_jid, sender_phone, sender_name, content, timestamp) VALUES (?,?,?,?,?,?)",
                       (f"DEMO{i}", GROUP, PHONES[name], name, content, timestamp))


def capture(binary, language):
    cfg = TEXT[language]
    with tempfile.TemporaryDirectory(prefix="askmywa-fictional-demo-") as tmp:
        directory = Path(tmp)
        seed(directory, language)
        with (directory / "stderr.log").open("w+") as errlog:
            proc = subprocess.Popen([str(binary), "serve", "--data-dir", tmp, "--send-root", str(directory / "files")],
                                    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=errlog, text=True, bufsize=1)
            selector = selectors.DefaultSelector()
            selector.register(proc.stdout, selectors.EVENT_READ)
            next_id = 0

            def rpc(method, params, notification=False):
                nonlocal next_id
                next_id += 1
                payload = {"jsonrpc": "2.0", "method": method, "params": params}
                if not notification:
                    payload["id"] = next_id
                proc.stdin.write(json.dumps(payload) + "\n")
                proc.stdin.flush()
                if notification:
                    return
                while True:
                    if not selector.select(timeout=15):
                        raise RuntimeError(f"MCP response timed out: {method}")
                    line = proc.stdout.readline()
                    if not line:
                        raise RuntimeError(f"MCP server exited: {method}")
                    response = json.loads(line)
                    if response.get("id") != next_id:
                        continue
                    if "error" in response:
                        raise RuntimeError(response["error"])
                    return response["result"]

            calls = []

            def call(name, arguments):
                assert name in {"list_chats", "list_messages", "search_contacts", "send_message"}
                if name == "send_message":
                    assert arguments.get("confirm") is False
                result = rpc("tools/call", {"name": name, "arguments": arguments})
                assert not result.get("isError"), result
                structured = result.get("structuredContent")
                if structured is None:
                    structured = json.loads(result["content"][0]["text"])
                calls.append({"tool": name, "arguments": arguments, "result": structured})
                return structured

            try:
                info = rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "askmywa-fictional-demo", "version": "1.0"}})
                assert info["serverInfo"]["version"] == "0.1.0", "Use the recorded v0.1.0 release"
                rpc("notifications/initialized", {}, notification=True)
                chats = call("list_chats", {"query": cfg["chat"], "limit": 20})
                assert len(chats["chats"]) == 1
                hits = call("list_messages", {"chat_jid": GROUP, "query": cfg["query"], "include_context": True, "context_before": 2, "context_after": 3, "limit": 20})
                assert len(hits["messages"]) == 2
                messages = call("list_messages", {"chat_jid": GROUP, "limit": 20})
                assert len(messages["messages"]) == len(cfg["messages"]) and messages["has_more"] is False
                contacts = call("search_contacts", {"query": "Sophie", "limit": 20})
                assert len(contacts["contacts"]) == 1
                preview = call("send_message", {"recipient": "Sophie", "message": cfg["draft"], "confirm": False})
                assert preview["sent"] is False and preview["needs_confirmation"] is True
                assert preview["preview"]["recipient_name"] == cfg["contact"]
                assert not (directory / "daemon.sock").exists(), "A daemon must never run in this demo"
            finally:
                proc.stdin.close()
                proc.terminate()
                proc.wait(timeout=5)
                selector.close()
    evidence = {
        "fictional_data": True,
        "presentation_note": "Real MCP tool outputs, presented in an illustrated video. Not a capture of Claude. No WhatsApp pairing or send.",
        "server": info["serverInfo"],
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "release": "https://github.com/Ruwad-io/askmywa/releases/tag/v0.1.0",
        "timezone": "Europe/Paris", "calls": calls,
    }
    destination = ROOT / f".github/assets/demo-run-{language}.json"
    destination.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n")
    print(f"{language}: recorded {len(calls)} actual tool calls; {len(messages['messages'])} fictional messages; sent=false")


if __name__ == "__main__":
    executable = Path(sys.argv[1]).resolve()
    for lang in TEXT:
        capture(executable, lang)
