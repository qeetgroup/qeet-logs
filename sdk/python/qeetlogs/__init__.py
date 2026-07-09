"""Official Python SDK for Qeet Logs (PRD Module 01.1 / 29.1).

Zero-config: one API key, one line to send a structured, searchable event.
Sends are best-effort by default — a network failure never raises into the host
application (logging must never break the request it observes; Stripe's
canonical-log-line discipline). Set raise_on_error=True to surface failures.

    from qeetlogs import Client
    logs = Client(api_key="qeel_...", base_url="http://localhost:8101")
    logs.log("payments", "charge failed", level="error", route="/charge")
"""

from __future__ import annotations

import json
import time
import urllib.error
import urllib.request
from typing import Any, Iterable

__all__ = ["Client"]

_DEFAULT_BASE = "https://ingest.logs.qeet.in"


class Client:
    def __init__(
        self,
        api_key: str,
        base_url: str = _DEFAULT_BASE,
        timeout: float = 10.0,
        retries: int = 2,
        raise_on_error: bool = False,
    ) -> None:
        if not api_key:
            raise ValueError("qeetlogs: api_key is required")
        self.api_key = api_key
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.retries = retries
        self.raise_on_error = raise_on_error

    def log(self, service: str, message: str, level: str = "info", **attrs: Any) -> None:
        """Build and send one structured event. Extra kwargs become attributes."""
        record: dict[str, Any] = {"service": service, "message": message, "level": level}
        # Recognised top-level fields pass through; everything else is metadata.
        for key in ("trace_id", "span_id", "environment", "timestamp",
                    "git_sha", "deploy_id", "pr_number"):
            if key in attrs:
                record[key] = attrs.pop(key)
        record.update(attrs)
        self.ingest(record)

    def ingest(self, record: dict[str, Any]) -> None:
        """Send a single record to /v1/ingest."""
        self._post("/v1/ingest", json.dumps(record).encode("utf-8"), "application/json")

    def ingest_batch(self, records: Iterable[dict[str, Any]]) -> None:
        """Send many records to /v1/ingest/batch as newline-delimited JSON."""
        body = "\n".join(json.dumps(r) for r in records).encode("utf-8")
        if not body:
            return
        self._post("/v1/ingest/batch", body, "application/x-ndjson")

    def _post(self, path: str, body: bytes, content_type: str) -> None:
        url = self.base_url + path
        last_err: Exception | None = None
        for attempt in range(self.retries + 1):
            req = urllib.request.Request(url, data=body, method="POST")
            req.add_header("X-Qeet-Api-Key", self.api_key)
            req.add_header("Content-Type", content_type)
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    if resp.status < 400:
                        return
                    last_err = RuntimeError(f"HTTP {resp.status}")
            except urllib.error.HTTPError as e:  # 4xx: don't retry client errors
                last_err = e
                if 400 <= e.code < 500:
                    break
            except Exception as e:  # network: retry with backoff
                last_err = e
            time.sleep(0.1 * (attempt + 1))
        if self.raise_on_error and last_err is not None:
            raise last_err
